package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/i18n"
	"github.com/Balrog57/DiskCount/internal/notifier"
	"github.com/Balrog57/DiskCount/internal/rules"
	"github.com/Balrog57/DiskCount/internal/scanner"
	"github.com/Balrog57/DiskCount/internal/sources"
)

const (
	sessionCookieName = "diskcount_session"
	sessionTTL        = 12 * time.Hour
	langCookieName    = "diskcount_lang"
	themeCookieName   = "diskcount_theme"
)

// Theme preference values. "auto" means "follow the OS preference";
// the inline bootstrap script in the layout applies the real
// light/dark attribute before paint so the user does not see a flash.
const (
	themeAuto  = "auto"
	themeLight = "light"
	themeDark  = "dark"
)

// localeForRequest resolves the active UI locale from (in order): the
// "lang" cookie set by the language switcher, the Accept-Language header
// on first visit, and finally the i18n default. The result is also
// persisted to a cookie so subsequent requests are stable.
func (s *Server) localeForRequest(w http.ResponseWriter, r *http.Request) i18n.Locale {
	if c, err := r.Cookie(langCookieName); err == nil && c.Value != "" {
		return i18n.ParseLocale(c.Value)
	}
	if al := r.Header.Get("Accept-Language"); al != "" {
		return i18n.ParseLocale(al)
	}
	return i18n.Default
}

// themeForRequest resolves the user's theme preference. Default is
// "auto" which the inline script in the layout translates to either
// light or dark based on the OS preference.
func (s *Server) themeForRequest(r *http.Request) string {
	if c, err := r.Cookie(themeCookieName); err == nil {
		switch c.Value {
		case themeLight, themeDark, themeAuto:
			return c.Value
		}
	}
	return themeAuto
}

// setThemeCookie pins the theme preference to a cookie so the choice
// survives reloads and propagates to the inline bootstrap script.
func setThemeCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     themeCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		HttpOnly: false, // readable by the inline bootstrap script
		SameSite: http.SameSiteLaxMode,
	})
}

// setTheme handles POST /theme?theme=light|dark|auto. It is a public
// endpoint so the switcher works on the login page too.
func (s *Server) setTheme(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v := r.Form.Get("theme")
	switch v {
	case themeLight, themeDark, themeAuto:
		setThemeCookie(w, v)
	default:
		http.Error(w, "invalid theme", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, sanitizeNext(r.Form.Get("next")), http.StatusSeeOther)
}

type Server struct {
	db                *db.DB
	scanner           *scanner.Scanner
	cfg               *config.Config
	cfgMu             sync.RWMutex
	sourceNames       []string
	sourceMu          sync.RWMutex
	startedAt         time.Time
	discordConfigured atomic.Bool
	discordTestSender func(string, string) error
}

type configRow struct {
	Meta  config.SettingMeta
	Value string
}

type appStatus struct {
	DiscordConfigured bool
	SourceCount       int
}

func New(dbase *db.DB, scan *scanner.Scanner, cfg *config.Config, sources []string) *Server {
	sort.Strings(sources)
	s := &Server{
		db: dbase, scanner: scan, cfg: cfg, sourceNames: sources, startedAt: time.Now().UTC(),
	}
	s.discordTestSender = func(token, channelID string) error {
		return notifier.NewDiscord(token, channelID).SendTest()
	}
	s.discordConfigured.Store(cfg != nil && cfg.DiscordBotToken != "" && cfg.DiscordChannelID != "")
	return s
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg == nil || s.cfg.WebAdminPassword == "" {
			http.Error(w, "Admin password not configured (set WEB_ADMIN_PASSWORD)", http.StatusForbidden)
			return
		}

		if s.validSession(r) {
			next.ServeHTTP(w, r)
			return
		}

		if user, pass, ok := r.BasicAuth(); ok {
			if subtle.ConstantTimeCompare([]byte(user), []byte("admin")) == 1 &&
				subtle.ConstantTimeCompare([]byte(pass), []byte(s.cfg.WebAdminPassword)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}

		if isHTMX(r) || acceptsJSON(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="DiskCount Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		nextURL := r.URL.RequestURI()
		if nextURL == "" {
			nextURL = "/"
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(nextURL), http.StatusSeeOther)
	})
}

func (s *Server) sessionSecret() []byte {
	return []byte(s.cfg.WebAdminPassword)
}

func (s *Server) signSession(issued time.Time) string {
	mac := hmac.New(sha256.New, s.sessionSecret())
	fmt.Fprintf(mac, "session:%d", issued.UnixNano())
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) validSession(r *http.Request) bool {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	parts := strings.SplitN(c.Value, ":", 2)
	if len(parts) != 2 {
		return false
	}
	issued, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	ts := time.Unix(0, issued)
	if time.Since(ts) > sessionTTL {
		return false
	}
	want := s.signSession(ts)
	got := parts[1]
	if len(want) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func (s *Server) setSessionCookie(w http.ResponseWriter) {
	issued := time.Now()
	value := fmt.Sprintf("%d:%s", issued.UnixNano(), s.signSession(issued))
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  issued.Add(sessionTTL),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func acceptsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}

func (s *Server) Run(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	slog.Info("web interface starting", "addr", addr)
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// handler composes the public health endpoints with the authenticated
// admin endpoints. Anything not matching the public paths goes through
// the session-based auth middleware, which redirects browsers to /login
// when no valid session cookie is present.
func (s *Server) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSafeMethod(r.Method) && !sameOrigin(r) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/health", "/healthz", "/readyz":
			s.health(w, r)
			return
		case "/login":
			s.login(w, r)
			return
		case "/logout":
			s.logout(w, r)
			return
		case "/lang":
			s.setLang(w, r)
			return
		case "/theme":
			s.setTheme(w, r)
			return
		case "/feed.xml", "/feed":
			s.feed(w, r)
			return
		}
		s.withAuth(s.routes()).ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// sameOrigin rejects cross-site state-changing requests by requiring Origin
// (or Referer) to match this request's Host with an exact boundary check.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" || r.Host == "" {
		return false
	}
	httpExact := "http://" + r.Host
	httpsExact := "https://" + r.Host
	return origin == httpExact || origin == httpsExact ||
		strings.HasPrefix(origin, httpExact+"/") ||
		strings.HasPrefix(origin, httpsExact+"/")
}

func (s *Server) routes() http.Handler {
	// muxAdmin is protected by session auth (see Server.handler). Public
	// endpoints like /health, /healthz, /readyz, /login and /logout are
	// dispatched in Server.handler before this mux sees the request.
	muxAdmin := http.NewServeMux()
	muxAdmin.HandleFunc("/", s.stats)
	muxAdmin.HandleFunc("/quality", s.quality)
	muxAdmin.HandleFunc("/products", s.products)
	muxAdmin.HandleFunc("/sites", s.sites)
	muxAdmin.HandleFunc("/logs", s.logs)
	muxAdmin.HandleFunc("/drops", s.priceDrops)
	muxAdmin.HandleFunc("/market", s.market)
	muxAdmin.HandleFunc("/api/market", s.apiMarket)
	muxAdmin.HandleFunc("/europe", s.europe)
	muxAdmin.HandleFunc("/product", s.productDetail)
	muxAdmin.HandleFunc("/alerts", s.alerts)
	muxAdmin.HandleFunc("/alerts/add", s.addAlert)
	muxAdmin.HandleFunc("/alerts/toggle", s.toggleAlert)
	muxAdmin.HandleFunc("/alerts/delete", s.deleteAlert)
	muxAdmin.HandleFunc("/discord", s.discordSettings)
	muxAdmin.HandleFunc("/discord/save", s.saveDiscordSettings)
	muxAdmin.HandleFunc("/discord/test", s.testDiscord)
	muxAdmin.HandleFunc("/config", s.config)
	muxAdmin.HandleFunc("/config/save", s.saveConfig)
	muxAdmin.HandleFunc("/sites/sources", s.saveSiteSources)
	muxAdmin.HandleFunc("/metrics/dashboard", s.metricsDashboard)
	muxAdmin.HandleFunc("/api/metrics", s.apiMetrics)
	muxAdmin.HandleFunc("/api/sources/breaker/reset", s.apiResetBreaker)
	muxAdmin.HandleFunc("/api/sources/health", s.apiSourcesHealth)
	muxAdmin.HandleFunc("/api/sources/preview", s.apiSourcePreview)
	muxAdmin.HandleFunc("/lang", s.setLang)
	return muxAdmin
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil || s.cfg.WebAdminPassword == "" {
		loc := s.localeForRequest(w, r)
		http.Error(w, i18n.T("web.login.no_pwd", loc), http.StatusForbidden)
		return
	}

	if s.validSession(r) {
		http.Redirect(w, r, sanitizeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}

	loc := s.localeForRequest(w, r)
	data := map[string]any{
		"Title":      i18n.T("web.login.title", loc),
		"Active":     "",
		"Next":       sanitizeNext(r.URL.Query().Get("next")),
		"Error":      "",
		"Locale":     string(loc),
		"T":          func(key string) string { return i18n.T(key, loc) },
		"KnownLangs": i18n.KnownLocales(),
		"Status":     appStatus{},
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		pass := r.Form.Get("password")
		if subtle.ConstantTimeCompare([]byte(pass), []byte(s.cfg.WebAdminPassword)) == 1 {
			s.setSessionCookie(w)
			http.Redirect(w, r, sanitizeNext(r.Form.Get("next")), http.StatusSeeOther)
			return
		}
		loc := s.localeForRequest(w, r)
		data["Error"] = i18n.T("web.login.error", loc)
	}

	render(w, loginTpl, data)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func sanitizeNext(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	if !strings.HasPrefix(u.Path, "/") {
		return "/"
	}
	// Reject protocol-relative paths (///evil.com) and backslash tricks;
	// browsers may treat these as off-site navigations after a redirect.
	if strings.HasPrefix(u.Path, "//") || strings.Contains(u.Path, `\`) {
		return "/"
	}
	if decoded, err := url.PathUnescape(u.Path); err != nil ||
		strings.HasPrefix(decoded, "//") || strings.Contains(decoded, `\`) {
		return "/"
	}
	return u.RequestURI()
}

func (s *Server) liveSourceNames() []string {
	s.sourceMu.RLock()
	defer s.sourceMu.RUnlock()
	out := make([]string, len(s.sourceNames))
	copy(out, s.sourceNames)
	return out
}

func (s *Server) setLiveSources(names []string) {
	sort.Strings(names)
	s.sourceMu.Lock()
	s.sourceNames = names
	s.sourceMu.Unlock()
}

func (s *Server) liveConfig() *config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

// reloadSources rebuilds scrapers from DB app_config and applies them live.
func (s *Server) reloadSources(ctx context.Context) error {
	if s.db == nil || s.scanner == nil {
		return nil
	}
	values, err := s.db.ListAppConfig(ctx)
	if err != nil {
		return err
	}
	newCfg := config.LoadWithAppValues(values)
	s.cfgMu.RLock()
	old := s.cfg
	s.cfgMu.RUnlock()
	if old != nil {
		newCfg.DatabaseURL = old.DatabaseURL
		newCfg.WebAdminAddr = old.WebAdminAddr
		if newCfg.WebAdminPassword == "" {
			newCfg.WebAdminPassword = old.WebAdminPassword
		}
	}
	reg := sources.NewRegistry(newCfg)
	srcs := sources.BuildAll(reg)
	names := make([]string, 0, len(srcs))
	for _, src := range srcs {
		names = append(names, src.Name())
	}
	s.scanner.SetConfig(newCfg)
	s.scanner.SetSources(srcs)
	s.cfgMu.Lock()
	s.cfg = newCfg
	s.cfgMu.Unlock()
	s.setLiveSources(names)
	slog.Info("sources reloaded", "count", len(names))
	return nil
}

func (s *Server) base(title, active string) map[string]any {
	return map[string]any{
		"Title":      title,
		"Active":     active,
		"Path":       "/",
		"Locale":     string(i18n.Default),
		"Theme":      themeAuto,
		"T":          func(key string) string { return i18n.T(key, i18n.Default) },
		"KnownLangs": i18n.KnownLocales(),
		"Status": appStatus{
			DiscordConfigured: s.discordConfigured.Load(),
			SourceCount:       len(s.liveSourceNames()),
		},
	}
}

// baseWithRequest is the request-aware version of base(). Templates that
// render a localised UI should always go through this so the chosen
// locale is honoured. Falls back to base() (default locale) when the
// request is nil so unit tests can keep using base().
func (s *Server) baseWithRequest(r *http.Request, title, active string) map[string]any {
	if r == nil {
		return s.base(title, active)
	}
	loc := s.localeForRequest(nil, r)
	theme := s.themeForRequest(r)
	path := "/"
	if r.URL != nil {
		path = r.URL.RequestURI()
		if path == "" {
			path = "/"
		}
	}
	return map[string]any{
		"Title":      title,
		"Active":     active,
		"Path":       path,
		"Locale":     string(loc),
		"Theme":      theme,
		"T":          func(key string) string { return i18n.T(key, loc) },
		"KnownLangs": i18n.KnownLocales(),
		"Status": appStatus{
			DiscordConfigured: s.discordConfigured.Load(),
			SourceCount:       len(s.liveSourceNames()),
		},
	}
}

// setLang handles POST /lang?lang=fr|en. It pins the chosen locale to a
// cookie and bounces the user back to the page they were on.
// setLangCookie pins the locale to a cookie. It is called by the
// language switcher endpoint and on login so the choice survives a
// logout.
func setLangCookie(w http.ResponseWriter, loc i18n.Locale) {
	http.SetCookie(w, &http.Cookie{
		Name:     langCookieName,
		Value:    string(loc),
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		HttpOnly: false, // readable by client-side code if we ever add JS
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) setLang(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	loc := i18n.ParseLocale(r.Form.Get("lang"))
	setLangCookie(w, loc)
	redirect := sanitizeNext(r.Form.Get("next"))
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	st, err := s.db.Stats(r.Context())
	notifications, notificationsErr := s.db.RecentNotifications(r.Context(), 20)
	loc := s.localeForRequest(nil, r)
	data := s.baseWithRequest(r, i18n.T("web.nav.dashboard", loc), "stats")
	data["Stats"] = st
	data["StatsError"] = err
	data["Notifications"] = notifications
	data["NotificationsError"] = notificationsErr
	data["Sources"] = s.liveSourceNames()
	data["LastReport"] = lastReport(s.scanner)
	data["StartedAt"] = s.startedAt
	// "Prochain scan" countdown: derive from the last finished report
	// and the configured interval. If the scheduler is not running
	// (LastReport is nil) or the interval cannot be parsed, NextCheck
	// stays nil so the template can branch on it.
	if s.scanner != nil && s.cfg != nil {
		if interval, ok := parseCronInterval(s.cfg.ScrapeIntervalCron); ok {
			var lastEnd time.Time
			if rep := s.scanner.LastReport(); rep != nil && !rep.FinishedAt.IsZero() {
				lastEnd = rep.FinishedAt
			} else if !s.startedAt.IsZero() {
				lastEnd = s.startedAt
			}
			if !lastEnd.IsZero() {
				next := lastEnd.Add(interval)
				now := time.Now().UTC()
				data["NextCheck"] = next
				data["NextCheckIn"] = next.Sub(now)
			}
		}
	}
	render(w, statsTpl, data)
}

// parseCronInterval delegates to config.ParseScrapeInterval so the web UI's
// "prochain scan" countdown and the scheduler share one parser. Anything the
// scheduler cannot understand is also hidden from the UI (ok=false → no
// countdown shown), which keeps the two views consistent.
func parseCronInterval(spec string) (time.Duration, bool) {
	return config.ParseScrapeInterval(spec)
}

// durationHuman formats a duration as a short, human-readable countdown
// like "2h 14m", "45s", or "1j 3h". Negative inputs (countdown already
// overdue) are clamped to zero and prefixed with a small "en retard"
// hint handled by the template, so we always return a positive string.
func durationHuman(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d / (24 * time.Hour))
	rem := d - time.Duration(days)*24*time.Hour
	return fmt.Sprintf("%dj %dh", days, int(rem.Hours()))
}

func relativeTimeAt(t, now time.Time) string {
	if t.IsZero() {
		return "inconnu"
	}
	d := now.Sub(t)
	if d < time.Minute {
		return "à l'instant"
	}
	if d < time.Hour {
		return fmt.Sprintf("il y a %d min", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("il y a %d h", int(d.Hours()))
	}
	return fmt.Sprintf("il y a %d j", int(d.Hours()/24))
}

func (s *Server) quality(w http.ResponseWriter, r *http.Request) {
	qs, err := s.db.QualityStats(r.Context())
	data := s.baseWithRequest(r, "Qualite donnees", "quality")
	data["Quality"] = qs
	data["Error"] = err
	render(w, qualityTpl, data)
}

func (s *Server) products(w http.ResponseWriter, r *http.Request) {
	data := s.baseWithRequest(r, "Produits", "products")
	data["Groups"] = []db.ProductGroup{}
	data["Ungrouped"] = []db.CurrentPrice{}
	data["Sparklines"] = map[string][]db.SparklinePoint{}
	q := catalogQueryFromRequest(r)
	data["Page"], data["Pages"], data["Total"] = q.Offset/q.Limit+1, 1, 0
	if s.db != nil {
		groups, total, err := s.db.CatalogGroups(r.Context(), q)
		data["Groups"], data["Total"], data["Error"] = groups, total, err
		if r.URL.Query().Get("ungrouped") == "1" {
			data["Ungrouped"], err = s.db.UngroupedPrices(r.Context(), q)
			if data["Error"] == nil {
				data["Error"] = err
			}
		}
		ids := make([]string, 0, len(groups))
		for _, g := range groups {
			if g.BestProductID != "" {
				ids = append(ids, g.BestProductID)
			}
		}
		if len(ids) > 0 {
			data["Sparklines"], _ = s.db.Sparklines(r.Context(), ids, 7, 24)
		}
		if total > 0 {
			data["Pages"] = (total + q.Limit - 1) / q.Limit
		}
	}
	if brands, categories, interfaces, recordings, sources, err := s.dbFacets(r.Context()); err == nil {
		data["Brands"], data["Categories"], data["Interfaces"], data["Recordings"], data["Sources"] = brands, categories, interfaces, recordings, sources
	}
	data["SelectedSource"] = q.Source
	data["SelectedMedia"] = q.Media
	data["SelectedCondition"] = q.Condition
	data["SelectedAvailability"] = q.Availability
	data["SelectedBrand"] = q.Brand
	data["SelectedCategory"] = q.Category
	data["SelectedInterface"] = q.Interface
	data["SelectedRecording"] = q.Recording
	data["Query"] = q.Search
	data["MinTB"], data["MaxTB"], data["MaxPrice"] = r.URL.Query().Get("min_tb"), r.URL.Query().Get("max_tb"), r.URL.Query().Get("max_eur_tb")
	data["Sort"], data["UngroupedSelected"] = q.Sort, r.URL.Query().Get("ungrouped") == "1"
	pages := data["Pages"].(int)
	data["PageLinks"] = catalogPageLinks(r, pages)
	render(w, productsTpl, data)
}

type catalogPageLink struct {
	Number int
	URL    string
}

func catalogPageLinks(r *http.Request, pages int) []catalogPageLink {
	if pages < 1 {
		return nil
	}
	current, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if current < 1 {
		current = 1
	}
	start, end := 1, pages
	if pages > 15 {
		start = current - 7
		if start < 1 {
			start = 1
		}
		end = start + 14
		if end > pages {
			end = pages
			start = pages - 14
			if start < 1 {
				start = 1
			}
		}
	}
	out := make([]catalogPageLink, 0, end-start+1)
	for i := start; i <= end; i++ {
		values := r.URL.Query()
		values.Set("page", strconv.Itoa(i))
		out = append(out, catalogPageLink{Number: i, URL: "/products?" + values.Encode()})
	}
	return out
}

func catalogQueryFromRequest(r *http.Request) db.CatalogQuery {
	v := r.URL.Query()
	q := db.CatalogQuery{Search: strings.TrimSpace(v.Get("q")), Source: v.Get("source"), Media: v.Get("media"), Condition: v.Get("condition"), Availability: v.Get("availability"), Brand: v.Get("brand"), Category: v.Get("category"), Interface: v.Get("interface"), Recording: v.Get("recording"), Sort: v.Get("sort"), Limit: 48}
	if page, err := strconv.Atoi(v.Get("page")); err == nil && page > 1 {
		q.Offset = (page - 1) * q.Limit
	}
	if n, ok := parseOptionalFloat(v.Get("min_tb")); ok {
		q.MinTB = &n
	}
	if n, ok := parseOptionalFloat(v.Get("max_tb")); ok {
		q.MaxTB = &n
	}
	if n, ok := parseOptionalFloat(v.Get("max_eur_tb")); ok {
		q.MaxEURTB = &n
	}
	return q
}

func (s *Server) dbFacets(ctx context.Context) ([]string, []string, []string, []string, []string, error) {
	if s.db == nil {
		return nil, nil, nil, nil, nil, nil
	}
	return s.db.CatalogFacets(ctx)
}

func (s *Server) priceDrops(w http.ResponseWriter, r *http.Request) {
	days, minDrop := 30, 2.0
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= 365 {
		days = v
	}
	if v, err := strconv.ParseFloat(strings.ReplaceAll(r.URL.Query().Get("min_drop"), ",", "."), 64); err == nil && v >= 0 && v <= 100 {
		minDrop = v
	}
	drops, err := s.db.PriceDrops(r.Context(), days, minDrop, 100)
	data := s.baseWithRequest(r, "Baisses de prix", "drops")
	data["Drops"], data["Days"], data["MinDrop"], data["Error"] = drops, days, minDrop, err
	render(w, priceDropsTpl, data)
}

func marketDays(r *http.Request) int {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days != 7 && days != 30 && days != 90 {
		days = 30
	}
	return days
}

func (s *Server) market(w http.ResponseWriter, r *http.Request) {
	days := marketDays(r)
	points, err := s.db.MarketIndex(r.Context(), days)
	data := s.baseWithRequest(r, "Indice du marche", "market")
	data["Market"], data["Days"], data["Error"] = points, days, err
	render(w, marketTpl, data)
}

func (s *Server) apiMarket(w http.ResponseWriter, r *http.Request) {
	points, err := s.db.MarketIndex(r.Context(), marketDays(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(points)
}

func (s *Server) europe(w http.ResponseWriter, r *http.Request) {
	selected := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("country")))
	prices, err := s.db.LatestPrices(r.Context(), 5000)
	offers, summaries := regionalize(prices, selected, 300)
	data := s.baseWithRequest(r, "Comparaison européenne", "europe")
	data["Offers"], data["Regions"], data["SelectedCountry"], data["Error"] = offers, summaries, selected, err
	render(w, europeTpl, data)
}

func (s *Server) productDetail(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}
	canonicalKey := strings.TrimSpace(r.URL.Query().Get("key"))
	productID := strings.TrimSpace(r.URL.Query().Get("id"))
	if canonicalKey == "" && productID == "" {
		http.Redirect(w, r, "/products", http.StatusSeeOther)
		return
	}
	var product *db.Product
	var err error
	if canonicalKey != "" {
		product, err = s.db.GetProductByCanonicalKey(r.Context(), canonicalKey)
	} else {
		product, err = s.db.GetProduct(r.Context(), productID)
	}
	if err != nil {
		data := s.baseWithRequest(r, "Produit", "products")
		data["Error"] = err
		render(w, productDetailTpl, data)
		return
	}
	if product == nil {
		http.Redirect(w, r, "/products", http.StatusSeeOther)
		return
	}
	if canonicalKey == "" && product.CanonicalKey != nil && *product.CanonicalKey != "" {
		target := "/product?key=" + url.QueryEscape(*product.CanonicalKey)
		if days := r.URL.Query().Get("days"); days != "" {
			target += "&days=" + url.QueryEscape(days)
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 && v <= 365 {
			days = v
		}
	}
	var history []db.PriceHistoryPoint
	var histErr error
	if canonicalKey != "" {
		history, histErr = s.db.PriceHistoryByKey(r.Context(), canonicalKey, days)
	} else {
		history, histErr = s.db.PriceHistory(r.Context(), productID, days)
	}
	var offers []db.ProductOffer
	var offersErr error
	if product.CanonicalKey != nil {
		offers, offersErr = s.db.ProductOffers(r.Context(), *product.CanonicalKey)
	}
	var current *db.PriceHistoryPoint
	if len(history) > 0 {
		current = &history[len(history)-1]
	}

	// Compute stats.
	var minPT, maxPT, avgPT float64
	if len(history) > 0 {
		minPT = history[0].PricePerTB
		maxPT = history[0].PricePerTB
		sum := 0.0
		for _, pt := range history {
			if pt.PricePerTB < minPT {
				minPT = pt.PricePerTB
			}
			if pt.PricePerTB > maxPT {
				maxPT = pt.PricePerTB
			}
			sum += pt.PricePerTB
		}
		avgPT = sum / float64(len(history))
	}

	data := s.baseWithRequest(r, "Produit", "products")
	data["Product"] = product
	data["History"] = history
	data["Current"] = current
	data["Offers"] = offers
	data["Days"] = days
	data["MinPT"] = minPT
	data["MaxPT"] = maxPT
	data["AvgPT"] = avgPT
	data["ChartPoints"] = computeChartPoints(history, minPT, maxPT)
	data["CanonicalKey"] = canonicalKey
	data["Error"] = firstErr(histErr, offersErr)
	render(w, productDetailTpl, data)
}

// computeChartPoints converts price history into SVG polyline coordinate
// strings for the detail page chart. The chart is 800x200 with 10px padding.
// Returns an empty string if there are fewer than 2 points.
func computeChartPoints(history []db.PriceHistoryPoint, minPT, maxPT float64) string {
	if len(history) < 2 {
		return ""
	}
	rng := maxPT - minPT
	if rng <= 0 {
		rng = 1
	}
	const (
		w = 800.0
		h = 200.0
		p = 10.0
	)
	var sb strings.Builder
	for i, pt := range history {
		x := p + float64(i)*(w-2*p)/float64(len(history)-1)
		normalized := (pt.PricePerTB - minPT) / rng
		y := h - p - normalized*(h-2*p)
		if i > 0 {
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, "%.1f,%.1f", x, y)
	}
	return sb.String()
}

// SparklineResult is the value exposed to templates via the .Sparkline
// helper. Templates can branch on Coords (empty → no data) and Trend
// (down/up/flat) without exposing the raw point list.
type SparklineResult struct {
	Coords string
	Trend  string
}

// computeSparklinePoints is the small 80x24 version rendered inside each
// row of the products page. It returns the polyline "x1,y1 x2,y2 ..."
// string and a stroke colour hint derived from the price trend (down =
// last observation below the median, up = above). "Down=green, up=red"
// matches what users expect on price pages: cheaper = good.
func computeSparklinePoints(points []db.SparklinePoint) SparklineResult {
	if len(points) < 2 {
		return SparklineResult{}
	}
	// Median to decide colour. Equal observations are a perfectly valid
	// sparkline; we do not bail.
	prices := make([]float64, len(points))
	for i, p := range points {
		prices[i] = p.PricePerTB
	}
	sorted := append([]float64(nil), prices...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	median := sorted[len(sorted)/2]
	last := prices[len(prices)-1]
	var trend string
	switch {
	case last < median-0.01:
		trend = "down"
	case last > median+0.01:
		trend = "up"
	default:
		trend = "flat"
	}

	const (
		w   = 80.0
		h   = 24.0
		pad = 2.0
	)
	minP, maxP := prices[0], prices[0]
	for _, v := range prices {
		if v < minP {
			minP = v
		}
		if v > maxP {
			maxP = v
		}
	}
	rng := maxP - minP
	if rng <= 0 {
		rng = 1
	}
	var sb strings.Builder
	for i, pt := range points {
		x := pad + float64(i)*(w-2*pad)/float64(len(points)-1)
		normalized := (pt.PricePerTB - minP) / rng
		y := h - pad - normalized*(h-2*pad)
		if i > 0 {
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, "%.1f,%.1f", x, y)
	}
	return SparklineResult{Coords: sb.String(), Trend: trend}
}

// feed serves a public RSS 2.0 feed of the latest best prices. It is
// unauthenticated so users can subscribe via RSS readers, Home Assistant,
// or automation tools like n8n.
func (s *Server) feed(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	prices, err := s.db.LatestPrices(r.Context(), limit)
	if err != nil {
		http.Error(w, "failed to load prices", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<rss version="2.0"><channel>`)
	sb.WriteString("<title>DiskCount - Meilleurs prix</title>")
	sb.WriteString("<link>https://diskcount.local</link>")
	sb.WriteString("<description>Meilleurs prix HDD/SSD suivus par DiskCount</description>")
	sb.WriteString("<language>fr</language>")
	for _, p := range prices {
		title := p.Title
		if title == "" {
			title = p.ProductID
		}
		media := "HDD"
		if p.MediaType != nil && *p.MediaType == "solid_state" {
			media = "SSD"
		}
		desc := fmt.Sprintf("%.2f EUR | %.2f EUR/To | %.1f To | %s | %s",
			p.PriceEUR, p.PricePerTB, p.CapacityTB, media, p.Source)
		sb.WriteString("<item>")
		sb.WriteString("<title>" + xmlEscape(title) + "</title>")
		sb.WriteString("<link>" + xmlEscape(p.URL) + "</link>")
		sb.WriteString("<description>" + xmlEscape(desc) + "</description>")
		sb.WriteString("<guid isPermaLink=\"false\">" + xmlEscape(p.ProductID) + "</guid>")
		if !p.ObservedAt.IsZero() {
			sb.WriteString("<pubDate>" + p.ObservedAt.UTC().Format(time.RFC1123Z) + "</pubDate>")
		}
		sb.WriteString("</item>")
	}
	sb.WriteString("</channel></rss>")
	w.Write([]byte(sb.String()))
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

func (s *Server) alerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.db.ListAlerts(r.Context(), false)
	data := s.baseWithRequest(r, "Alertes", "alerts")
	loc := i18n.ParseLocale(fmt.Sprint(data["Locale"]))
	data["Title"] = i18n.T("web.alerts.title", loc)
	data["Alerts"] = alerts
	data["Sources"] = s.liveSourceNames()
	data["CapacityPresets"] = rules.CapacityPresets
	data["BrandOptions"] = []string{"Seagate", "Western Digital", "Samsung", "Crucial", "Kingston", "Toshiba", "Corsair", "SanDisk", "Intel", "Micron", "SK Hynix", "TeamGroup", "Patriot", "ADATA", "Lexar"}
	data["InterfaceOptions"] = []string{"sata", "nvme", "usb", "sas", "thunderbolt"}
	data["RecordingOptions"] = []string{"cmr", "smr"}
	data["CategoryOptions"] = []string{"internal_3_5", "internal_2_5", "external_3_5", "external_2_5", "m2_nvme", "m2_sata", "internal_ssd", "external_ssd"}
	data["Error"] = err
	data["Saved"] = r.URL.Query().Get("saved") == "1"
	data["PrefillName"] = r.URL.Query().Get("name")
	data["PrefillKeywords"] = r.URL.Query().Get("keywords")
	render(w, alertsTpl, data)
}

func (s *Server) addAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	if name == "" {
		http.Error(w, "nom requis", http.StatusBadRequest)
		return
	}
	draft, err := alertDraftFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.db.CreateAlert(r.Context(), name, draft); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/alerts?saved=1", http.StatusSeeOther)
}

func alertDraftFromForm(r *http.Request) (db.AlertDraft, error) {
	d := db.AlertDraft{
		CapacityPresets:   cleanValues(r.Form["capacity"]),
		Conditions:        cleanValues(r.Form["condition"]),
		MediaTypes:        cleanValues(r.Form["media"]),
		Sources:           cleanValues(r.Form["source"]),
		Brands:            cleanValues(r.Form["brand"]),
		Interfaces:        cleanValues(r.Form["interface"]),
		RecordingMethods:  cleanValues(r.Form["recording"]),
		DriveCategories:   cleanValues(r.Form["category"]),
		Keywords:          splitList(r.Form.Get("keywords")),
		ExcludeKeywords:   splitList(r.Form.Get("exclude_keywords")),
		MinDiscountPct:    5,
		CooldownHours:     24,
		DiscordEnabled:    r.Form.Get("discord_enabled") == "1",
	}
	for _, preset := range d.CapacityPresets {
		if _, ok := rules.CapacityPresets[preset]; !ok {
			return d, fmt.Errorf("capacite inconnue: %s", preset)
		}
	}
	if raw := strings.TrimSpace(r.Form.Get("max_price_per_tb")); raw != "" {
		v, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err != nil || v <= 0 {
			return d, fmt.Errorf("prix maximum invalide")
		}
		d.MaxPricePerTB = &v
	}
	if raw := strings.TrimSpace(r.Form.Get("min_discount_pct")); raw != "" {
		v, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err != nil || v < 0 || v > 100 {
			return d, fmt.Errorf("remise minimale invalide")
		}
		d.MinDiscountPct = v
	}
	if raw := strings.TrimSpace(r.Form.Get("cooldown_hours")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return d, fmt.Errorf("delai invalide")
		}
		d.CooldownHours = v
	}
	return d, nil
}

func cleanValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func splitList(value string) []string {
	return cleanValues(strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' }))
}

func (s *Server) toggleAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	alertID, err := alertID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	enabled := r.Form.Get("enabled") == "1"
	if err := s.db.SetAlertEnabled(r.Context(), alertID, enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/alerts?saved=1", http.StatusSeeOther)
}

func (s *Server) deleteAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	alertID, err := alertID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Form.Get("confirm") != "delete" {
		http.Error(w, "confirmation required", http.StatusBadRequest)
		return
	}
	if err := s.db.DeleteAlert(r.Context(), alertID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/alerts?saved=1", http.StatusSeeOther)
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	values, err := s.db.ListAppConfig(r.Context())
	effective := config.DefaultValues()
	for k, v := range values {
		effective[k] = v
	}
	sections := map[string][]configRow{}
	for _, meta := range config.AppSettings {
		if isDiscordSetting(meta.Key) {
			continue
		}
		sec := meta.Section
		if sec == "" {
			sec = "advanced"
		}
		sections[sec] = append(sections[sec], configRow{Meta: meta, Value: effective[meta.Key]})
	}
	type sectionView struct {
		Key  string
		Rows []configRow
	}
	ordered := make([]sectionView, 0, len(config.ConfigSectionOrder))
	for _, key := range config.ConfigSectionOrder {
		if rows := sections[key]; len(rows) > 0 {
			ordered = append(ordered, sectionView{Key: key, Rows: rows})
		}
	}
	data := s.baseWithRequest(r, "Configuration", "config")
	loc := i18n.ParseLocale(fmt.Sprint(data["Locale"]))
	data["Title"] = i18n.T("web.config.title", loc)
	data["Sections"] = ordered
	data["Error"] = err
	data["Saved"] = r.URL.Query().Get("saved") == "1"
	data["RestartMsg"] = i18n.T("web.config.restart_note", loc)
	render(w, configTpl, data)
}

func (s *Server) saveConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	existing, err := s.db.ListAppConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	values := make(map[string]string)
	for _, meta := range config.AppSettings {
		if isDiscordSetting(meta.Key) {
			continue
		}
		if meta.Secret && r.Form.Get("replace_"+meta.Key) != "1" {
			values[meta.Key] = existing[meta.Key]
			continue
		}
		values[meta.Key] = strings.TrimSpace(r.Form.Get(meta.Key))
	}
	if err := s.db.SetAppConfig(r.Context(), values); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.reloadSources(r.Context()); err != nil {
		slog.Warn("config reload sources", "err", err)
	}
	http.Redirect(w, r, "/config?saved=1", http.StatusSeeOther)
}

func isDiscordSetting(key string) bool {
	return key == "DISCORD_BOT_TOKEN" || key == "DISCORD_CHANNEL_ID"
}

func (s *Server) discordSettings(w http.ResponseWriter, r *http.Request) {
	values, err := s.db.ListAppConfig(r.Context())
	data := s.baseWithRequest(r, "Discord", "discord")
	data["ChannelID"] = values["DISCORD_CHANNEL_ID"]
	data["TokenConfigured"] = values["DISCORD_BOT_TOKEN"] != ""
	data["Saved"] = r.URL.Query().Get("saved") == "1"
	data["Tested"] = r.URL.Query().Get("tested") == "1"
	data["TestAvailable"] = data["TokenConfigured"] == true && data["ChannelID"] != ""
	data["Error"] = err
	render(w, discordTpl, data)
}

func (s *Server) testDiscord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	values, err := s.db.ListAppConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	token, channelID := strings.TrimSpace(values["DISCORD_BOT_TOKEN"]), strings.TrimSpace(values["DISCORD_CHANNEL_ID"])
	if token == "" || channelID == "" {
		http.Error(w, "Discord non configuré", http.StatusBadRequest)
		return
	}
	if s.discordTestSender == nil {
		http.Error(w, "test Discord indisponible", http.StatusInternalServerError)
		return
	}
	if err := s.discordTestSender(token, channelID); err != nil {
		http.Error(w, "échec du test Discord: "+err.Error(), http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, "/discord?tested=1", http.StatusSeeOther)
}

func (s *Server) saveDiscordSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	existing, err := s.db.ListAppConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	token := existing["DISCORD_BOT_TOKEN"]
	if token == "" || r.Form.Get("replace_token") == "1" {
		token = strings.TrimSpace(r.Form.Get("DISCORD_BOT_TOKEN"))
	}
	channelID := strings.TrimSpace(r.Form.Get("DISCORD_CHANNEL_ID"))
	if (token == "") != (channelID == "") {
		http.Error(w, "le token et l'identifiant du salon Discord doivent être configurés ensemble", http.StatusBadRequest)
		return
	}
	if channelID != "" {
		if _, err := strconv.ParseUint(channelID, 10, 64); err != nil {
			http.Error(w, "l'identifiant du salon Discord doit être numérique", http.StatusBadRequest)
			return
		}
	}
	values := map[string]string{
		"DISCORD_BOT_TOKEN":  token,
		"DISCORD_CHANNEL_ID": channelID,
	}
	if err := s.db.SetAppConfig(r.Context(), values); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.discordConfigured.Store(token != "" && channelID != "")
	if s.scanner != nil {
		s.scanner.SetNotifier(notifier.NewDiscord(token, channelID))
	}
	http.Redirect(w, r, "/discord?saved=1", http.StatusSeeOther)
}

func (r configRow) DisplayValue() string {
	if r.Meta.Secret && r.Value != "" {
		return "********"
	}
	return r.Value
}

// apiMetrics returns a JSON snapshot of the last scan and per-source breaker
// state. The schema is intentionally stable so it can be polled by an
// external monitor or scraped by Prometheus (a separate exporter could map
// the same struct into the Prometheus text format).
func (s *Server) apiMetrics(w http.ResponseWriter, r *http.Request) {
	report := s.scanner.LastReport()
	out := map[string]any{
		"started_at":  s.startedAt,
		"discord":     s.discordConfigured.Load(),
		"sources":     s.liveSourceNames(),
		"breakers":    s.scanner.BreakerSnapshot(),
		"last_report": nil,
	}
	if report != nil {
		out["last_report"] = map[string]any{
			"started_at":      report.StartedAt,
			"finished_at":     report.FinishedAt,
			"fetched":         report.Fetched,
			"accepted":        report.Accepted,
			"rejected":        report.Rejected,
			"matched":         report.Matched,
			"notified":        report.Notified,
			"dry_run":         report.DryRun,
			"error_count":     len(report.Errors),
			"breaker_skips":   report.BreakerSkips,
			"sources":         report.SourceMetrics,
			"source_warnings": report.SourceWarnings,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	healthy := true
	dbStatus := "ok"
	if s.db == nil {
		dbStatus = "disabled"
		healthy = false
	} else if _, err := s.db.Stats(r.Context()); err != nil {
		dbStatus = "error: " + err.Error()
		healthy = false
	}
	report := s.scanner.LastReport()
	lastScan := "never"
	if report != nil && !report.FinishedAt.IsZero() {
		lastScan = report.FinishedAt.Format(time.RFC3339)
	}
	out := map[string]any{
		"status":    "ok",
		"db":        dbStatus,
		"discord":   s.discordConfigured.Load(),
		"sources":   len(s.liveSourceNames()),
		"last_scan": lastScan,
		"breakers":  s.scanner.BreakerSnapshot(),
	}
	if !healthy {
		out["status"] = "degraded"
	}
	code := http.StatusOK
	if !healthy {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, out)
}

func (s *Server) apiResetBreaker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	s.scanner.ResetBreaker(name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "reset": name})
}

// apiSourcesHealth returns a JSON snapshot of every configured source's
// health (consecutive zero-deal scans + flagged status). Intended for
// external monitoring (Prometheus exporter, uptime checks, dashboards).
func (s *Server) apiSourcesHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries := s.scanner.SourceHealth()
	flagged := 0
	for _, e := range entries {
		if e.Flagged {
			flagged++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":     len(entries),
		"flagged":   flagged,
		"threshold": s.scanner.ZeroStreakThreshold(),
		"sources":   entries,
	})
}

// apiSourcePreview triggers a live fetch for a single source and
// returns the parsed deals as JSON. It is admin-only and bypasses
// the per-source circuit breaker so the operator can verify a
// fix without waiting for the breaker to half-open.
//
// Query string: ?name=diskprices
func (s *Server) apiSourcePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Error(w, "name query param required", http.StatusBadRequest)
		return
	}
	if s.scanner == nil {
		http.Error(w, "scanner not configured", http.StatusServiceUnavailable)
		return
	}
	var target sources.Source
	for _, src := range s.scanner.Sources() {
		if src.Name() == name {
			target = src
			break
		}
	}
	if target == nil {
		http.Error(w, "unknown source: "+name, http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deals, err := target.Fetch(ctx)
	resp := map[string]any{
		"source": name,
		"deals":  deals,
		"count":  len(deals),
	}
	if err != nil {
		resp["error"] = err.Error()
		// Classify so the admin can tell apart a transient network
		// blip from a broken selector without parsing the message.
		if se, ok := err.(*sources.SourceError); ok {
			resp["severity"] = se.Severity
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) metricsDashboard(w http.ResponseWriter, r *http.Request) {
	report := s.scanner.LastReport()
	data := s.baseWithRequest(r, "Sante & metriques", "metrics")
	data["Report"] = report
	data["Breakers"] = s.scanner.BreakerSnapshot()
	render(w, metricsTpl, data)
}
func (r configRow) InputType() string {
	if r.Meta.Secret {
		return "password"
	}
	return "text"
}

func lastReport(scan *scanner.Scanner) *scanner.ScanReport {
	if scan == nil {
		return nil
	}
	return scan.LastReport()
}

func alertID(r *http.Request) (int64, error) {
	alertID, err := strconv.ParseInt(strings.TrimSpace(r.Form.Get("alert_id")), 10, 64)
	if err != nil || alertID == 0 {
		return 0, fmt.Errorf("alert_id invalide")
	}
	return alertID, nil
}

func filterPrices(prices []db.CurrentPrice, r *http.Request) []db.CurrentPrice {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	media := strings.TrimSpace(r.URL.Query().Get("media"))
	condition := strings.TrimSpace(r.URL.Query().Get("condition"))
	availability := strings.TrimSpace(r.URL.Query().Get("availability"))
	brand := strings.TrimSpace(r.URL.Query().Get("brand"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	iface := strings.TrimSpace(r.URL.Query().Get("interface"))
	recording := strings.TrimSpace(r.URL.Query().Get("recording"))
	minTB, hasMin := parseOptionalFloat(r.URL.Query().Get("min_tb"))
	maxTB, hasMax := parseOptionalFloat(r.URL.Query().Get("max_tb"))
	maxPrice, hasMaxPrice := parseOptionalFloat(r.URL.Query().Get("max_eur_tb"))
	out := make([]db.CurrentPrice, 0, len(prices))
	for _, p := range prices {
		if query != "" && !strings.Contains(strings.ToLower(p.Title), query) {
			continue
		}
		if source != "" && p.Source != source {
			continue
		}
		if media != "" && (p.MediaType == nil || *p.MediaType != media) {
			continue
		}
		if condition != "" && (p.Condition == nil || *p.Condition != condition) {
			continue
		}
		if availability != "" && string(p.Availability) != availability {
			continue
		}
		if brand != "" && (p.Brand == nil || *p.Brand != brand) {
			continue
		}
		if category != "" && (p.DriveCategory == nil || *p.DriveCategory != category) {
			continue
		}
		if recording != "" && (p.RecordingMethod == nil || *p.RecordingMethod != recording) {
			continue
		}
		if iface != "" && !slices.Contains(p.Interfaces, iface) {
			continue
		}
		if hasMin && p.CapacityTB < minTB {
			continue
		}
		if hasMax && p.CapacityTB > maxTB {
			continue
		}
		if hasMaxPrice && p.PricePerTB > maxPrice {
			continue
		}
		out = append(out, p)
	}
	if len(out) > 100 {
		return out[:100]
	}
	return out
}

func sortedSet(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uniqueSources(prices []db.CurrentPrice) []string {
	seen := make(map[string]bool)
	for _, p := range prices {
		seen[p.Source] = true
	}
	out := make([]string, 0, len(seen))
	for source := range seen {
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

func parseOptionalFloat(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	val, err := strconv.ParseFloat(raw, 64)
	return val, err == nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

// tmplFuncs is the function map shared by every parsed template. It is built
// once at package init; render() references it when populating the cache.
var tmplFuncs = template.FuncMap{
	"join": strings.Join,
	"ts": func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return t.Local().Format("2006-01-02 15:04")
	},
	"tsv": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Local().Format("2006-01-02 15:04")
	},
	"flt": func(v *float64) string {
		if v == nil {
			return "-"
		}
		return fmt.Sprintf("%.2f", *v)
	},
	"price":    func(v float64) string { return fmt.Sprintf("%.2f", v) },
	"cap":      func(v float64) string { return fmt.Sprintf("%.1f To", v) },
	"urlquery": url.QueryEscape,
	"ptr": func(v *string) string {
		if v == nil || *v == "" {
			return "-"
		}
		return *v
	},
	"csv": func(values []string) string {
		if len(values) == 0 {
			return "Tous"
		}
		return strings.Join(values, ", ")
	},
	"alertPrice": func(a db.Alert) string {
		if a.MaxPricePerTB == nil {
			return "Sans limite"
		}
		return fmt.Sprintf("%.2f EUR/To", *a.MaxPricePerTB)
	},
	"stateClass": func(ok bool) string {
		if ok {
			return "good"
		}
		return "warn"
	},
	"Sparkline": func(points []db.SparklinePoint) SparklineResult {
		return computeSparklinePoints(points)
	},
	"durationHuman": durationHuman,
	"ago":           func(t time.Time) string { return relativeTimeAt(t, time.Now()) },
	"mediaLabel": func(v *string) string {
		if v == nil {
			return "Stockage"
		}
		if *v == "solid_state" {
			return "SSD"
		}
		if *v == "rotational" {
			return "HDD"
		}
		return *v
	},
	"conditionLabel": func(v *string) string {
		if v == nil {
			return i18n.T("web.condition.unknown", i18n.Default)
		}
		if *v == "new" {
			return i18n.T("web.condition.new", i18n.Default)
		}
		if *v == "used" {
			return i18n.T("web.condition.used", i18n.Default)
		}
		return *v
	},
}

// templateCache memoises the parsed template for each body string. The
// previous render() re-parsed layoutTpl+body on every HTTP request, which
// allocated and parsed a multi-KB template each time. Since the bodies are
// package-level constants, the cache holds ~10 entries in steady state.
// A sync.Map keeps the read path lock-free after the first miss.
var templateCache sync.Map // body(string) -> *template.Template

// parseBodyTemplate parses layoutTpl+body once and caches the result.
// The FuncMap is installed before Parse so the cached template already
// knows about all helpers. Duplicate concurrent parses of the same body
// are harmless — last writer wins and both produce equivalent templates.
func parseBodyTemplate(body string) *template.Template {
	if cached, ok := templateCache.Load(body); ok {
		return cached.(*template.Template)
	}
	tpl := template.Must(template.New("page").Funcs(tmplFuncs).Parse(layoutTpl + body))
	actual, _ := templateCache.LoadOrStore(body, tpl)
	return actual.(*template.Template)
}

func render(w http.ResponseWriter, body string, data map[string]any) {
	if _, ok := data["Status"]; !ok {
		data["Status"] = appStatus{}
	}
	if _, ok := data["Locale"]; !ok {
		data["Locale"] = string(i18n.Default)
	}
	if _, ok := data["T"]; !ok {
		// Fallback translator so legacy callers (and a couple of unit
		// tests that render bodies in isolation) still get a working
		// `{{call .T "key"}}` instead of a nil-call panic. The default
		// locale matches the rendered locale above.
		loc := i18n.Locale(data["Locale"].(string))
		data["T"] = func(key string) string { return i18n.T(key, loc) }
	}
	if _, ok := data["KnownLangs"]; !ok {
		data["KnownLangs"] = i18n.KnownLocales()
	}
	if _, ok := data["Theme"]; !ok {
		data["Theme"] = themeAuto
	}
	tpl := parseBodyTemplate(body)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template: %v", err), http.StatusInternalServerError)
	}
}

const layoutTpl = `<!doctype html>
<html lang="{{.Locale}}" data-theme="{{.Theme}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>DiskCount - {{.Title}}</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;600;700&family=Press+Start+2P&display=swap" rel="stylesheet">
<script>
  // Inline theme bootstrap: read the cookie and apply the data-theme
  // attribute before the CSS paints so users on dark mode do not see
  // a light-mode flash on first load. Falls back to the OS preference
  // when no cookie is set.
  (function () {
    var m = document.cookie.match(/(?:^|;\s*)diskcount_theme=(light|dark|auto)/);
    var v = m ? m[1] : 'auto';
    if (v === 'auto') {
      v = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
    document.documentElement.setAttribute('data-theme', v);
  })();
</script>
<style>
:root[data-theme=light]{color-scheme:light;--bg:#e8eef5;--panel:#ffffff;--ink:#0a1628;--muted:#5a6b82;--line:#1a2a40;--line2:#c5d0de;--nav:#0a1628;--nav2:#1a3a5c;--brand:#1a6fff;--brand2:#0d4fcc;--good:#0d7a4a;--warn:#9a5a00;--bad:#b42018;--soft:#d6e4f5;--pixel:#1a6fff}
:root[data-theme=dark]{color-scheme:dark;--bg:#060e1a;--panel:#0c1828;--ink:#e8f0fa;--muted:#8a9bb0;--line:#3a5a7a;--line2:#1a3048;--nav:#040a14;--nav2:#0e2848;--brand:#3b9cff;--brand2:#1879dc;--good:#3dbf7a;--warn:#d4a04a;--bad:#e07068;--soft:#0e2438;--pixel:#3b9cff}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.45 "IBM Plex Mono",ui-monospace,Consolas,monospace}a{color:inherit}
.app{min-height:100vh;display:grid;grid-template-columns:232px 1fr}.sidebar{position:sticky;top:0;height:100vh;background:var(--nav);color:#f0f6fa;padding:16px 12px;display:flex;flex-direction:column;gap:18px;border-right:3px solid var(--pixel)}.brand{font-family:"Press Start 2P",monospace;font-size:11px;line-height:1.5;letter-spacing:0;color:#fff}.brand:before{content:"";display:inline-block;width:10px;height:10px;margin-right:8px;background:var(--pixel);box-shadow:2px 2px 0 #000;vertical-align:middle}.nav{display:grid;gap:4px}.nav a{display:flex;align-items:center;gap:10px;text-decoration:none;color:#b8cdd8;padding:9px 10px;border:2px solid transparent;transition:background-color .1s step-end,color .1s step-end}.nav a.active,.nav a:hover{background:var(--nav2);color:#fff;border-color:var(--pixel)}.dot{width:8px;height:8px;background:#4a6680;flex:0 0 auto}.active .dot{background:var(--pixel);box-shadow:2px 0 0 #000}.shell{min-width:0}.topbar{min-height:60px;border-bottom:3px solid var(--line);display:flex;align-items:center;justify-content:space-between;padding:8px 22px;position:sticky;top:0;z-index:2;background:var(--panel)}.topbar h1{font-family:"Press Start 2P",monospace;font-size:12px;margin:0;line-height:1.4}.status{display:flex;gap:8px;flex-wrap:wrap;align-items:center}.badge{display:inline-flex;align-items:center;gap:6px;border:2px solid var(--line);border-radius:0;padding:4px 8px;background:var(--panel);color:var(--muted);font-size:11px}.badge.good{color:var(--good);border-color:var(--good)}.badge.warn{color:var(--warn);border-color:var(--warn)}.badge.bad{color:var(--bad);border-color:var(--bad)}main{max-width:1440px;margin:0 auto;padding:22px 26px 40px}.section{margin-top:20px}.grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.card,.panel{background:var(--panel);border:2px solid var(--line);border-radius:0}.card{padding:16px}.label{color:var(--muted);font-size:11px;text-transform:uppercase;letter-spacing:.06em}.value{font-size:26px;font-weight:700;margin-top:4px;font-family:"Press Start 2P",monospace;font-size:16px;line-height:1.3}.hint{color:var(--muted);font-size:12px}.panel{overflow:hidden}.panel-head{display:flex;align-items:center;justify-content:space-between;gap:12px;border-bottom:2px solid var(--line2);padding:14px 16px}.panel-head h2{font-size:14px;margin:0;font-family:"Press Start 2P",monospace;font-size:11px;line-height:1.4}.panel-body{padding:16px}.table-wrap{overflow:auto}.config-row{display:grid;grid-template-columns:1fr 2fr auto;gap:14px;align-items:center;padding:12px 16px;border-bottom:1px solid var(--line2)}.stats-row{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;padding:16px}.stat{padding:12px;border:2px solid var(--line2);background:var(--soft)}.chart-wrap{padding:16px}.price-chart{width:100%;height:220px}.range-links{display:flex;gap:6px}.range-links a{padding:5px 9px;border:2px solid var(--line);text-decoration:none}.range-links a.active{background:var(--brand);color:#fff;border-color:var(--brand)}
.topbar .lang-switch,.topbar .theme-switch{margin:0;width:auto;display:inline-flex;gap:4px;align-items:center;color:var(--muted);font-size:11px}
.topbar .lang-switch a,.topbar .lang-switch button,.topbar .theme-switch a,.topbar .theme-switch button{background:var(--panel);border:2px solid var(--line);color:var(--ink);border-radius:0;padding:4px 8px;font-size:11px;cursor:pointer;text-decoration:none;font-family:inherit}
.topbar .lang-switch a.active,.topbar .lang-switch button.active,.topbar .theme-switch a.active,.topbar .theme-switch button.active{background:var(--brand);border-color:var(--brand);color:#fff;font-weight:700}
.login-shell .lang-switch{display:flex;gap:6px;justify-content:center;margin-top:14px}
.login-shell .lang-switch button{background:var(--panel);border:2px solid var(--line);color:var(--ink);border-radius:0;padding:4px 10px;cursor:pointer}
.login-shell .lang-switch button.active{background:var(--brand);border-color:var(--brand);color:#fff}
@media (max-width:960px){.app{grid-template-columns:1fr}.sidebar{position:sticky;top:0;z-index:8;height:auto;display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;gap:12px;padding:10px 12px;border-right:0;border-bottom:3px solid var(--pixel)}.brand{font-size:10px;white-space:nowrap}.nav{display:flex;gap:4px;overflow-x:auto;scrollbar-width:none}.nav::-webkit-scrollbar{display:none}.nav a{flex:0 0 auto;padding:7px 9px}.topbar{position:relative;height:auto;padding:10px 14px;flex-wrap:wrap;gap:8px}.topbar .status>span.badge:not(.ctrl){display:none}main{padding:14px}.grid{grid-template-columns:repeat(2,minmax(0,1fr))}.filters,.config-row{grid-template-columns:1fr}.form-grid{grid-template-columns:1fr}.mobile-title{display:block}}
@media (max-width:560px){.sidebar{grid-template-columns:1fr}.brand{display:none}.grid{grid-template-columns:1fr}.topbar{min-height:48px}.topbar h1{font-size:10px}.status{margin-left:auto}.truncate{max-width:240px}}
.sparkline{width:80px;height:24px;display:block}
.muted{color:var(--muted)}
:root[data-theme=dark] .topbar{background:var(--panel);color:var(--ink)}
:root[data-theme=dark] .login-card input[type=password]{background:var(--panel);color:var(--ink)}
:root[data-theme=dark] .login-card .error{background:rgba(232,121,114,.12);border-color:rgba(232,121,114,.5)}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:11px 14px;border-bottom:1px solid var(--line2);vertical-align:middle}th{color:var(--muted);font-size:10px;text-transform:uppercase;letter-spacing:.06em}tbody tr:hover{background:var(--soft)}
input,select{width:100%;min-height:40px;padding:8px 10px;border:2px solid var(--line);border-radius:0;background:var(--panel);color:var(--ink);font-family:inherit}input:focus,select:focus{outline:2px solid var(--brand);outline-offset:0}.filters,.form-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;align-items:end}.actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}.inline{display:inline;margin:0}button,.button{display:inline-flex;align-items:center;justify-content:center;min-height:36px;border:2px solid var(--brand2);border-radius:0;padding:8px 12px;background:var(--brand);color:#fff;font-weight:700;text-decoration:none;cursor:pointer;font-family:inherit;box-shadow:3px 3px 0 #000}button:hover,.button:hover{background:var(--brand2)}button.secondary{background:var(--soft);color:var(--brand);border-color:var(--line);box-shadow:2px 2px 0 var(--line)}button.danger{background:var(--bad);border-color:#7a100c}.notice,.warnbox{border-radius:0;padding:11px 13px;margin-bottom:14px;border:2px solid}.notice{background:var(--soft);border-color:var(--brand);color:var(--brand)}.warnbox{background:rgba(212,160,74,.12);border-color:var(--warn)}.source-list,.check-grid{display:flex;gap:8px;flex-wrap:wrap}.check-chip{display:flex;gap:7px;align-items:center;border:2px solid var(--line);border-radius:0;padding:7px 10px;background:var(--soft)}.check-chip input{width:auto;min-height:auto}.alert-form{display:grid;gap:16px}.alert-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.offer-link{color:var(--brand);font-weight:700;text-decoration:none;white-space:nowrap}.empty{padding:22px;text-align:center;color:var(--muted)}.config-section{margin-top:0}.config-section+.config-section{margin-top:18px}.config-section summary{cursor:pointer;font-weight:700;padding:10px 0;color:var(--brand)}
@media (max-width:960px){.alert-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media (max-width:560px){.alert-grid{grid-template-columns:1fr}}
</style>
</head>
<body>
<div class="app">
<aside class="sidebar">
<div class="brand">DiskCount</div>
<nav class="nav">
<a href="/" class="{{if eq .Active "stats"}}active{{end}}" {{if eq .Active "stats"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.dashboard"}}</a>
<a href="/products" class="{{if eq .Active "products"}}active{{end}}" {{if eq .Active "products"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.products"}}</a>
<a href="/sites" class="{{if eq .Active "sites"}}active{{end}}" {{if eq .Active "sites"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.sites"}}</a>
<a href="/logs" class="{{if eq .Active "logs"}}active{{end}}" {{if eq .Active "logs"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.logs"}}</a>
<a href="/drops" class="{{if eq .Active "drops"}}active{{end}}" {{if eq .Active "drops"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.drops"}}</a>
<a href="/alerts" class="{{if eq .Active "alerts"}}active{{end}}" {{if eq .Active "alerts"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.create_alert"}}</a>
<a href="/discord" class="{{if eq .Active "discord"}}active{{end}}" {{if eq .Active "discord"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.discord"}}</a>
<a href="/config" class="{{if eq .Active "config"}}active{{end}}" {{if eq .Active "config"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.config"}}</a>
</nav>
</aside>
<div class="shell">
<header class="topbar">
<h1>{{.Title}}</h1>
<div class="status">
<div class="lang-switch" role="group" aria-label="{{call .T "web.lang.label"}}">{{range .KnownLangs}}<form method="post" action="/lang" style="display:inline;margin:0"><input type="hidden" name="lang" value="{{.}}"><input type="hidden" name="next" value="{{$.Path}}"><button type="submit" aria-label="{{if eq . "fr"}}{{call $.T "web.lang.fr"}}{{else}}{{call $.T "web.lang.en"}}{{end}}" {{if eq $.Locale .}}class="active" aria-pressed="true"{{else}}aria-pressed="false"{{end}}>{{if eq . "fr"}}FR{{else}}EN{{end}}</button></form>{{end}}</div>
<div class="theme-switch" role="group" aria-label="{{call .T "web.theme.label"}}">
<form method="post" action="/theme" style="display:inline;margin:0"><input type="hidden" name="theme" value="light"><input type="hidden" name="next" value="{{.Path}}"><button type="submit" aria-label="{{call .T "web.theme.light"}}" {{if eq .Theme "light"}}class="active" aria-pressed="true"{{else}}aria-pressed="false"{{end}}>{{call .T "web.theme.light"}}</button></form>
<form method="post" action="/theme" style="display:inline;margin:0"><input type="hidden" name="theme" value="dark"><input type="hidden" name="next" value="{{.Path}}"><button type="submit" aria-label="{{call .T "web.theme.dark"}}" {{if eq .Theme "dark"}}class="active" aria-pressed="true"{{else}}aria-pressed="false"{{end}}>{{call .T "web.theme.dark"}}</button></form>
<form method="post" action="/theme" style="display:inline;margin:0"><input type="hidden" name="theme" value="auto"><input type="hidden" name="next" value="{{.Path}}"><button type="submit" aria-label="{{call .T "web.theme.auto"}}" {{if eq .Theme "auto"}}class="active" aria-pressed="true"{{else}}aria-pressed="false"{{end}}>{{call .T "web.theme.auto"}}</button></form>
</div>
<span class="badge {{stateClass .Status.DiscordConfigured}}">Discord {{if .Status.DiscordConfigured}}{{call .T "web.topbar.discord_ok"}}{{else}}{{call .T "web.topbar.discord_off"}}{{end}}</span>
<span class="badge">{{.Status.SourceCount}} {{call .T "web.topbar.sources"}}</span>
<form method="post" action="/logout" style="display:inline;margin:0"><button class="badge" type="submit" style="cursor:pointer">{{call .T "web.nav.logout"}}</button></form>
</div>
</header>
<main>{{template "body" .}}</main>
</div>
</div>
</body></html>`

const loginTpl = `{{define "body"}}
<style>
.login-shell{min-height:calc(100vh - 0px);display:grid;place-items:center;padding:40px 20px}
.login-card{background:var(--panel);border:2px solid var(--line);border-radius:0;padding:28px;width:100%;max-width:380px;box-shadow:4px 4px 0 #000}
.login-card h1{margin:0 0 6px;font-size:22px}
.login-card p.hint{margin:0 0 18px}
.login-card label{display:block;font-size:13px;color:var(--muted);margin-bottom:6px}
.login-card input[type=password]{width:100%;padding:10px 12px;border:1px solid var(--line);border-radius:8px;font-size:15px;background:#fff;color:var(--ink)}
.login-card input[type=password]:focus{outline:2px solid var(--brand);outline-offset:1px;border-color:var(--brand)}
.login-card .actions{margin-top:18px;display:flex;align-items:center;gap:10px}
.login-card button{background:var(--brand);color:#fff;border:0;border-radius:8px;padding:10px 16px;font-weight:600;cursor:pointer}
.login-card button:hover{background:var(--brand2)}
.login-card .error{background:#fff1f0;border:1px solid #f5b5b0;color:var(--bad);border-radius:8px;padding:10px 12px;margin-bottom:14px;font-size:13px}
</style>
<div class="login-shell">
<form class="login-card" method="post" action="/login" autocomplete="on">
<h1>{{call .T "web.login.title"}}</h1>
<p class="hint">{{call .T "web.login.intro"}}</p>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
<input type="hidden" name="next" value="{{.Next}}">
<label for="password">{{call .T "web.login.password"}}</label>
<input id="password" name="password" type="password" required autocomplete="current-password" autofocus>
<div class="actions">
<button type="submit">{{call .T "web.login.submit"}}</button>
<span class="hint" style="margin-left:auto">{{call .T "web.login.restricted"}}</span>
</div>
</form>
<div class="lang-switch" style="margin:14px auto 0;justify-content:center" role="group" aria-label="{{call .T "web.lang.label"}}">{{range .KnownLangs}}<form method="post" action="/lang" style="display:inline;margin:0"><input type="hidden" name="lang" value="{{.}}"><input type="hidden" name="next" value="{{$.Title}}"><button type="submit" aria-label="{{if eq . "fr"}}{{call $.T "web.lang.fr"}}{{else}}{{call $.T "web.lang.en"}}{{end}}" {{if eq $.Locale .}}class="active" aria-pressed="true"{{else}}aria-pressed="false"{{end}}>{{if eq . "fr"}}FR{{else}}EN{{end}}</button></form>{{end}}</div>
</div>
{{end}}`

const statsTpl = `{{define "body"}}
{{if .StatsError}}<div class="warnbox">{{call .T "web.common.error_prefix"}} DB: {{.StatsError}}</div>{{end}}
<div class="grid">
<div class="card"><div class="label">{{call .T "web.dashboard.active_alerts"}}</div><div class="value">{{if .Stats}}{{.Stats.ActiveAlerts}}{{else}}0{{end}}</div></div>
<div class="card"><div class="label">{{call .T "web.dashboard.products"}}</div><div class="value">{{if .Stats}}{{.Stats.Products}}{{else}}0{{end}}</div></div>
<div class="card"><div class="label">{{call .T "web.dashboard.observations"}}</div><div class="value">{{if .Stats}}{{.Stats.Observations}}{{else}}0{{end}}</div></div>
<div class="card"><div class="label">{{call .T "web.dashboard.notifications"}}</div><div class="value">{{if .Stats}}{{.Stats.Notifications}}{{else}}0{{end}}</div></div>
</div>
<div class="section grid">
<div class="card"><div class="label">{{call .T "web.dashboard.inactive_alerts"}}</div><div class="value">{{if .Stats}}{{.Stats.InactiveAlerts}}{{else}}0{{end}}</div></div>
<div class="card"><div class="label">{{call .T "web.dashboard.discord"}}</div><div class="value" style="font-size:14px">{{if .Status.DiscordConfigured}}{{call .T "web.common.configured"}}{{else}}{{call .T "web.common.optional"}}{{end}}</div></div>
<div class="card"><div class="label">{{call .T "web.dashboard.sources"}}</div><div class="value">{{.Status.SourceCount}}</div></div>
<div class="card"><div class="label">{{call .T "web.dashboard.rejected"}}</div><div class="value">{{if .Stats}}{{.Stats.RejectedDeals}}{{else}}0{{end}}</div></div>
</div>
<div class="section" style="display:flex;gap:9px;flex-wrap:wrap"><a class="button" href="/products">{{call .T "web.dashboard.view_products"}}</a><a class="button" href="/sites">{{call .T "web.dashboard.sites_state"}}</a><a class="button" href="/drops">{{call .T "web.dashboard.price_drops"}}</a><a class="button" href="/alerts">{{call .T "web.dashboard.create_alert"}}</a><a class="offer-link" href="/market">{{call .T "web.dashboard.market_index"}}</a><a class="offer-link" href="/europe">{{call .T "web.dashboard.europe"}}</a><a class="offer-link" href="/metrics/dashboard">{{call .T "web.dashboard.tech_metrics"}}</a></div>
<section class="section panel"><div class="panel-head"><h2>{{call .T "web.dashboard.active_sources"}}</h2></div><div class="panel-body"><div class="source-list">{{range .Sources}}<span class="badge">{{.}}</span>{{else}}<span class="muted">{{call $.T "web.common.no_source"}}</span>{{end}}</div></div></section>
<section class="section panel"><div class="panel-head"><h2>{{call .T "web.dashboard.recent_events"}}</h2></div><div class="table-wrap"><table><tbody>
<tr><th>{{call .T "web.dashboard.web_started"}}</th><td>{{tsv .StartedAt}}</td></tr>
<tr><th>{{call .T "web.dashboard.last_observation"}}</th><td>{{if .Stats}}{{ts .Stats.LastObservationAt}}{{else}}-{{end}}</td></tr>
<tr><th>{{call .T "web.dashboard.last_notification"}}</th><td>{{if .Stats}}{{ts .Stats.LastNotificationAt}}{{else}}-{{end}}</td></tr>
{{if .LastReport}}<tr><th>{{call .T "web.dashboard.last_scan"}}</th><td>{{tsv .LastReport.FinishedAt}} - fetched={{.LastReport.Fetched}}, accepted={{.LastReport.Accepted}}, rejected={{.LastReport.Rejected}}, matched={{.LastReport.Matched}}, notified={{.LastReport.Notified}}, errors={{len .LastReport.Errors}}</td></tr>{{else}}<tr><th>{{call .T "web.dashboard.last_scan"}}</th><td>-</td></tr>{{end}}
{{if .NextCheck}}<tr><th>{{call .T "web.dashboard.next_scan"}}</th><td>{{tsv .NextCheck}} - {{durationHuman .NextCheckIn}}</td></tr>{{end}}
</tbody></table></div></section>
{{if .LastReport}}{{if gt (len .LastReport.SourceWarnings) 0}}<section class="panel warnbox"><div class="panel-head"><h2>{{call .T "web.dashboard.warnings_title"}}</h2></div><div class="table-wrap"><table><thead><tr><th>{{call .T "web.dashboard.col_source"}}</th><th>{{call .T "web.dashboard.col_streak"}}</th><th>{{call .T "web.dashboard.col_message"}}</th></tr></thead><tbody>{{range .LastReport.SourceWarnings}}<tr><td><span class="badge">{{.Name}}</span></td><td>{{.ConsecutiveZeros}}</td><td>{{.Message}}</td></tr>{{end}}</tbody></table></div></section>{{end}}{{end}}
{{if .NotificationsError}}<div class="warnbox">{{call .T "web.common.error_prefix"}} {{.NotificationsError}}</div>{{end}}
<section class="section panel"><div class="panel-head"><h2>{{call .T "web.dashboard.last_triggered_alerts"}}</h2></div><div class="table-wrap"><table><thead><tr><th>{{call .T "web.dashboard.col_date"}}</th><th>{{call .T "web.dashboard.col_alert"}}</th><th>{{call .T "web.dashboard.col_product"}}</th><th>{{call .T "web.dashboard.col_price"}}</th><th>{{call .T "web.dashboard.col_eur_tb"}}</th><th>{{call .T "web.dashboard.col_reason"}}</th><th>{{call .T "web.dashboard.col_offer"}}</th></tr></thead><tbody>
{{range .Notifications}}<tr><td>{{tsv .SentAt}}</td><td>{{.AlertName}}</td><td>{{.Title}}</td><td>{{price .PriceEUR}} EUR</td><td>{{price .PricePerTB}}</td><td>{{.Reason}}</td><td><a class="offer-link" href="{{.URL}}" target="_blank" rel="noopener noreferrer">{{call $.T "web.common.view_offer"}} ↗</a></td></tr>{{else}}<tr><td colspan="7" class="empty">{{call $.T "web.dashboard.no_triggered"}}</td></tr>{{end}}
</tbody></table></div></section>
{{end}}`

const qualityTpl = `{{define "body"}}
{{if .Error}}<div class="warnbox">Erreur: {{.Error}}</div>{{end}}
<section class="panel"><div class="panel-head"><h2>Qualite par source</h2></div><div class="table-wrap"><table><thead><tr><th>Source</th><th>Produits</th><th>Observations</th><th>Rejets</th><th>Titre manquant</th><th>Media manquant</th><th>Categorie manquante</th><th>Interfaces manquantes</th><th>EUR/To min</th><th>Median</th><th>Max</th></tr></thead><tbody>
{{if .Quality}}{{range .Quality.Sources}}<tr><td><span class="badge">{{.Source}}</span></td><td>{{.Products}}</td><td>{{.Observations}}</td><td>{{.Rejected}}</td><td>{{.MissingTitle}}</td><td>{{.MissingMedia}}</td><td>{{.MissingCategory}}</td><td>{{.MissingInterfaces}}</td><td>{{flt .MinPricePerTB}}</td><td>{{flt .MedianPricePerTB}}</td><td>{{flt .MaxPricePerTB}}</td></tr>{{else}}<tr><td colspan="11" class="empty">Aucune donnee.</td></tr>{{end}}{{end}}
</tbody></table></div></section>
<section class="section panel"><div class="panel-head"><h2>Top raisons de rejet</h2></div><div class="table-wrap"><table><thead><tr><th>Source</th><th>Raison</th><th>Nombre</th></tr></thead><tbody>
{{if .Quality}}{{range .Quality.Reasons}}<tr><td>{{.Source}}</td><td>{{.Reason}}</td><td>{{.Count}}</td></tr>{{else}}<tr><td colspan="3" class="empty">Aucun rejet.</td></tr>{{end}}{{end}}
</tbody></table></div></section>
{{end}}`

const priceDropsTpl = `{{define "body"}}
<style>
.drops-hero{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:22px 24px;margin-bottom:18px;background:linear-gradient(135deg,var(--nav),var(--nav2));color:#fff;border:1px solid #174f92;border-radius:16px}.drops-hero h2{margin:0 0 4px;font-size:24px}.drops-hero p{margin:0;color:#b9d6f7}.drops-count{font-size:26px;font-weight:900;color:#78bdff;white-space:nowrap}.drops-filter{display:grid;grid-template-columns:1fr 1fr auto;gap:12px;align-items:end;margin-bottom:18px}.drops-filter label{color:var(--muted);font-size:12px;font-weight:750}.drops-filter input{margin-top:5px}.drops-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.drop-card{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:14px;padding:17px;background:var(--panel);border:1px solid var(--line);border-radius:14px}.drop-card h3{margin:0 0 6px;font-size:16px}.drop-card h3 a{text-decoration:none}.drop-card h3 a:hover{color:var(--brand)}.drop-meta{color:var(--muted);font-size:12px}.drop-prices{text-align:right}.drop-current{display:block;color:var(--brand);font-size:24px;font-weight:900}.drop-old{color:var(--muted);text-decoration:line-through}.drop-footer{grid-column:1/-1;display:flex;align-items:center;justify-content:space-between;gap:10px;padding-top:11px;border-top:1px solid var(--line2)}
@media(max-width:820px){.drops-grid{grid-template-columns:1fr}}@media(max-width:560px){.drops-hero{display:block;padding:17px}.drops-count{margin-top:8px;font-size:20px}.drops-filter{grid-template-columns:1fr}.drop-current{font-size:21px}}
</style>
<section class="drops-hero"><div><h2>Baisses de prix</h2><p>Les offres dont le prix actuel vient réellement de diminuer.</p></div><div class="drops-count">{{len .Drops}} baisses</div></section>
<form class="panel panel-body drops-filter" method="get" action="/drops"><label>Période (jours)<input name="days" type="number" min="1" max="365" value="{{.Days}}"></label><label>Baisse minimale %<input name="min_drop" inputmode="decimal" min="0" max="100" step="0.1" value="{{.MinDrop}}"></label><button type="submit">Filtrer</button></form>
{{if .Error}}<p class="warnbox">Impossible de charger les baisses : {{.Error}}</p>{{end}}
<section class="drops-grid">{{range .Drops}}<article class="drop-card"><div><h3><a href="/product?id={{.ProductID}}">{{.Title}}</a></h3><div class="drop-meta">{{.Source}} · {{cap .CapacityTB}} · actualisé {{ago .ObservedAt}}</div></div><div class="drop-prices"><span class="drop-current">{{price .PricePerTB}} €/To</span><span class="drop-old">{{price .PreviousPricePerTB}} €/To</span></div><div class="drop-footer"><span class="badge good">▼ {{price .DropPct}} %</span><span>{{price .PriceEUR}} €</span><a class="offer-link" href="{{.URL}}" target="_blank" rel="noopener noreferrer">Voir l'offre ↗</a></div></article>{{else}}<div class="panel empty">Aucune baisse trouvée avec ces critères.</div>{{end}}</section>
{{end}}`

const marketTpl = `{{define "body"}}
<style>
.market-hero{padding:24px;margin-bottom:18px;background:linear-gradient(135deg,#0758c7,#1677ff);color:#fff;border-radius:16px;border:1px solid #3b9cff}.market-hero h2{margin:0 0 5px;font-size:24px}.market-hero p{margin:0;color:#d7eaff}.market-periods{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:18px}.market-periods a{padding:8px 13px;border:1px solid var(--line);border-radius:999px;text-decoration:none;background:var(--panel);color:var(--brand);font-weight:750}.market-periods a.active{background:var(--brand);color:#fff;border-color:var(--brand)}.market-table{overflow:auto}.market-table table{min-width:560px}.market-value{color:var(--brand);font-weight:850;font-size:17px}
</style>
<section class="market-hero"><h2>Indice quotidien du marché</h2><p>Médiane observée du prix par téraoctet, regroupée par tranche de capacité.</p></section>
<nav class="market-periods" aria-label="Période"><a href="/market?days=7" class="{{if eq .Days 7}}active{{end}}">7 jours</a><a href="/market?days=30" class="{{if eq .Days 30}}active{{end}}">30 jours</a><a href="/market?days=90" class="{{if eq .Days 90}}active{{end}}">90 jours</a><a href="/api/market?days={{.Days}}">JSON</a></nav>
{{if .Error}}<p class="warnbox">Impossible de charger l'indice : {{.Error}}</p>{{end}}
<section class="panel market-table"><table><thead><tr><th>Jour UTC</th><th>Capacité</th><th>Médiane €/To</th><th>Observations</th></tr></thead><tbody>{{range .Market}}<tr><td>{{.Day.Format "02/01/2006"}}</td><td>{{.Band}}</td><td class="market-value">{{price .MedianEUR}} €/To</td><td>{{.Samples}}</td></tr>{{else}}<tr><td colspan="4" class="empty">Aucune observation qualifiée sur cette période.</td></tr>{{end}}</tbody></table></section>
{{end}}`

const europeTpl = `{{define "body"}}
<style>
.eu-hero{padding:24px;margin-bottom:18px;background:linear-gradient(135deg,#071a38,#0758c7);color:#fff;border:1px solid #174f92;border-radius:16px}.eu-hero h2{margin:0 0 5px;font-size:24px}.eu-hero p{margin:0;color:#c7ddf8}.eu-regions{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:10px;margin-bottom:18px}.eu-region{display:block;padding:14px;background:var(--panel);border:1px solid var(--line);border-radius:12px;text-decoration:none}.eu-region.active,.eu-region:hover{border-color:var(--brand)}.eu-region strong{display:block;font-size:17px}.eu-region span{color:var(--muted);font-size:12px}.eu-region b{display:block;margin-top:5px;color:var(--brand)}.eu-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.eu-offer{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;padding:16px;background:var(--panel);border:1px solid var(--line);border-radius:14px}.eu-offer h3{margin:0 0 6px;font-size:15px}.eu-offer h3 a{text-decoration:none}.eu-meta{color:var(--muted);font-size:12px}.eu-price{text-align:right;color:var(--brand);font-size:23px;font-weight:900}.eu-price small{display:block;color:var(--muted);font-size:11px}.eu-foot{grid-column:1/-1;display:flex;justify-content:space-between;gap:10px;padding-top:10px;border-top:1px solid var(--line2)}
@media(max-width:780px){.eu-grid{grid-template-columns:1fr}}@media(max-width:520px){.eu-hero{padding:17px}.eu-hero h2{font-size:20px}.eu-regions{grid-template-columns:repeat(2,minmax(0,1fr))}.eu-price{font-size:20px}}
</style>
<section class="eu-hero"><h2>Comparaison européenne</h2><p>Prix observés par boutique nationale, classés au coût par téraoctet.</p></section>
<div class="warnbox">Les frais de port et les restrictions de livraison ne sont pas inclus. Vérifiez le total chez le marchand.</div>
{{if .Error}}<div class="warnbox">Impossible de charger la comparaison : {{.Error}}</div>{{end}}
<nav class="eu-regions" aria-label="Pays"><a class="eu-region {{if eq .SelectedCountry ""}}active{{end}}" href="/europe"><strong>🇪🇺 Toute l'Europe</strong><span>Comparer les pays</span></a>{{range .Regions}}<a class="eu-region {{if eq $.SelectedCountry .Country.Code}}active{{end}}" href="/europe?country={{.Country.Code}}"><strong>{{.Country.Flag}} {{.Country.Name}}</strong><span>{{.Offers}} offres</span><b>Dès {{price .BestPricePerTB}} €/To</b></a>{{end}}</nav>
<section class="eu-grid">{{range .Offers}}<article class="eu-offer"><div><h3><a href="/product?id={{.ProductID}}">{{.Title}}</a></h3><div class="eu-meta">{{.Country.Flag}} {{.Country.Name}} · {{.Source}} · {{cap .CapacityTB}}</div></div><div class="eu-price">{{price .PriceEUR}} €<small>{{price .PricePerTB}} €/To</small></div><div class="eu-foot"><span>Actualisé {{ago .ObservedAt}}</span><a class="offer-link" href="{{.URL}}" target="_blank" rel="noopener noreferrer">Voir l'offre ↗</a></div></article>{{else}}<div class="panel empty">Aucune offre fiable pour ce pays.</div>{{end}}</section>
{{end}}`

const productsTpl = `{{define "body"}}
<style>
.drive-photo{width:100%;height:126px;object-fit:contain;background:var(--soft);border-radius:11px;border:1px solid var(--line)}
.catalog-hero{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:18px 20px;margin-bottom:16px;background:var(--nav);color:#fff;border:3px solid var(--pixel)}.catalog-hero h2{margin:0 0 4px;font-size:14px;font-family:"Press Start 2P",monospace;line-height:1.4}.catalog-hero p{margin:0;color:#b9d6f7}.catalog-count{font-size:14px;font-weight:700;color:#78bdff;white-space:nowrap;font-family:"Press Start 2P",monospace}.catalog-layout{display:grid;grid-template-columns:280px minmax(0,1fr);gap:18px;align-items:start}.filter-drawer{position:sticky;top:84px}.filter-head{display:flex;align-items:center;justify-content:space-between;margin-bottom:18px}.filter-head h2{margin:0;font-size:19px}.filter-close{display:none;font-size:28px;text-decoration:none}.filter-form{display:grid;gap:14px}.filter-form label{display:block;margin-bottom:5px;color:var(--muted);font-size:12px;font-weight:700;text-transform:uppercase;letter-spacing:.04em}.filter-range{display:grid;grid-template-columns:1fr 1fr;gap:9px}.filter-actions{display:grid;grid-template-columns:1fr auto;gap:8px;margin-top:4px}.filter-reset{display:flex;align-items:center;padding:0 8px;color:var(--brand);font-weight:700;text-decoration:none}.catalog-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:12px}.catalog-toolbar h2{margin:0;font-size:17px}.product-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.product-card{display:grid;grid-template-columns:112px minmax(0,1fr);gap:15px;padding:14px;background:var(--panel);border:2px solid var(--line);border-radius:0;transition:border-color .1s step-end}.product-card:hover{border-color:var(--brand)}.drive-visual{min-height:126px;display:grid;place-content:center;text-align:center;border-radius:0;background:var(--soft);color:var(--brand);border:2px solid var(--line)}.drive-visual span{font-size:22px;font-weight:900;letter-spacing:.08em;font-family:"Press Start 2P",monospace;font-size:12px}.drive-visual small{font-weight:800}.product-copy{min-width:0}.product-title{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--ink);font-size:15px;font-weight:800;text-decoration:none}.product-title:hover{color:var(--brand)}.product-price{margin:10px 0 7px;color:var(--brand);font-size:22px;font-weight:900}.product-price small{color:var(--muted);font-size:12px;font-weight:700}.tag-row{display:flex;gap:6px;flex-wrap:wrap}.storage-tag{display:inline-flex;border-radius:0;padding:4px 8px;border:2px solid var(--line);background:var(--soft);color:var(--brand);font-size:11px;font-weight:750}.storage-tag.strong{background:var(--brand);color:#fff;border-color:var(--brand2)}.storage-tag.unavailable{background:rgba(232,121,114,.12);color:var(--bad);border-color:var(--bad)}.product-meta{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-top:11px;color:var(--muted);font-size:12px}.product-meta a{color:var(--brand);font-weight:800;text-decoration:none}.trend{margin-left:auto}.mobile-filter-button{display:none}
:root[data-theme=dark] .drive-visual{background:linear-gradient(145deg,#102c51,#07172d);color:#78bdff;border-color:#214b78}
@media(max-width:1180px){.product-grid{grid-template-columns:1fr}}
@media(max-width:760px){main{padding-bottom:94px}.catalog-hero{align-items:flex-start;padding:17px}.catalog-hero h2{font-size:20px}.catalog-count{font-size:20px}.catalog-layout{display:block}.filter-drawer{display:none;position:fixed;inset:0;z-index:20;overflow:auto;border-radius:0;padding:22px;background:var(--bg)}.filter-drawer:target{display:block}.filter-close{display:block}.filter-form{padding:18px;background:var(--panel);border:1px solid var(--line);border-radius:14px}.catalog-toolbar .button{display:none}.product-card{grid-template-columns:96px minmax(0,1fr);padding:12px}.drive-visual{min-height:112px}.product-title{white-space:normal;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical}.product-price{font-size:22px}.product-meta{align-items:flex-end}.mobile-filter-button{display:flex;position:fixed;left:16px;right:16px;bottom:14px;z-index:15;min-height:58px;align-items:center;justify-content:center;border-radius:13px;background:var(--brand);color:#fff;text-decoration:none;font-size:18px;font-weight:850;box-shadow:0 12px 35px rgba(0,65,160,.35)}}
@media(max-width:420px){.catalog-hero{display:block}.catalog-count{margin-top:10px}.product-card{grid-template-columns:82px minmax(0,1fr);gap:11px}.drive-visual{min-height:100px}.drive-visual span{font-size:22px}.storage-tag:nth-of-type(n+5){display:none}}
</style>
{{if .Error}}<div class="warnbox">Erreur: {{.Error}}</div>{{end}}
<section id="catalog" class="catalog-hero"><div><h2>Le meilleur du stockage, au bon prix</h2><p>HDD, SSD et NVMe classés par coût réel au téraoctet.</p></div><div class="catalog-count">{{.Total}} produits</div></section>
<div class="catalog-layout">
<aside id="filters" class="panel panel-body filter-drawer"><div class="filter-head"><h2>Filtres</h2><a class="filter-close" href="#catalog" aria-label="Fermer les filtres">×</a></div>
<form method="get" action="/products" class="filter-form">
<div><label for="filter_q">Rechercher</label><input id="filter_q" name="q" value="{{.Query}}" placeholder="Exos, IronWolf, NVMe..."></div>
<div><label for="filter_source">Marchand</label><select id="filter_source" name="source"><option value="">Tous</option>{{range .Sources}}<option value="{{.}}" {{if eq $.SelectedSource .}}selected{{end}}>{{.}}</option>{{end}}</select></div>
<div><label for="filter_media">Support</label><select id="filter_media" name="media"><option value="">HDD et SSD</option><option value="rotational" {{if eq .SelectedMedia "rotational"}}selected{{end}}>HDD</option><option value="solid_state" {{if eq .SelectedMedia "solid_state"}}selected{{end}}>SSD</option></select></div>
<div><label for="filter_condition">État</label><select id="filter_condition" name="condition"><option value="">Tous</option><option value="new" {{if eq .SelectedCondition "new"}}selected{{end}}>Neuf</option><option value="used" {{if eq .SelectedCondition "used"}}selected{{end}}>Occasion</option></select></div>
<div><label for="filter_availability">Disponibilité</label><select id="filter_availability" name="availability"><option value="">Toutes</option><option value="available" {{if eq .SelectedAvailability "available"}}selected{{end}}>Disponible</option><option value="unavailable" {{if eq .SelectedAvailability "unavailable"}}selected{{end}}>Indisponible</option></select></div>
<div><label for="filter_brand">Marque</label><select id="filter_brand" name="brand"><option value="">Toutes</option>{{range .Brands}}<option value="{{.}}" {{if eq $.SelectedBrand .}}selected{{end}}>{{.}}</option>{{end}}</select></div>
<div><label for="filter_category">{{call $.T "web.products.form_factor"}}</label><select id="filter_category" name="category"><option value="">{{call $.T "web.common.all"}}</option>{{range .Categories}}<option value="{{.}}" {{if eq $.SelectedCategory .}}selected{{end}}>{{.}}</option>{{end}}</select></div>
<div><label for="filter_interface">Interface</label><select id="filter_interface" name="interface"><option value="">Toutes</option>{{range .Interfaces}}<option value="{{.}}" {{if eq $.SelectedInterface .}}selected{{end}}>{{.}}</option>{{end}}</select></div>
<div><label for="filter_recording">Enregistrement</label><select id="filter_recording" name="recording"><option value="">Tous</option>{{range .Recordings}}<option value="{{.}}" {{if eq $.SelectedRecording .}}selected{{end}}>{{.}}</option>{{end}}</select></div>
<div><label>Capacité</label><div class="filter-range"><input aria-label="Capacité minimale" name="min_tb" value="{{.MinTB}}" inputmode="decimal" placeholder="Min To"><input aria-label="Capacité maximale" name="max_tb" value="{{.MaxTB}}" inputmode="decimal" placeholder="Max To"></div></div>
<div><label for="filter_max_eur_tb">Prix maximum</label><input id="filter_max_eur_tb" name="max_eur_tb" value="{{.MaxPrice}}" inputmode="decimal" placeholder="EUR / To"></div>
<div><label for="filter_sort">Trier par</label><select id="filter_sort" name="sort"><option value="eur_tb" {{if eq .Sort "eur_tb"}}selected{{end}}>Prix/To</option><option value="price" {{if eq .Sort "price"}}selected{{end}}>Prix</option><option value="freshness" {{if eq .Sort "freshness"}}selected{{end}}>Fraîcheur</option><option value="sellers" {{if eq .Sort "sellers"}}selected{{end}}>Vendeurs</option></select></div><label class="check-chip"><input type="checkbox" name="ungrouped" value="1" {{if .UngroupedSelected}}checked{{end}}> Offres non référencées</label><div class="filter-actions"><button type="submit">Afficher les produits</button><a class="filter-reset" href="/products">Réinitialiser</a></div>
</form></aside>
<section><div class="catalog-toolbar"><div><h2>Produits référencés</h2><span class="hint">Prix et fraîcheur vérifiés lors du dernier scan.</span></div><a class="button" href="/alerts">Créer une alerte</a></div>
<div class="product-grid">{{range .Groups}}{{$pts := index $.Sparklines .BestProductID}}{{$spark := Sparkline $pts}}
<article class="product-card"><div>{{if .ImageURL}}<img class="drive-photo" src="{{ptr .ImageURL}}" alt="" referrerpolicy="no-referrer" loading="lazy" onerror="this.style.display='none';this.nextElementSibling.style.display='grid'">{{end}}<div class="drive-visual" {{if .ImageURL}}style="display:none"{{end}}><span>{{mediaLabel .MediaType}}</span><small>{{cap .CapacityTB}}</small></div></div><div class="product-copy">
<a class="product-title" href="/product?key={{urlquery .CanonicalKey}}">{{.Brand}} {{.Model}} · {{cap .CapacityTB}}</a>{{if .EAN}}<div class="hint">EAN : {{ptr .EAN}}</div>{{end}}{{if .SKU}}<div class="hint">SKU : {{ptr .SKU}}</div>{{end}}
<div class="product-price">{{price .BestPriceEUR}} € <small>{{price .BestPricePerTB}} €/To</small></div>
<div class="tag-row"><span class="storage-tag strong">{{.OfferCount}} vendeurs</span><span class="storage-tag {{if eq (printf "%s" .Availability) "unavailable"}}unavailable{{end}}">{{if eq (printf "%s" .Availability) "unavailable"}}Indisponible{{else}}Disponible{{end}}</span><span class="storage-tag">{{mediaLabel .MediaType}}</span><span class="storage-tag">{{.Brand}}</span>{{if .DriveCategory}}<span class="storage-tag">{{ptr .DriveCategory}}</span>{{end}}{{if .RecordingMethod}}<span class="storage-tag">{{ptr .RecordingMethod}}</span>{{end}}{{range .Interfaces}}<span class="storage-tag">{{.}}</span>{{end}}</div>
<div class="product-meta"><span>Actualisé {{ago .ObservedAt}}</span>{{if $spark.Coords}}<svg class="sparkline trend" viewBox="0 0 80 24" preserveAspectRatio="none" aria-label="Tendance 7 jours ({{$spark.Trend}})"><polyline fill="none" stroke-width="1.5" {{if eq $spark.Trend "down"}}stroke="#4ec78a"{{else if eq $spark.Trend "up"}}stroke="#e87972"{{else}}stroke="#91a4bf"{{end}} points="{{$spark.Coords}}"/></svg>{{end}}</div>
</div></article>{{else}}<div class="panel empty">Aucun produit ne correspond aux filtres.</div>{{end}}</div>
{{if .UngroupedSelected}}<h2>Offres non référencées</h2><div class="product-grid">{{range .Ungrouped}}<article class="product-card"><div class="product-copy"><a class="product-title" href="/product?id={{.ProductID}}">{{.Title}}</a><div class="hint">{{.Source}} · {{cap .CapacityTB}}</div><div class="product-price">{{price .PriceEUR}} € <small>{{price .PricePerTB}} €/To</small></div><div class="product-meta"><span>Actualisé {{ago .ObservedAt}}</span><a href="{{.URL}}" target="_blank" rel="noopener noreferrer">Voir l'offre ↗</a></div></div></article>{{end}}</div>{{end}}
{{if gt .Pages 1}}<nav class="range-links" aria-label="Pagination">{{range .PageLinks}}<a href="{{.URL}}" {{if eq $.Page .Number}}class="active"{{end}}>{{.Number}}</a>{{end}}</nav>{{end}}</section>
</div><a class="mobile-filter-button" href="#filters">⌁ &nbsp; Filtres</a>
{{end}}`

const productDetailTpl = `{{define "body"}}
<style>
.drive-photo{width:100%;height:180px;object-fit:contain;background:var(--soft);border-radius:14px;border:1px solid var(--line)}
.detail-back{display:inline-block;margin-bottom:14px;color:var(--muted);font-weight:700;text-decoration:none}.detail-hero{display:grid;grid-template-columns:180px minmax(0,1fr);gap:24px;padding:24px;background:linear-gradient(135deg,var(--panel),var(--soft));border:1px solid var(--line);border-radius:16px}.detail-visual{min-height:180px;display:grid;place-content:center;text-align:center;border-radius:14px;background:linear-gradient(145deg,#eef6ff,#cfe4ff);color:#0758c7;border:1px solid #c1dcff}.detail-visual span{font-size:44px;font-weight:950;letter-spacing:.08em}.detail-visual small{font-size:16px;font-weight:800}.detail-copy h2{margin:0 0 9px;font-size:25px;line-height:1.25}.detail-tags{display:flex;gap:7px;flex-wrap:wrap;margin:12px 0 18px}.detail-tag{border-radius:999px;padding:5px 10px;background:var(--panel);border:1px solid var(--line);color:var(--muted);font-size:12px;font-weight:750}.detail-actions{display:flex;gap:9px;flex-wrap:wrap}.detail-actions .secondary-link{background:var(--panel);color:var(--brand);border:1px solid var(--line)}.detail-layout{display:grid;grid-template-columns:minmax(0,1.1fr) minmax(320px,.9fr);gap:18px;margin-top:18px;align-items:start}.compare-panel{padding:22px}.compare-panel h2{margin:0;color:var(--brand);font-size:22px}.compare-sub{margin:4px 0 18px;color:var(--muted)}.offer-card{display:grid;grid-template-columns:minmax(0,1fr) auto 48px;gap:15px;align-items:center;padding:18px;border:1px solid var(--line);border-radius:14px;background:var(--panel);box-shadow:0 10px 28px rgba(0,29,72,.08)}.offer-card+.offer-card{margin-top:12px}.merchant-name{font-size:19px;font-weight:900;text-transform:capitalize}.merchant-title{display:block;max-width:340px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted);font-size:11px}.offer-state{display:inline-flex;margin-top:8px;padding:4px 9px;border-radius:999px;background:rgba(78,199,138,.12);color:var(--good);font-size:12px;font-weight:800}.offer-state.unavailable{background:rgba(232,121,114,.12);color:var(--bad)}.offer-price{text-align:right}.offer-price strong{display:block;color:var(--brand);font-size:28px}.offer-price span{color:var(--muted);font-size:12px}.offer-go{width:46px;height:46px;display:grid;place-content:center;border-radius:999px;background:var(--brand);color:#fff;font-size:28px;text-decoration:none}.spec-list{display:grid;grid-template-columns:1fr 1fr;gap:1px;background:var(--line)}.spec{padding:14px;background:var(--panel)}.spec span{display:block;color:var(--muted);font-size:11px;text-transform:uppercase}.spec strong{display:block;margin-top:4px}.history-panel{margin-top:18px}.history-scroll{max-height:330px;overflow:auto}:root[data-theme=dark] .detail-visual{background:linear-gradient(145deg,#102c51,#07172d);color:#78bdff;border-color:#214b78}
@media(max-width:900px){.detail-layout{grid-template-columns:1fr}}
@media(max-width:620px){.detail-hero{grid-template-columns:100px minmax(0,1fr);gap:14px;padding:14px}.detail-visual{min-height:115px}.detail-visual span{font-size:28px}.detail-copy h2{font-size:18px}.detail-actions{display:grid}.detail-actions .button{width:100%}.compare-panel{padding:17px}.offer-card{grid-template-columns:1fr auto;gap:10px}.offer-go{grid-column:2;grid-row:1 / span 2}.offer-price{text-align:left}.offer-price strong{font-size:25px}.spec-list{grid-template-columns:1fr}.stats-row{grid-template-columns:1fr}.stat .value{font-size:23px}}
</style>
{{if .Error}}<div class="warnbox">Erreur: {{.Error}}</div>{{end}}
{{if .Product}}<a class="detail-back" href="/products">← Retour aux produits</a>
<section class="detail-hero"><div>{{if .Product.ImageURL}}<img class="drive-photo" src="{{ptr .Product.ImageURL}}" alt="" referrerpolicy="no-referrer" loading="lazy" onerror="this.style.display='none';this.nextElementSibling.style.display='grid'">{{end}}<div class="detail-visual" {{if .Product.ImageURL}}style="display:none"{{end}}><span>{{mediaLabel .Product.MediaType}}</span><small>{{cap .Product.CapacityTB}}</small></div></div><div class="detail-copy"><h2>{{ptr .Product.Brand}} {{ptr .Product.Model}} · {{cap .Product.CapacityTB}}</h2>{{if .Product.EAN}}<div class="hint">EAN : {{ptr .Product.EAN}}</div>{{end}}{{if .Product.SKU}}<div class="hint">SKU : {{ptr .Product.SKU}}</div>{{end}}<div class="hint">Référence suivie chez {{.Product.Source}} · actualisée {{ago .Product.LastSeenAt}}</div><div class="detail-tags"><span class="detail-tag">{{mediaLabel .Product.MediaType}}</span><span class="detail-tag">{{if eq (printf "%s" .Product.Availability) "unavailable"}}Indisponible{{else}}Disponible{{end}}</span>{{if .Product.Brand}}<span class="detail-tag">{{ptr .Product.Brand}}</span>{{end}}{{if .Product.DriveCategory}}<span class="detail-tag">{{ptr .Product.DriveCategory}}</span>{{end}}{{if .Product.RecordingMethod}}<span class="detail-tag">{{ptr .Product.RecordingMethod}}</span>{{end}}{{range .Product.Interfaces}}<span class="detail-tag">{{.}}</span>{{end}}</div><div class="detail-actions"><a class="button" href="/alerts?name={{urlquery (printf "%s %s" (ptr .Product.Brand) (ptr .Product.Model))}}&keywords={{urlquery (printf "%s,%s" (ptr .Product.Brand) (ptr .Product.Model))}}">♧ &nbsp; Créer une alerte de prix</a><a class="button secondary-link" href="{{.Product.URL}}" target="_blank" rel="noopener noreferrer">Voir chez le marchand ↗</a></div></div></section>
<div class="detail-layout"><section class="panel compare-panel"><h2>Comparaison des prix</h2><p class="compare-sub">{{if gt (len .Offers) 1}}{{len .Offers}} offres regroupées par marque, modèle et capacité.{{else}}Dernière offre disponible pour cette référence.{{end}}</p>{{if .Offers}}{{range .Offers}}<article class="offer-card"><div><div class="merchant-name">{{.Source}}</div><span class="merchant-title">{{.Title}}</span><span class="offer-state {{if eq (printf "%s" .Availability) "unavailable"}}unavailable{{end}}">{{if eq (printf "%s" .Availability) "unavailable"}}Indisponible{{else}}✓ {{conditionLabel .Condition}}{{end}}</span></div><div class="offer-price"><strong>{{price .PriceEUR}} €</strong><span>{{price .PricePerTB}} €/To · {{ago .ObservedAt}}</span></div><a class="offer-go" href="{{.URL}}" target="_blank" rel="noopener noreferrer" aria-label="Voir l'offre chez {{.Source}}">›</a></article>{{end}}{{else if .Current}}<article class="offer-card"><div><div class="merchant-name">{{.Product.Source}}</div><span class="offer-state {{if eq (printf "%s" .Product.Availability) "unavailable"}}unavailable{{end}}">{{if eq (printf "%s" .Product.Availability) "unavailable"}}Indisponible{{else}}✓ {{conditionLabel .Product.Condition}}{{end}}</span></div><div class="offer-price"><strong>{{price .Current.PriceEUR}} €</strong><span>{{price .Current.PricePerTB}} €/To · {{ago .Current.ObservedAt}}</span></div><a class="offer-go" href="{{.Product.URL}}" target="_blank" rel="noopener noreferrer" aria-label="Voir l'offre chez {{.Product.Source}}">›</a></article>{{else}}<p class="empty">Aucun prix récent disponible.</p>{{end}}</section>
<section class="panel"><div class="panel-head"><h2>Caractéristiques</h2></div><div class="spec-list"><div class="spec"><span>Capacité</span><strong>{{cap .Product.CapacityTB}}</strong></div><div class="spec"><span>EAN</span><strong>{{ptr .Product.EAN}}</strong></div><div class="spec"><span>SKU</span><strong>{{ptr .Product.SKU}}</strong></div><div class="spec"><span>Modèle</span><strong>{{ptr .Product.Model}}</strong></div><div class="spec"><span>Support</span><strong>{{mediaLabel .Product.MediaType}}</strong></div><div class="spec"><span>Marque</span><strong>{{ptr .Product.Brand}}</strong></div><div class="spec"><span>État</span><strong>{{conditionLabel .Product.Condition}}</strong></div><div class="spec"><span>Disponibilité</span><strong>{{if eq (printf "%s" .Product.Availability) "unavailable"}}Indisponible{{else}}Disponible{{end}}</strong></div><div class="spec"><span>Disponibilité vérifiée</span><strong>{{tsv .Product.AvailabilityUpdatedAt}}</strong></div><div class="spec"><span>Première observation</span><strong>{{tsv .Product.FirstSeenAt}}</strong></div><div class="spec"><span>Dernier refresh</span><strong>{{tsv .Product.LastSeenAt}}</strong></div></div></section></div>
<section class="panel history-panel"><div class="panel-head"><h2>Historique de prix ({{.Days}} jours)</h2><div class="range-links"><a href="/product?{{if .CanonicalKey}}key={{urlquery .CanonicalKey}}{{else}}id={{.Product.ID}}{{end}}&days=7" {{if eq .Days 7}}class="active"{{end}}>7j</a><a href="/product?{{if .CanonicalKey}}key={{urlquery .CanonicalKey}}{{else}}id={{.Product.ID}}{{end}}&days=30" {{if eq .Days 30}}class="active"{{end}}>30j</a><a href="/product?{{if .CanonicalKey}}key={{urlquery .CanonicalKey}}{{else}}id={{.Product.ID}}{{end}}&days=90" {{if eq .Days 90}}class="active"{{end}}>90j</a><a href="/product?{{if .CanonicalKey}}key={{urlquery .CanonicalKey}}{{else}}id={{.Product.ID}}{{end}}&days=365" {{if eq .Days 365}}class="active"{{end}}>1 an</a></div></div>
{{if .History}}<div class="stats-row"><div class="stat"><span class="label">Minimum EUR/To</span><span class="value">{{price .MinPT}}</span></div><div class="stat"><span class="label">Moyenne EUR/To</span><span class="value">{{price .AvgPT}}</span></div><div class="stat"><span class="label">Maximum EUR/To</span><span class="value">{{price .MaxPT}}</span></div></div><div class="chart-wrap">{{if .ChartPoints}}<svg class="price-chart" viewBox="0 0 800 200" preserveAspectRatio="none" aria-label="Évolution du prix au téraoctet"><polyline fill="none" stroke="#3b9cff" stroke-width="2" points="{{.ChartPoints}}"/></svg>{{else}}<p class="empty">Pas assez de données pour un graphique.</p>{{end}}</div><div class="table-wrap history-scroll"><table><thead><tr><th>Date</th><th>Prix</th><th>EUR/To</th><th>Source</th></tr></thead><tbody>{{range .History}}<tr><td>{{tsv .ObservedAt}}</td><td>{{price .PriceEUR}} EUR</td><td>{{price .PricePerTB}}</td><td>{{.Source}}</td></tr>{{end}}</tbody></table></div>{{else}}<p class="empty">Aucune observation sur cette période.</p>{{end}}</section>
{{else}}<p class="empty">Produit introuvable. <a href="/products">Retour aux produits</a></p>{{end}}
{{end}}`

const alertsTpl = `{{define "body"}}
{{if .Saved}}<div class="notice">Alertes mises a jour.</div>{{end}}
{{if .Error}}<div class="warnbox">Erreur: {{.Error}}</div>{{end}}
<section class="panel"><div class="panel-head"><div><h2>Créer une alerte</h2><div class="hint">Tous les critères sont gérés ici. Discord reste optionnel pour la diffusion.</div></div></div><div class="panel-body">
<form method="post" action="/alerts/add" class="alert-form">
<div class="alert-grid"><div><label for="alert_name">Nom</label><input id="alert_name" name="name" value="{{.PrefillName}}" required placeholder="NAS 20 To"></div><div><label for="alert_price">Prix max €/To</label><input id="alert_price" name="max_price_per_tb" inputmode="decimal" placeholder="20"></div><div><label for="alert_discount">Baisse minimale %</label><input id="alert_discount" name="min_discount_pct" type="number" min="0" max="100" step="0.1" value="5"></div><div><label for="alert_cooldown">Délai entre alertes (h)</label><input id="alert_cooldown" name="cooldown_hours" type="number" min="0" value="24"></div><div><label for="alert_keywords">Mots inclus</label><input id="alert_keywords" name="keywords" value="{{.PrefillKeywords}}" placeholder="IronWolf, Exos"></div><div><label for="alert_exclude">Mots exclus</label><input id="alert_exclude" name="exclude_keywords" placeholder="reconditionné"></div><div><label class="check-chip"><input type="checkbox" name="discord_enabled" value="1" {{if not .Status.DiscordConfigured}}disabled{{end}}> Diffuser cette alerte sur Discord</label>{{if not .Status.DiscordConfigured}}<div class="hint">Disponible après la configuration finale du bot.</div>{{end}}</div></div>
<div><div class="label">Support</div><div class="check-grid"><label class="check-chip"><input type="checkbox" name="media" value="rotational"> HDD</label><label class="check-chip"><input type="checkbox" name="media" value="solid_state"> SSD</label></div></div>
<div><div class="label">État</div><div class="check-grid"><label class="check-chip"><input type="checkbox" name="condition" value="new"> Neuf</label><label class="check-chip"><input type="checkbox" name="condition" value="used"> Occasion</label></div></div>
<div><div class="label">Capacité</div><div class="check-grid">{{range $key, $preset := .CapacityPresets}}<label class="check-chip"><input type="checkbox" name="capacity" value="{{$key}}"> {{$preset.Label}}</label>{{end}}</div></div>
<div><div class="label">{{call .T "web.alerts.sources"}}</div><div class="check-grid">{{range .Sources}}<label class="check-chip"><input type="checkbox" name="source" value="{{.}}"> {{.}}</label>{{end}}</div></div>
<div><div class="label">{{call .T "web.alerts.brands"}}</div><div class="check-grid">{{range .BrandOptions}}<label class="check-chip"><input type="checkbox" name="brand" value="{{.}}"> {{.}}</label>{{end}}</div></div>
<div><div class="label">{{call .T "web.alerts.interfaces"}}</div><div class="check-grid">{{range .InterfaceOptions}}<label class="check-chip"><input type="checkbox" name="interface" value="{{.}}"> {{.}}</label>{{end}}</div></div>
<div><div class="label">{{call .T "web.alerts.recording"}}</div><div class="check-grid">{{range .RecordingOptions}}<label class="check-chip"><input type="checkbox" name="recording" value="{{.}}"> {{.}}</label>{{end}}</div></div>
<div><div class="label">{{call .T "web.alerts.categories"}}</div><div class="check-grid">{{range .CategoryOptions}}<label class="check-chip"><input type="checkbox" name="category" value="{{.}}"> {{.}}</label>{{end}}</div></div>
<div><button type="submit">{{call .T "web.alerts.submit"}}</button></div></form>
</div></section>
<section class="panel"><div class="panel-head"><h2>Alertes existantes</h2></div><div class="table-wrap"><table><thead><tr><th>Nom</th><th>État</th><th>Discord</th><th>Capacités</th><th>Media</th><th>Prix max</th><th>Actions</th></tr></thead><tbody>
{{range .Alerts}}<tr><td>{{.Name}}</td><td>{{if .Enabled}}<span class="badge good">active</span>{{else}}<span class="badge warn">inactive</span>{{end}}</td><td>{{if .DiscordEnabled}}<span class="badge good">coché</span>{{else}}<span class="badge">non</span>{{end}}</td><td>{{csv .CapacityPresets}}</td><td>{{csv .MediaTypes}}</td><td>{{alertPrice .}}</td><td><div class="actions">
<form class="inline" method="post" action="/alerts/toggle"><input type="hidden" name="alert_id" value="{{.ID}}">{{if .Enabled}}<input type="hidden" name="enabled" value="0"><button class="secondary" type="submit" aria-label="Mettre en pause l'alerte {{.Name}}">Pause</button>{{else}}<input type="hidden" name="enabled" value="1"><button type="submit" aria-label="Reprendre l'alerte {{.Name}}">Reprendre</button>{{end}}</form>
<form class="inline" method="post" action="/alerts/delete" onsubmit="return confirm('Supprimer cette alerte ?')"><input type="hidden" name="alert_id" value="{{.ID}}"><input type="hidden" name="confirm" value="delete"><button class="danger" type="submit" aria-label="Supprimer l'alerte {{.Name}}">Supprimer</button></form>
</div></td></tr>{{else}}<tr><td colspan="7" class="empty">Aucune alerte.</td></tr>{{end}}
</tbody></table></div></section>
{{end}}`

const configTpl = `{{define "body"}}
{{if .Saved}}<div class="notice">{{call .T "web.common.config_saved"}}</div>{{end}}
{{if .Error}}<div class="warnbox">{{call .T "web.common.error_prefix"}} {{.Error}}</div>{{end}}
<div class="warnbox">{{.RestartMsg}}</div>
<div class="notice">{{call .T "web.config.merchants_hint"}} <a class="offer-link" href="/sites">{{call .T "web.nav.sites"}}</a></div>
<form method="post" action="/config/save">
{{range .Sections}}
<section class="panel config-section"><div class="panel-head"><h2>{{call $.T (printf "web.config.section_%s" .Key)}}</h2>{{if eq .Key "essential"}}<button type="submit">{{call $.T "web.common.save"}}</button>{{end}}</div>
{{if eq .Key "merchants"}}<details><summary>{{call $.T "web.config.show_advanced_urls"}}</summary>{{end}}
{{range .Rows}}<div class="config-row"><div><label for="{{.Meta.Key}}">{{.Meta.Key}}</label><div class="hint">{{.Meta.Label}}</div></div><div>{{if .Meta.Secret}}<input id="{{.Meta.Key}}" name="{{.Meta.Key}}" type="password" placeholder="********"><div class="hint">{{call $.T "web.config.replace"}}</div>{{else}}<input id="{{.Meta.Key}}" name="{{.Meta.Key}}" type="text" value="{{.Value}}">{{end}}</div><div>{{if .Meta.Secret}}<label><input type="checkbox" name="replace_{{.Meta.Key}}" value="1"> {{call $.T "web.config.replace"}}</label>{{else if .Meta.RestartRequired}}<span class="badge warn">{{call $.T "web.config.restart_badge"}}</span>{{end}}</div></div>{{end}}
{{if eq .Key "merchants"}}</details>{{end}}
</section>
{{end}}
<div style="margin-top:14px"><button type="submit">{{call .T "web.common.save"}}</button></div>
</form>
{{end}}`

const metricsTpl = `{{define "body"}}
{{if not .Report}}<div class="warnbox">Aucun scan n'a encore eu lieu.</div>{{else}}
<div class="grid">
<div class="card"><div class="label">Dernier scan</div><div class="value" style="font-size:18px">{{tsv .Report.FinishedAt}}</div></div>
<div class="card"><div class="label">Fetched</div><div class="value">{{.Report.Fetched}}</div></div>
<div class="card"><div class="label">Accepted</div><div class="value">{{.Report.Accepted}}</div></div>
<div class="card"><div class="label">Rejected</div><div class="value">{{.Report.Rejected}}</div></div>
</div>
<div class="section grid">
<div class="card"><div class="label">Matched</div><div class="value">{{.Report.Matched}}</div></div>
<div class="card"><div class="label">Notified</div><div class="value">{{.Report.Notified}}</div></div>
<div class="card"><div class="label">Errors</div><div class="value">{{len .Report.Errors}}</div></div>
<div class="card"><div class="label">Breaker skips</div><div class="value">{{len .Report.BreakerSkips}}</div></div>
</div>
{{end}}
<section class="section panel"><div class="panel-head"><h2>Sante des sources</h2></div><div class="table-wrap"><table><thead><tr><th>Source</th><th>Etat</th><th>Action</th></tr></thead><tbody>
{{range $name, $state := .Breakers}}<tr><td>{{$name}}</td><td>{{if eq $state "closed"}}<span class="badge good">closed</span>{{else if eq $state "half-open"}}<span class="badge warn">half-open</span>{{else}}<span class="badge bad">open</span>{{end}}</td><td><form class="inline" method="post" action="/api/sources/breaker/reset"><input type="hidden" name="name" value="{{$name}}"><button class="secondary" type="submit" aria-label="{{call $.T "web.common.reset"}} {{$name}}">{{call $.T "web.common.reset"}}</button></form></td></tr>{{else}}<tr><td colspan="3" class="empty">{{call .T "web.common.no_breaker"}}</td></tr>{{end}}
</tbody></table></div></section>
<section class="section panel"><div class="panel-head"><h2>Metriques par source (dernier scan)</h2></div><div class="table-wrap"><table><thead><tr><th>Source</th><th>Deals</th><th>Breaker</th><th>Erreur</th></tr></thead><tbody>
{{if .Report}}{{range .Report.SourceMetrics}}<tr><td>{{.Name}}</td><td>{{.DealsFetched}}</td><td>{{.BreakerState}}</td><td>{{.Error}}</td></tr>{{else}}<tr><td colspan="4" class="empty">Aucune metrique.</td></tr>{{end}}{{else}}<tr><td colspan="4" class="empty">Aucun scan.</td></tr>{{end}}
</tbody></table></div></section>
{{end}}`

const discordTpl = `{{define "body"}}
{{if .Saved}}<div class="notice">Configuration Discord sauvegardée et appliquée.</div>{{end}}
{{if .Tested}}<div class="notice">Message de test Discord envoyé.</div>{{end}}
{{if .Error}}<div class="warnbox">Erreur: {{.Error}}</div>{{end}}
<div class="notice">Intégration prête mais laissée en attente. Configurez le bot seulement après validation complète du suivi des produits et des alertes.</div>
<section class="panel"><div class="panel-head"><div><h2>Bot Discord de diffusion</h2><div class="hint">Le bot publie uniquement les alertes créées dans DiskCount dont la case Discord est cochée.</div></div></div><div class="panel-body">
<form method="post" action="/discord/save" class="alert-form">
<div class="alert-grid"><div><label for="discord_channel">Identifiant du salon</label><input id="discord_channel" name="DISCORD_CHANNEL_ID" value="{{.ChannelID}}" inputmode="numeric" placeholder="123456789012345678"></div><div><label for="discord_token">Token du bot</label><input id="discord_token" name="DISCORD_BOT_TOKEN" type="password" placeholder="{{if .TokenConfigured}}********{{else}}Token du bot{{end}}" autocomplete="off"></div>{{if .TokenConfigured}}<div><label class="check-chip"><input type="checkbox" name="replace_token" value="1"> Remplacer le token enregistré</label></div>{{end}}</div>
<div class="warnbox">Permissions minimales du bot dans ce salon : Voir le salon et Envoyer des messages. Aucun message entrant ni commande Discord n'est traité.</div>
<div><button type="submit">Sauvegarder Discord</button></div></form>
{{if .TestAvailable}}<form method="post" action="/discord/test"><button class="secondary" type="submit">Envoyer un message de test</button></form>{{else}}<div class="hint">Le test sera disponible après configuration du token et du salon.</div>{{end}}
</div></section>
{{end}}`
