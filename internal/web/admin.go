package web

import (
	"context"
	"crypto/subtle"
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
	"github.com/Balrog57/DiskCount/internal/scanner"
)

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

		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="DiskCount Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userMatch := subtle.ConstantTimeCompare([]byte(user), []byte("admin")) == 1
		passMatch := subtle.ConstantTimeCompare([]byte(pass), []byte(s.cfg.WebAdminPassword)) == 1

		if !userMatch || !passMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="DiskCount Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func checkCSRF(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || r.Method == http.MethodTrace {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	if u.Host != host {
		return false
	}
	return true
}

func (s *Server) withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !checkCSRF(r) {
			http.Error(w, "Forbidden - CSRF check failed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
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
// admin endpoints. Anything not matching /health, /healthz or /readyz
// goes through the basic-auth middleware.
func (s *Server) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			s.health(w, r)
			return
		}
		s.withAuth(s.routes()).ServeHTTP(w, r)
	})
}

func (s *Server) routes() http.Handler {
	// muxAdmin is protected by basic auth (see Server.handler). Public
	// endpoints like /health, /healthz, /readyz are dispatched in
	// Server.handler before this mux sees the request.
	muxAdmin := http.NewServeMux()
	muxAdmin.HandleFunc("/", s.stats)
	muxAdmin.HandleFunc("/quality", s.quality)
	muxAdmin.HandleFunc("/products", s.products)
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
	return s.withCSRF(muxAdmin)
}

func (s *Server) base(title, active string) map[string]any {
	return map[string]any{
		"Title":  title,
		"Active": active,
		"Status": appStatus{
			TelegramRunning: s.telegramRunning,
			ConfigComplete:  s.cfg != nil && s.cfg.TelegramBotToken != "",
			SourceCount:     len(s.sourceNames),
		},
	}
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	st, err := s.db.Stats(r.Context())
	data := s.base("Vue d'ensemble", "stats")
	data["Stats"] = st
	data["StatsError"] = err
	data["Sources"] = s.sourceNames
	data["LastReport"] = lastReport(s.scanner)
	data["StartedAt"] = s.startedAt
	render(w, statsTpl, data)
}

func (s *Server) quality(w http.ResponseWriter, r *http.Request) {
	qs, err := s.db.QualityStats(r.Context())
	data := s.base("Qualite donnees", "quality")
	data["Quality"] = qs
	data["Error"] = err
	render(w, qualityTpl, data)
}

func (s *Server) products(w http.ResponseWriter, r *http.Request) {
	prices, err := s.db.LatestPrices(r.Context(), 300)
	filtered := filterPrices(prices, r)
	data := s.base("Produits", "products")
	data["Prices"] = filtered
	data["Error"] = err
	data["Sources"] = uniqueSources(prices)
	data["SelectedSource"] = r.URL.Query().Get("source")
	data["SelectedMedia"] = r.URL.Query().Get("media")
	data["MinTB"] = r.URL.Query().Get("min_tb")
	data["MaxTB"] = r.URL.Query().Get("max_tb")
	data["MaxPrice"] = r.URL.Query().Get("max_eur_tb")
	render(w, productsTpl, data)
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
	data := s.base("Alertes", "alerts")
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
	data := s.base("Configuration", "config")
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
	data := s.base("Utilisateurs", "users")
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
			"started_at":    report.StartedAt,
			"finished_at":   report.FinishedAt,
			"fetched":       report.Fetched,
			"accepted":      report.Accepted,
			"rejected":      report.Rejected,
			"matched":       report.Matched,
			"notified":      report.Notified,
			"dry_run":       report.DryRun,
			"error_count":   len(report.Errors),
			"breaker_skips": report.BreakerSkips,
			"sources":       report.SourceMetrics,
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

func (s *Server) metricsDashboard(w http.ResponseWriter, r *http.Request) {
	report := s.scanner.LastReport()
	data := s.base("Sante & metriques", "metrics")
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
	}).Parse(layoutTpl + body))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template: %v", err), http.StatusInternalServerError)
	}
}

const layoutTpl = `<!doctype html>
<html lang="fr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>DiskCount - {{.Title}}</title>
<style>
:root{color-scheme:light;--bg:#f3f6f8;--panel:#fff;--ink:#15202b;--muted:#667085;--line:#d8e0e7;--line2:#edf1f4;--nav:#102532;--nav2:#183847;--brand:#167c80;--brand2:#255f78;--good:#188052;--warn:#a15c00;--bad:#b42318;--soft:#eef7f7}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.45 Segoe UI,Roboto,Arial,sans-serif}a{color:inherit}
.app{min-height:100vh;display:grid;grid-template-columns:248px 1fr}.sidebar{position:sticky;top:0;height:100vh;background:var(--nav);color:#f8fbfc;padding:18px 14px;display:flex;flex-direction:column;gap:22px}.brand{font-size:20px;font-weight:800;letter-spacing:.2px}.nav{display:grid;gap:6px}.nav a{display:flex;align-items:center;gap:10px;text-decoration:none;color:#d5e3e8;padding:10px 12px;border-radius:8px;transition:background-color .15s ease,color .15s ease}.nav a.active,.nav a:hover{background:var(--nav2);color:#fff}.dot{width:8px;height:8px;border-radius:99px;background:#7ba7b4}.active .dot{background:#59d7c9}.shell{min-width:0}.topbar{height:58px;background:#fff;border-bottom:1px solid var(--line);display:flex;align-items:center;justify-content:space-between;padding:0 28px;position:sticky;top:0;z-index:2}.topbar h1{font-size:20px;margin:0}.status{display:flex;gap:8px;flex-wrap:wrap}.badge{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--line);border-radius:999px;padding:5px 9px;background:#fff;color:var(--muted);font-size:12px}.badge.good{border-color:#b7dec9;background:#edf9f2;color:var(--good)}.badge.warn{border-color:#ffd99d;background:#fff8eb;color:var(--warn)}.badge.bad{border-color:#f5b5b0;background:#fff1f0;color:var(--bad)}main{max-width:1280px;margin:0 auto;padding:24px 28px 44px}.section{margin-top:22px}.grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px}.card,.panel{background:var(--panel);border:1px solid var(--line);border-radius:8px}.card{padding:16px}.label{color:var(--muted);font-size:12px;text-transform:uppercase;letter-spacing:.04em}.value{font-size:30px;font-weight:800;margin-top:4px}.hint{color:var(--muted);font-size:13px}.panel{overflow:hidden}.panel-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 16px;border-bottom:1px solid var(--line2)}.panel-head h2{font-size:16px;margin:0}.panel-body{padding:16px}.table-wrap{overflow:auto}table{width:100%;border-collapse:collapse;white-space:nowrap}th,td{text-align:left;padding:11px 12px;border-bottom:1px solid var(--line2);vertical-align:top}th{background:#f7fafb;color:#41505d;font-size:12px;text-transform:uppercase;letter-spacing:.04em}tr:last-child td{border-bottom:0}.muted{color:var(--muted)}.notice{border:1px solid #c7e7d4;background:#f0faf4;color:#176640;border-radius:8px;padding:10px 12px;margin-bottom:14px}.warnbox{border:1px solid #ffd99d;background:#fff8eb;color:#7a4500;border-radius:8px;padding:10px 12px;margin-bottom:14px}.filters{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:10px}.form-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.config-row{display:grid;grid-template-columns:260px 1fr 150px;gap:12px;align-items:center;padding:13px 16px;border-bottom:1px solid var(--line2)}label{font-weight:700;display:block;margin-bottom:5px}input,select{transition:border-color .15s ease,box-shadow .15s ease;width:100%;border:1px solid var(--line);border-radius:7px;background:#fff;color:var(--ink);padding:9px 10px}input:focus,select:focus{outline:none;border-color:var(--brand);box-shadow:0 0 0 2px var(--soft)}button{transition:background-color .15s ease;border:0;border-radius:7px;background:var(--brand);color:#fff;font-weight:700;padding:9px 12px;cursor:pointer}button:hover{background-color:var(--brand2)}button:focus-visible,a:focus-visible{outline:2px solid var(--brand);outline-offset:2px;border-radius:4px}.secondary{background:#5f6b7a}.secondary:hover{background:#4b5563}.danger{background:var(--bad)}.danger:hover{background:#991b1b}.ghost{background:#eef2f5;color:#243443}.ghost:hover{background:#e2e8f0}.actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}.inline{display:inline}.source-list{display:flex;gap:8px;flex-wrap:wrap}.empty{padding:22px;color:var(--muted);text-align:center}.truncate{max-width:460px;overflow:hidden;text-overflow:ellipsis}.mobile-title{display:none}
@media (max-width:960px){.app{grid-template-columns:1fr}.sidebar{position:relative;height:auto;gap:12px}.nav{grid-template-columns:repeat(3,minmax(0,1fr))}.topbar{position:relative;height:auto;align-items:flex-start;gap:10px;flex-direction:column;padding:16px}main{padding:16px}.grid{grid-template-columns:repeat(2,minmax(0,1fr))}.filters,.config-row{grid-template-columns:1fr}.form-grid{grid-template-columns:1fr}.mobile-title{display:block}}
@media (max-width:560px){.nav{grid-template-columns:1fr}.grid{grid-template-columns:1fr}.status{display:grid;width:100%}.truncate{max-width:240px}}
</style>
</head>
<body>
<div class="app">
<aside class="sidebar">
<div class="brand">DiskCount</div>
<nav class="nav">
<a href="/" class="{{if eq .Active "stats"}}active{{end}}" {{if eq .Active "stats"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>Vue d'ensemble</a>
<a href="/quality" class="{{if eq .Active "quality"}}active{{end}}" {{if eq .Active "quality"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>Qualite</a>
<a href="/products" class="{{if eq .Active "products"}}active{{end}}" {{if eq .Active "products"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>Produits</a>
<a href="/alerts" class="{{if eq .Active "alerts"}}active{{end}}" {{if eq .Active "alerts"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>Alertes</a>
<a href="/config" class="{{if eq .Active "config"}}active{{end}}" {{if eq .Active "config"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>Configuration</a>
<a href="/metrics/dashboard" class="{{if eq .Active "metrics"}}active{{end}}" {{if eq .Active "metrics"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>Sante</a>
<a href="/users" class="{{if eq .Active "users"}}active{{end}}" {{if eq .Active "users"}}aria-current="page"{{end}}><span class="dot" aria-hidden="true"></span>Utilisateurs</a>
</nav>
</aside>
<div class="shell">
<header class="topbar">
<h1>{{.Title}}</h1>
<div class="status">
<span class="badge {{stateClass .Status.TelegramRunning}}">Telegram {{if .Status.TelegramRunning}}actif{{else}}inactif{{end}}</span>
<span class="badge {{stateClass .Status.ConfigComplete}}">Config {{if .Status.ConfigComplete}}complete{{else}}incomplete{{end}}</span>
<span class="badge">{{.Status.SourceCount}} sources</span>
</div>
</header>
<main>{{template "body" .}}</main>
</div>
</div>
</body></html>`

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
<section class="section panel"><div class="panel-head"><h2>Meilleures offres recentes</h2><span class="hint">Creation d'alertes uniquement via Telegram.</span></div><div class="table-wrap"><table><thead><tr><th>Produit</th><th>Source</th><th>Media</th><th>Capacite</th><th>Prix</th><th>EUR/To</th><th>Observe</th></tr></thead><tbody>
{{range .Prices}}<tr><td class="truncate"><a href="{{.URL}}" target="_blank" rel="noreferrer">{{.Title}}</a></td><td>{{.Source}}</td><td>{{ptr .MediaType}}</td><td>{{cap .CapacityTB}}</td><td>{{price .PriceEUR}} EUR</td><td><strong>{{price .PricePerTB}}</strong></td><td>{{tsv .ObservedAt}}</td></tr>{{else}}<tr><td colspan="7" class="empty">Aucun produit ne correspond aux filtres.</td></tr>{{end}}
</tbody></table></div></section>
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
{{range .Rows}}<div class="config-row"><div><label for="{{.Meta.Key}}">{{.Meta.Key}}</label><div class="hint">{{.Meta.Label}}</div></div><div>{{if .Meta.Secret}}<input id="{{.Meta.Key}}" name="{{.Meta.Key}}" type="{{.InputType}}" placeholder="{{.DisplayValue}}"><div class="hint">Coche remplacer pour enregistrer une nouvelle valeur.</div>{{else}}<input id="{{.Meta.Key}}" name="{{.Meta.Key}}" type="text" value="{{.Value}}">{{end}}</div><div>{{if .Meta.Secret}}<label><input type="checkbox" name="replace_{{.Meta.Key}}" value="1"> Remplacer</label>{{else}}<span class="badge warn">Redemarrage</span>{{end}}</div></div>{{end}}
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
