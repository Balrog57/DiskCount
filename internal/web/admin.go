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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/i18n"
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
	db              *db.DB
	scanner         *scanner.Scanner
	cfg             *config.Config
	sourceNames     []string
	telegramRunning bool
	startedAt       time.Time
}

type configRow struct {
	Meta  config.SettingMeta
	Value string
}

type alertRow struct {
	Alert db.Alert
	Owner string
}

type appStatus struct {
	TelegramRunning bool
	ConfigComplete  bool
	SourceCount     int
}

func New(dbase *db.DB, scan *scanner.Scanner, cfg *config.Config, sources []string, telegramRunning bool) *Server {
	sort.Strings(sources)
	return &Server{
		db:              dbase,
		scanner:         scan,
		cfg:             cfg,
		sourceNames:     sources,
		telegramRunning: telegramRunning,
		startedAt:       time.Now().UTC(),
	}
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

func (s *Server) routes() http.Handler {
	// muxAdmin is protected by session auth (see Server.handler). Public
	// endpoints like /health, /healthz, /readyz, /login and /logout are
	// dispatched in Server.handler before this mux sees the request.
	muxAdmin := http.NewServeMux()
	muxAdmin.HandleFunc("/", s.stats)
	muxAdmin.HandleFunc("/quality", s.quality)
	muxAdmin.HandleFunc("/products", s.products)
	muxAdmin.HandleFunc("/product", s.productDetail)
	muxAdmin.HandleFunc("/alerts", s.alerts)
	muxAdmin.HandleFunc("/alerts/toggle", s.toggleAlert)
	muxAdmin.HandleFunc("/alerts/delete", s.deleteAlert)
	muxAdmin.HandleFunc("/config", s.config)
	muxAdmin.HandleFunc("/config/save", s.saveConfig)
	muxAdmin.HandleFunc("/users", s.users)
	muxAdmin.HandleFunc("/users/add", s.addUser)
	muxAdmin.HandleFunc("/users/toggle", s.toggleUser)
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
	return u.RequestURI()
}

func (s *Server) base(title, active string) map[string]any {
	return map[string]any{
		"Title":      title,
		"Active":     active,
		"Locale":     string(i18n.Default),
		"Theme":      themeAuto,
		"T":          func(key string) string { return i18n.T(key, i18n.Default) },
		"KnownLangs": i18n.KnownLocales(),
		"Status": appStatus{
			TelegramRunning: s.telegramRunning,
			ConfigComplete:  s.cfg != nil && s.cfg.TelegramBotToken != "",
			SourceCount:     len(s.sourceNames),
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
	return map[string]any{
		"Title":      title,
		"Active":     active,
		"Locale":     string(loc),
		"Theme":      theme,
		"T":          func(key string) string { return i18n.T(key, loc) },
		"KnownLangs": i18n.KnownLocales(),
		"Status": appStatus{
			TelegramRunning: s.telegramRunning,
			ConfigComplete:  s.cfg != nil && s.cfg.TelegramBotToken != "",
			SourceCount:     len(s.sourceNames),
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
	loc := s.localeForRequest(nil, r)
	data := s.baseWithRequest(r, i18n.T("web.nav.dashboard", loc), "stats")
	data["Stats"] = st
	data["StatsError"] = err
	data["Sources"] = s.sourceNames
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

// parseCronInterval returns the duration encoded by the limited
// "interval" subset of our cron config: "@every 4h", "@every 30m", …
// Anything else (real cron expressions, empty) is rejected so the
// caller can fall back to a static display.
func parseCronInterval(spec string) (time.Duration, bool) {
	spec = strings.TrimSpace(spec)
	after, ok := strings.CutPrefix(spec, "@every ")
	if !ok {
		return 0, false
	}
	d, err := time.ParseDuration(strings.TrimSpace(after))
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
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

func (s *Server) quality(w http.ResponseWriter, r *http.Request) {
	qs, err := s.db.QualityStats(r.Context())
	data := s.baseWithRequest(r, "Qualite donnees", "quality")
	data["Quality"] = qs
	data["Error"] = err
	render(w, qualityTpl, data)
}

func (s *Server) products(w http.ResponseWriter, r *http.Request) {
	prices, err := s.db.LatestPrices(r.Context(), 300)
	filtered := filterPrices(prices, r)
	// 7-day sparkline data per product. We ask for it even when filtered
	// is empty so the page still renders (just with no sparklines).
	sparklines := map[string][]db.SparklinePoint{}
	if s.db != nil && len(filtered) > 0 {
		ids := make([]string, 0, len(filtered))
		seen := make(map[string]bool, len(filtered))
		for _, p := range filtered {
			if !seen[p.ProductID] {
				seen[p.ProductID] = true
				ids = append(ids, p.ProductID)
			}
		}
		sparklines, _ = s.db.Sparklines(r.Context(), ids, 7, 24)
	}
	data := s.baseWithRequest(r, "Produits", "products")
	data["Prices"] = filtered
	data["Sparklines"] = sparklines
	data["Sparkline"] = func(points []db.SparklinePoint) SparklineResult { return computeSparklinePoints(points) }
	data["Error"] = err
	data["Sources"] = uniqueSources(prices)
	data["SelectedSource"] = r.URL.Query().Get("source")
	data["SelectedMedia"] = r.URL.Query().Get("media")
	data["MinTB"] = r.URL.Query().Get("min_tb")
	data["MaxTB"] = r.URL.Query().Get("max_tb")
	data["MaxPrice"] = r.URL.Query().Get("max_eur_tb")
	render(w, productsTpl, data)
}

func (s *Server) productDetail(w http.ResponseWriter, r *http.Request) {
	productID := strings.TrimSpace(r.URL.Query().Get("id"))
	if productID == "" {
		http.Redirect(w, r, "/products", http.StatusSeeOther)
		return
	}
	product, err := s.db.GetProduct(r.Context(), productID)
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
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 && v <= 365 {
			days = v
		}
	}
	history, histErr := s.db.PriceHistory(r.Context(), productID, days)

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
	data["Days"] = days
	data["MinPT"] = minPT
	data["MaxPT"] = maxPT
	data["AvgPT"] = avgPT
	data["ChartPoints"] = computeChartPoints(history, minPT, maxPT)
	data["Error"] = histErr
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
	users, userErr := s.db.ListAuthorizedUsers(r.Context(), true)
	owners := make(map[int64]string, len(users))
	for _, u := range users {
		owners[u.TelegramUserID] = u.Label
	}
	rows := make([]alertRow, 0, len(alerts))
	for _, a := range alerts {
		owner := owners[a.OwnerUserID]
		if owner == "" {
			owner = fmt.Sprintf("Utilisateur %d", a.OwnerUserID)
		}
		rows = append(rows, alertRow{Alert: a, Owner: owner})
	}
	data := s.baseWithRequest(r, "Alertes", "alerts")
	data["Alerts"] = rows
	data["Error"] = firstErr(err, userErr)
	data["Saved"] = r.URL.Query().Get("saved") == "1"
	render(w, alertsTpl, data)
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
	ownerID, alertID, err := alertIDs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	enabled := r.Form.Get("enabled") == "1"
	if err := s.db.SetAlertEnabled(r.Context(), ownerID, alertID, enabled); err != nil {
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
	ownerID, alertID, err := alertIDs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Form.Get("confirm") != "delete" {
		http.Error(w, "confirmation required", http.StatusBadRequest)
		return
	}
	if err := s.db.DeleteAlert(r.Context(), ownerID, alertID); err != nil {
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
	rows := make([]configRow, 0, len(config.AppSettings))
	for _, meta := range config.AppSettings {
		rows = append(rows, configRow{Meta: meta, Value: effective[meta.Key]})
	}
	data := s.baseWithRequest(r, "Configuration", "config")
	data["Rows"] = rows
	data["Error"] = err
	data["Saved"] = r.URL.Query().Get("saved") == "1"
	data["RestartMsg"] = "Les changements Telegram, sources et scanner prennent effet apres redemarrage."
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
	http.Redirect(w, r, "/config?saved=1", http.StatusSeeOther)
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	users, err := s.db.ListAuthorizedUsers(r.Context(), true)
	data := s.baseWithRequest(r, "Utilisateurs", "users")
	data["Users"] = users
	data["Error"] = err
	data["Saved"] = r.URL.Query().Get("saved") == "1"
	render(w, usersTpl, data)
}

func (s *Server) addUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(r.Form.Get("telegram_user_id")), 10, 64)
	if err != nil || id == 0 {
		http.Error(w, "telegram_user_id invalide", http.StatusBadRequest)
		return
	}
	label := strings.TrimSpace(r.Form.Get("label"))
	if err := s.db.UpsertAuthorizedUser(r.Context(), id, label, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/users?saved=1", http.StatusSeeOther)
}

func (s *Server) toggleUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(r.Form.Get("telegram_user_id")), 10, 64)
	if err != nil || id == 0 {
		http.Error(w, "telegram_user_id invalide", http.StatusBadRequest)
		return
	}
	enabled := r.Form.Get("enabled") == "1"
	if err := s.db.SetAuthorizedUserEnabled(r.Context(), id, enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/users?saved=1", http.StatusSeeOther)
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
		"telegram":    s.telegramRunning,
		"sources":     s.sourceNames,
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
		"telegram":  s.telegramRunning,
		"sources":   len(s.sourceNames),
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

func alertIDs(r *http.Request) (int64, int64, error) {
	ownerID, err := strconv.ParseInt(strings.TrimSpace(r.Form.Get("owner_user_id")), 10, 64)
	if err != nil || ownerID == 0 {
		return 0, 0, fmt.Errorf("owner_user_id invalide")
	}
	alertID, err := strconv.ParseInt(strings.TrimSpace(r.Form.Get("alert_id")), 10, 64)
	if err != nil || alertID == 0 {
		return 0, 0, fmt.Errorf("alert_id invalide")
	}
	return ownerID, alertID, nil
}

func filterPrices(prices []db.CurrentPrice, r *http.Request) []db.CurrentPrice {
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	media := strings.TrimSpace(r.URL.Query().Get("media"))
	minTB, hasMin := parseOptionalFloat(r.URL.Query().Get("min_tb"))
	maxTB, hasMax := parseOptionalFloat(r.URL.Query().Get("max_tb"))
	maxPrice, hasMaxPrice := parseOptionalFloat(r.URL.Query().Get("max_eur_tb"))
	out := make([]db.CurrentPrice, 0, len(prices))
	for _, p := range prices {
		if source != "" && p.Source != source {
			continue
		}
		if media != "" && (p.MediaType == nil || *p.MediaType != media) {
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
	tpl := template.Must(template.New("page").Funcs(template.FuncMap{
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
		"price": func(v float64) string { return fmt.Sprintf("%.2f", v) },
		"cap":   func(v float64) string { return fmt.Sprintf("%.1f To", v) },
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
		// T is the i18n helper exposed to templates. When the caller did
		// not populate .T (e.g. some unit tests that render a body in
		// isolation) we fall back to a passthrough that returns the key
		// itself, so `{{call .T "any.key"}}` still renders something
		// instead of panicking.
		"call": func(f any, args ...any) (string, error) {
			if fn, ok := f.(func(string) string); ok {
				if len(args) == 0 {
					return "", nil
				}
				if s, ok := args[0].(string); ok {
					return fn(s), nil
				}
			}
			return "", nil
		},
		// Sparkline computes the 7-day mini-chart for a single product
		// row. Templates call it as `{{call .Sparkline $points}}` and
		// receive a SparklineResult with Coords/Trend fields.
		"Sparkline": func(points []db.SparklinePoint) SparklineResult {
			return computeSparklinePoints(points)
		},
		"durationHuman": durationHuman,
	}).Parse(layoutTpl + body))
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
:root[data-theme=light]{color-scheme:light;--bg:#f3f6f8;--panel:#fff;--ink:#15202b;--muted:#667085;--line:#d8e0e7;--line2:#edf1f4;--nav:#102532;--nav2:#183847;--brand:#167c80;--brand2:#255f78;--good:#188052;--warn:#a15c00;--bad:#b42318;--soft:#eef7f7}
:root[data-theme=dark]{color-scheme:dark;--bg:#0e1620;--panel:#15202b;--ink:#e7eef3;--muted:#8b9aa6;--line:#1f2e3a;--line2:#1a2731;--nav:#08111a;--nav2:#0f1d28;--brand:#3aa6a8;--brand2:#5fbcc2;--good:#4ec78a;--warn:#e0a558;--bad:#e87972;--soft:#152736}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.45 Segoe UI,Roboto,Arial,sans-serif}a{color:inherit}
.app{min-height:100vh;display:grid;grid-template-columns:248px 1fr}.sidebar{position:sticky;top:0;height:100vh;background:var(--nav);color:#f8fbfc;padding:18px 14px;display:flex;flex-direction:column;gap:22px}.brand{font-size:20px;font-weight:800;letter-spacing:.2px}.nav{display:grid;gap:6px}.nav a{display:flex;align-items:center;gap:10px;text-decoration:none;color:#d5e3e8;padding:10px 12px;border-radius:8px;transition:background-color .15s ease,color .15s ease}.nav a.active,.nav a:hover{background:var(--nav2);color:#fff}.dot{width:8px;height:8px;border-radius:99px;background:#7ba7b4}.active .dot{background:#59d7c9}.shell{min-width:0}.topbar{height:58px;background:#fff;border-bottom:1px solid var(--line);display:flex;align-items:center;justify-content:space-between;padding:0 28px;position:sticky;top:0;z-index:2}.topbar h1{font-size:20px;margin:0}.status{display:flex;gap:8px;flex-wrap:wrap}.badge{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--line);border-radius:999px;padding:5px 9px;background:#fff;color:var(--muted);font-size:12px}.badge.good{border-color:#b7dec9;background:#edf9f2;color:var(--good)}.badge.warn{border-color:#ffd99d;background:#fff8eb;color:var(--warn)}.badge.bad{border-color:#f5b5b0;background:#fff1f0;color:var(--bad)}main{max-width:1280px;margin:0 auto;padding:24px 28px 44px}.section{margin-top:22px}.grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px}.card,.panel{background:var(--panel);border:1px solid var(--line);border-radius:8px}.card{padding:16px}.label{color:var(--muted);font-size:12px;text-transform:uppercase;letter-spacing:.04em}.value{font-size:30px;font-weight:800;margin-top:4px}.hint{color:var(--muted);font-size:13px}.panel{overflow:hidden}.panel-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 16px;border-bottom:1px solid var(--line2)}.panel-head h2{font-size:16px;margin:0}.panel-body{padding:16px}.table-wrap{overflow:auto}table{width:100%;border-colla
.lang-switch{margin-top:auto;display:flex;gap:6px;align-items:center;color:#a9c0c8;font-size:12px}.lang-switch a,.lang-switch button{background:transparent;border:1px solid #2a4a55;color:#d5e3e8;border-radius:6px;padding:4px 8px;font-size:12px;cursor:pointer;text-decoration:none}.lang-switch a.active,.lang-switch button.active{background:#59d7c9;border-color:#59d7c9;color:#0a1a21;font-weight:600}.lang-switch a:hover,.lang-switch button:hover{background:#1c3a45}
@media (max-width:960px){.app{grid-template-columns:1fr}.sidebar{position:relative;height:auto;gap:12px}.nav{grid-template-columns:repeat(3,minmax(0,1fr))}.topbar{position:relative;height:auto;align-items:flex-start;gap:10px;flex-direction:column;padding:16px}main{padding:16px}.grid{grid-template-columns:repeat(2,minmax(0,1fr))}.filters,.config-row{grid-template-columns:1fr}.form-grid{grid-template-columns:1fr}.mobile-title{display:block}}
@media (max-width:560px){.nav{grid-template-columns:1fr}.grid{grid-template-columns:1fr}.status{display:grid;width:100%}.truncate{max-width:240px}}
.sparkline{width:80px;height:24px;display:block}
.muted{color:var(--muted)}
:root[data-theme=dark] .topbar{background:var(--panel);color:var(--ink)}
:root[data-theme=dark] .badge{background:var(--panel);color:var(--muted)}
:root[data-theme=dark] .badge.good{background:rgba(78,199,138,.12);color:var(--good);border-color:rgba(78,199,138,.4)}
:root[data-theme=dark] .badge.warn{background:rgba(224,165,88,.12);color:var(--warn);border-color:rgba(224,165,88,.4)}
:root[data-theme=dark] .badge.bad{background:rgba(232,121,114,.12);color:var(--bad);border-color:rgba(232,121,114,.4)}
:root[data-theme=dark] .login-card input[type=password]{background:var(--panel);color:var(--ink)}
:root[data-theme=dark] .login-card .error{background:rgba(232,121,114,.12);border-color:rgba(232,121,114,.5)}
.theme-switch{display:inline-flex;gap:4px;margin-top:6px;width:100%}
.theme-switch a,.theme-switch button{background:transparent;border:1px solid #2a4a55;color:#d5e3e8;border-radius:6px;padding:4px 6px;font-size:11px;cursor:pointer;flex:1;text-decoration:none;text-align:center}
:root[data-theme=light] .theme-switch a,.theme-switch button{color:var(--muted);border-color:var(--line)}
.theme-switch a.active,.theme-switch button.active{background:#59d7c9;border-color:#59d7c9;color:#0a1a21;font-weight:600}
:root[data-theme=light] .theme-switch a.active,.theme-switch button.active{background:var(--brand);color:#fff;border-color:var(--brand)}
</style>
</head>
<body>
<div class="app">
<aside class="sidebar">
<div class="brand">DiskCount</div>
<nav class="nav">
<a href="/" class="{{if eq .Active "stats"}}active{{end}}" {{if eq .Active "stats"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.dashboard"}}</a>
<a href="/quality" class="{{if eq .Active "quality"}}active{{end}}" {{if eq .Active "quality"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.quality"}}</a>
<a href="/products" class="{{if eq .Active "products"}}active{{end}}" {{if eq .Active "products"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.products"}}</a>
<a href="/alerts" class="{{if eq .Active "alerts"}}active{{end}}" {{if eq .Active "alerts"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.alerts"}}</a>
<a href="/config" class="{{if eq .Active "config"}}active{{end}}" {{if eq .Active "config"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.config"}}</a>
<a href="/metrics/dashboard" class="{{if eq .Active "metrics"}}active{{end}}" {{if eq .Active "metrics"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.metrics"}}</a>
<a href="/users" class="{{if eq .Active "users"}}active{{end}}" {{if eq .Active "users"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>{{call .T "web.nav.users"}}</a>
</nav>
<div class="lang-switch">{{range .KnownLangs}}<form method="post" action="/lang" style="display:inline;margin:0"><input type="hidden" name="lang" value="{{.}}"><input type="hidden" name="next" value="{{$.Title}}"><button type="submit" {{if eq $.Locale .}}class="active"{{end}}>{{if eq . "fr"}}FR{{else}}EN{{end}}</button></form>{{end}}</div>
<div class="theme-switch"><form method="post" action="/theme" style="display:inline;margin:0;width:100%"><input type="hidden" name="theme" value="light"><input type="hidden" name="next" value="{{$.Title}}"><button type="submit" {{if eq $.Theme "light"}}class="active"{{end}}>☀ Light</button></form><form method="post" action="/theme" style="display:inline;margin:0;width:100%"><input type="hidden" name="theme" value="dark"><input type="hidden" name="next" value="{{$.Title}}"><button type="submit" {{if eq $.Theme "dark"}}class="active"{{end}}>🌙 Dark</button></form><form method="post" action="/theme" style="display:inline;margin:0;width:100%"><input type="hidden" name="theme" value="auto"><input type="hidden" name="next" value="{{$.Title}}"><button type="submit" {{if eq $.Theme "auto"}}class="active"{{end}}>🖥 Auto</button></form></div>
</aside>
<div class="shell">
<header class="topbar">
<h1>{{.Title}}</h1>
<div class="status">
<span class="badge {{stateClass .Status.TelegramRunning}}">Telegram {{if .Status.TelegramRunning}}actif{{else}}inactif{{end}}</span>
<span class="badge {{stateClass .Status.ConfigComplete}}">Config {{if .Status.ConfigComplete}}complete{{else}}incomplete{{end}}</span>
<span class="badge">{{.Status.SourceCount}} sources</span>
<form method="post" action="/logout" style="display:inline;margin:0"><button class="badge" type="submit" style="cursor:pointer;border:1px solid var(--line);background:#fff;color:var(--muted)">{{call .T "web.nav.logout"}}</button></form>
</div>
</header>
<main>{{template "body" .}}</main>
</div>
</div>
</body></html>`

const loginTpl = `{{define "body"}}
<style>
.login-shell{min-height:calc(100vh - 0px);display:grid;place-items:center;padding:40px 20px}
.login-card{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:28px;width:100%;max-width:380px;box-shadow:0 12px 32px rgba(16,37,50,.12)}
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
<div class="lang-switch" style="margin:14px auto 0;justify-content:center">{{range .KnownLangs}}<form method="post" action="/lang" style="display:inline;margin:0"><input type="hidden" name="lang" value="{{.}}"><input type="hidden" name="next" value="{{$.Title}}"><button type="submit" {{if eq $.Locale .}}class="active"{{end}}>{{if eq . "fr"}}FR{{else}}EN{{end}}</button></form>{{end}}</div>
</div>
{{end}}`

const statsTpl = `{{define "body"}}
{{if .StatsError}}<div class="warnbox">Erreur DB: {{.StatsError}}</div>{{end}}
<div class="grid">
<div class="card"><div class="label">Alertes actives</div><div class="value">{{if .Stats}}{{.Stats.ActiveAlerts}}{{else}}0{{end}}</div></div>
<div class="card"><div class="label">Produits</div><div class="value">{{if .Stats}}{{.Stats.Products}}{{else}}0{{end}}</div></div>
<div class="card"><div class="label">Observations</div><div class="value">{{if .Stats}}{{.Stats.Observations}}{{else}}0{{end}}</div></div>
<div class="card"><div class="label">Notifications</div><div class="value">{{if .Stats}}{{.Stats.Notifications}}{{else}}0{{end}}</div></div>
</div>
<div class="section grid">
<div class="card"><div class="label">Alertes inactives</div><div class="value">{{if .Stats}}{{.Stats.InactiveAlerts}}{{else}}0{{end}}</div></div>
<div class="card"><div class="label">Utilisateurs actifs</div><div class="value">{{if .Stats}}{{.Stats.AuthorizedEnabled}}{{else}}0{{end}}</div></div>
<div class="card"><div class="label">Utilisateurs inactifs</div><div class="value">{{if .Stats}}{{.Stats.AuthorizedDisabled}}{{else}}0{{end}}</div></div>
<div class="card"><div class="label">Rejets donnees</div><div class="value">{{if .Stats}}{{.Stats.RejectedDeals}}{{else}}0{{end}}</div></div>
</div>
<section class="section panel"><div class="panel-head"><h2>Sources actives</h2></div><div class="panel-body"><div class="source-list">{{range .Sources}}<span class="badge">{{.}}</span>{{else}}<span class="muted">Aucune source active</span>{{end}}</div></div></section>
<section class="section panel"><div class="panel-head"><h2>Derniers evenements</h2></div><div class="table-wrap"><table><tbody>
<tr><th>Demarrage Web</th><td>{{tsv .StartedAt}}</td></tr>
<tr><th>Derniere observation</th><td>{{if .Stats}}{{ts .Stats.LastObservationAt}}{{else}}-{{end}}</td></tr>
<tr><th>Derniere notification</th><td>{{if .Stats}}{{ts .Stats.LastNotificationAt}}{{else}}-{{end}}</td></tr>
{{if .LastReport}}<tr><th>Dernier scan</th><td>{{tsv .LastReport.FinishedAt}} - fetched={{.LastReport.Fetched}}, accepted={{.LastReport.Accepted}}, rejected={{.LastReport.Rejected}}, matched={{.LastReport.Matched}}, notified={{.LastReport.Notified}}, errors={{len .LastReport.Errors}}</td></tr>{{else}}<tr><th>Dernier scan</th><td>-</td></tr>{{end}}
{{if .NextCheck}}<tr><th>Prochain scan</th><td>{{tsv .NextCheck}} - dans {{durationHuman .NextCheckIn}}</td></tr>{{end}}
</tbody></table></div></section>
{{if .LastReport}}{{if gt (len .LastReport.SourceWarnings) 0}}<section class="panel warnbox"><div class="panel-head"><h2>âš  Sources en alerte</h2></div><div class="table-wrap"><table><thead><tr><th>Source</th><th>Scans vides consecutifs</th><th>Message</th></tr></thead><tbody>{{range .LastReport.SourceWarnings}}<tr><td><span class="badge">{{.Name}}</span></td><td>{{.ConsecutiveZeros}}</td><td>{{.Message}}</td></tr>{{end}}</tbody></table></div></section>{{end}}{{end}}
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

const productsTpl = `{{define "body"}}
{{if .Error}}<div class="warnbox">Erreur: {{.Error}}</div>{{end}}
<section class="panel"><div class="panel-head"><h2>Filtres</h2></div><div class="panel-body">
<form method="get" action="/products" class="filters">
<div><label for="filter_source">Source</label><select id="filter_source" name="source"><option value="">Toutes</option>{{range .Sources}}<option value="{{.}}" {{if eq $.SelectedSource .}}selected{{end}}>{{.}}</option>{{end}}</select></div>
<div><label for="filter_media">Media</label><select id="filter_media" name="media"><option value="">Tous</option><option value="rotational" {{if eq .SelectedMedia "rotational"}}selected{{end}}>HDD</option><option value="solid_state" {{if eq .SelectedMedia "solid_state"}}selected{{end}}>SSD</option></select></div>
<div><label for="filter_min_tb">Min To</label><input id="filter_min_tb" name="min_tb" value="{{.MinTB}}" inputmode="decimal"></div>
<div><label for="filter_max_tb">Max To</label><input id="filter_max_tb" name="max_tb" value="{{.MaxTB}}" inputmode="decimal"></div>
<div><label for="filter_max_eur_tb">Max EUR/To</label><input id="filter_max_eur_tb" name="max_eur_tb" value="{{.MaxPrice}}" inputmode="decimal"></div>
<div class="actions"><button type="submit">Filtrer</button><a class="badge" href="/products">Reinitialiser</a></div>
</form></div></section>
<section class="section panel"><div class="panel-head"><h2>Meilleures offres recentes</h2><span class="hint">Creation d'alertes uniquement via Telegram.</span></div><div class="table-wrap"><table><thead><tr><th>Produit</th><th>Source</th><th>Media</th><th>Capacite</th><th>Prix</th><th>EUR/To</th><th>7j</th><th>Observe</th></tr></thead><tbody>
{{range .Prices}}{{$pts := index $.Sparklines .ProductID}}{{$spark := call .Sparkline $pts}}<tr><td class="truncate"><a href="/product?id={{.ProductID}}">{{.Title}}</a></td><td>{{.Source}}</td><td>{{ptr .MediaType}}</td><td>{{cap .CapacityTB}}</td><td>{{price .PriceEUR}} EUR</td><td><strong>{{price .PricePerTB}}</strong></td><td>{{if $spark.Coords}}<svg class="sparkline" viewBox="0 0 80 24" preserveAspectRatio="none" aria-label="Tendance 7 jours ({{$spark.Trend}})"><polyline fill="none" stroke-width="1.5" {{if eq $spark.Trend "down"}}stroke="#188052"{{else if eq $spark.Trend "up"}}stroke="#b42318"{{else}}stroke="#667085"{{end}} points="{{$spark.Coords}}"/></svg>{{else}}<span class="muted">-</span>{{end}}</td><td>{{tsv .ObservedAt}}</td></tr>{{else}}<tr><td colspan="8" class="empty">Aucun produit ne correspond aux filtres.</td></tr>{{end}}
</tbody></table></div></section>
{{end}}`

const productDetailTpl = `{{define "body"}}
{{if .Error}}<div class="warnbox">Erreur: {{.Error}}</div>{{end}}
{{if .Product}}
<section class="panel"><div class="panel-head"><h2>{{.Product.Title}}</h2></div><div class="table-wrap"><table><tbody>
<tr><th>Source</th><td>{{.Product.Source}}</td></tr>
<tr><th>Capacite</th><td>{{cap .Product.CapacityTB}}</td></tr>
<tr><th>Media</th><td>{{ptr .Product.MediaType}}</td></tr>
<tr><th>Marque</th><td>{{ptr .Product.Brand}}</td></tr>
{{if .Product.RecordingMethod}}<tr><th>Enregistrement</th><td>{{ptr .Product.RecordingMethod}}</td></tr>{{end}}
{{if .Product.DriveCategory}}<tr><th>Categorie</th><td>{{ptr .Product.DriveCategory}}</td></tr>{{end}}
<tr><th>Qualite</th><td>{{.Product.QualityScore}}/100</td></tr>
<tr><th>Premiere observation</th><td>{{tsv .Product.FirstSeenAt}}</td></tr>
<tr><th>Derniere observation</th><td>{{tsv .Product.LastSeenAt}}</td></tr>
<tr><th>Lien</th><td><a href="{{.Product.URL}}" target="_blank" rel="noreferrer">Ouvrir l'offre</a></td></tr>
</tbody></table></div></section>

<section class="panel"><div class="panel-head"><h2>Historique de prix ({{.Days}} jours)</h2><div class="range-links"><a href="/product?id={{.Product.ID}}&days=7" {{if eq .Days 7}}class="active"{{end}}>7j</a> <a href="/product?id={{.Product.ID}}&days=30" {{if eq .Days 30}}class="active"{{end}}>30j</a> <a href="/product?id={{.Product.ID}}&days=90" {{if eq .Days 90}}class="active"{{end}}>90j</a> <a href="/product?id={{.Product.ID}}&days=365" {{if eq .Days 365}}class="active"{{end}}>1an</a></div></div>
{{if .History}}
<div class="stats-row"><div class="stat"><span class="label">Min EUR/To</span><span class="value">{{price .MinPT}}</span></div><div class="stat"><span class="label">Moy EUR/To</span><span class="value">{{price .AvgPT}}</span></div><div class="stat"><span class="label">Max EUR/To</span><span class="value">{{price .MaxPT}}</span></div></div>
<div class="chart-wrap">{{if .ChartPoints}}<svg class="price-chart" viewBox="0 0 800 200" preserveAspectRatio="none"><polyline fill="none" stroke="#4a9" stroke-width="2" points="{{.ChartPoints}}"/></svg>{{else}}<p class="empty">Pas assez de donnees pour un graphique.</p>{{end}}</div>
<div class="table-wrap"><table><thead><tr><th>Date</th><th>Prix</th><th>EUR/To</th><th>Source</th></tr></thead><tbody>
{{range .History}}<tr><td>{{tsv .ObservedAt}}</td><td>{{price .PriceEUR}} EUR</td><td>{{price .PricePerTB}}</td><td>{{.Source}}</td></tr>{{end}}
</tbody></table></div>
{{else}}<p class="empty">Aucune observation sur cette periode.</p>{{end}}
</section>
{{else}}<p class="empty">Produit introuvable. <a href="/products">Retour aux produits</a></p>{{end}}
<p><a href="/products">&larr; Retour aux produits</a></p>
{{end}}`

const alertsTpl = `{{define "body"}}
{{if .Saved}}<div class="notice">Alertes mises a jour.</div>{{end}}
{{if .Error}}<div class="warnbox">Erreur: {{.Error}}</div>{{end}}
<div class="warnbox">Creation et edition detaillee via Telegram. Cette page permet uniquement pause, reprise et suppression.</div>
<section class="panel"><div class="panel-head"><h2>Alertes existantes</h2></div><div class="table-wrap"><table><thead><tr><th>Nom</th><th>Proprietaire</th><th>Etat</th><th>Capacites</th><th>Media</th><th>Prix max</th><th>Actions</th></tr></thead><tbody>
{{range .Alerts}}<tr><td>{{.Alert.Name}}</td><td>{{.Owner}}</td><td>{{if .Alert.Enabled}}<span class="badge good">active</span>{{else}}<span class="badge warn">inactive</span>{{end}}</td><td>{{csv .Alert.CapacityPresets}}</td><td>{{csv .Alert.MediaTypes}}</td><td>{{alertPrice .Alert}}</td><td><div class="actions">
<form class="inline" method="post" action="/alerts/toggle"><input type="hidden" name="owner_user_id" value="{{.Alert.OwnerUserID}}"><input type="hidden" name="alert_id" value="{{.Alert.ID}}">{{if .Alert.Enabled}}<input type="hidden" name="enabled" value="0"><button class="secondary" type="submit" aria-label="Mettre en pause l'alerte {{.Alert.Name}}">Pause</button>{{else}}<input type="hidden" name="enabled" value="1"><button type="submit" aria-label="Reprendre l'alerte {{.Alert.Name}}">Reprendre</button>{{end}}</form>
<form class="inline" method="post" action="/alerts/delete" onsubmit="return confirm('Supprimer cette alerte ?')"><input type="hidden" name="owner_user_id" value="{{.Alert.OwnerUserID}}"><input type="hidden" name="alert_id" value="{{.Alert.ID}}"><input type="hidden" name="confirm" value="delete"><button class="danger" type="submit" aria-label="Supprimer l'alerte {{.Alert.Name}}">Supprimer</button></form>
</div></td></tr>{{else}}<tr><td colspan="7" class="empty">Aucune alerte.</td></tr>{{end}}
</tbody></table></div></section>
{{end}}`

const configTpl = `{{define "body"}}
{{if .Saved}}<div class="notice">Configuration sauvegardee.</div>{{end}}
{{if .Error}}<div class="warnbox">Erreur: {{.Error}}</div>{{end}}
<div class="warnbox">{{.RestartMsg}}</div>
<form method="post" action="/config/save" class="panel"><div class="panel-head"><h2>Parametres applicatifs</h2><button type="submit">Sauvegarder</button></div>
{{range .Rows}}<div class="config-row"><div><label for="{{.Meta.Key}}">{{.Meta.Key}}</label><div id="hint_{{.Meta.Key}}" class="hint">{{.Meta.Label}}</div></div><div>{{if .Meta.Secret}}<input id="{{.Meta.Key}}" aria-describedby="hint_{{.Meta.Key}} hint_{{.Meta.Key}}_replace" name="{{.Meta.Key}}" type="{{.InputType}}" placeholder="{{.DisplayValue}}"><div id="hint_{{.Meta.Key}}_replace" class="hint">Coche remplacer pour enregistrer une nouvelle valeur.</div>{{else}}<input id="{{.Meta.Key}}" aria-describedby="hint_{{.Meta.Key}}" name="{{.Meta.Key}}" type="text" value="{{.Value}}">{{end}}</div><div>{{if .Meta.Secret}}<label><input type="checkbox" name="replace_{{.Meta.Key}}" value="1"> Remplacer</label>{{else}}<span class="badge warn">Redemarrage</span>{{end}}</div></div>{{end}}
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
{{range $name, $state := .Breakers}}<tr><td>{{$name}}</td><td>{{if eq $state "closed"}}<span class="badge good">closed</span>{{else if eq $state "half-open"}}<span class="badge warn">half-open</span>{{else}}<span class="badge bad">open</span>{{end}}</td><td><form class="inline" method="post" action="/api/sources/breaker/reset"><input type="hidden" name="name" value="{{$name}}"><button class="secondary" type="submit">Reinitialiser</button></form></td></tr>{{else}}<tr><td colspan="3" class="empty">Aucun breaker.</td></tr>{{end}}
</tbody></table></div></section>
<section class="section panel"><div class="panel-head"><h2>Metriques par source (dernier scan)</h2></div><div class="table-wrap"><table><thead><tr><th>Source</th><th>Deals</th><th>Breaker</th><th>Erreur</th></tr></thead><tbody>
{{if .Report}}{{range .Report.SourceMetrics}}<tr><td>{{.Name}}</td><td>{{.DealsFetched}}</td><td>{{.BreakerState}}</td><td>{{.Error}}</td></tr>{{else}}<tr><td colspan="4" class="empty">Aucune metrique.</td></tr>{{end}}{{else}}<tr><td colspan="4" class="empty">Aucun scan.</td></tr>{{end}}
</tbody></table></div></section>
{{end}}`

const usersTpl = `{{define "body"}}
{{if .Saved}}<div class="notice">Utilisateurs mis a jour.</div>{{end}}
{{if .Error}}<div class="warnbox">Erreur: {{.Error}}</div>{{end}}
<section class="panel"><div class="panel-head"><h2>Ajouter ou reactiver</h2></div><div class="panel-body"><form method="post" action="/users/add" class="form-grid"><div><label for="add_user_id">Identifiant Telegram</label><input id="add_user_id" type="number" name="telegram_user_id" required></div><div><label for="add_user_label">Nom</label><input id="add_user_label" type="text" name="label" required></div><div><button type="submit">Enregistrer</button></div></form></div></section>
<section class="section panel"><div class="panel-head"><h2>Utilisateurs autorises</h2></div><div class="table-wrap"><table><thead><tr><th>Nom</th><th>Identifiant</th><th>Etat</th><th>Action</th></tr></thead><tbody>
{{range .Users}}<tr><td>{{.Label}}</td><td>{{.TelegramUserID}}</td><td>{{if .Enabled}}<span class="badge good">actif</span>{{else}}<span class="badge warn">desactive</span>{{end}}</td><td><form class="inline" method="post" action="/users/toggle"><input type="hidden" name="telegram_user_id" value="{{.TelegramUserID}}">{{if .Enabled}}<input type="hidden" name="enabled" value="0"><button class="secondary" type="submit" aria-label="Desactiver l'utilisateur {{.Label}}">Desactiver</button>{{else}}<input type="hidden" name="enabled" value="1"><button class="secondary" type="submit" aria-label="Reactiver l'utilisateur {{.Label}}">Reactiver</button>{{end}}</form></td></tr>{{else}}<tr><td colspan="4" class="empty">Aucun utilisateur.</td></tr>{{end}}
</tbody></table></div></section>
{{end}}`
