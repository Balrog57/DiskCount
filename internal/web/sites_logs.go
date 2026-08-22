package web

import (
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/i18n"
	"github.com/Balrog57/DiskCount/internal/scanner"
)

// siteStat is deliberately a view model: source credentials and URLs never
// leave the source registry, only counters and quality metrics are rendered.
type siteStat struct {
	Name, Label, Status, Breaker, Error string
	Products, Observations, Rejected    int64
	MedianPricePerTB                    *float64
	LastDeals                           int
	LastScan                            time.Time
	Duration                            time.Duration
}

type merchantToggle struct {
	Name, Label string
	Enabled     bool
}

func buildSiteStats(names []string, quality *db.QualityStats, health []scanner.SourceHealthEntry, report *scanner.ScanReport) []siteStat {
	byName := make(map[string]db.SourceQuality)
	if quality != nil {
		for _, row := range quality.Sources {
			byName[row.Source] = row
		}
	}
	last := make(map[string]scanner.SourceHealthEntry)
	for _, row := range health {
		last[row.Name] = row
	}
	seen := make(map[string]bool)
	active := make(map[string]bool, len(names))
	for _, name := range names {
		active[name] = true
	}
	metrics := make(map[string]scanner.SourceMetrics)
	if report != nil {
		for _, row := range report.SourceMetrics {
			metrics[row.Name] = row
		}
	}
	out := make([]siteStat, 0, len(names)+len(byName))
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		row := byName[name]
		h := last[name]
		metric := metrics[name]
		status := "actif"
		if name == "asus-shop-fr" || name == "nvidia-fr" {
			status = "hors perimetre stockage"
		} else if !active[name] {
			status = "inactif"
		} else if isBlockedMetric(metric) {
			status = "captcha"
		} else if metric.Error != "" {
			status = "erreur"
		} else if h.Flagged {
			status = "a surveiller"
		}
		stat := siteStat{Name: name, Label: siteLabel(name), Status: status, Products: row.Products, Observations: row.Observations, Rejected: row.Rejected, MedianPricePerTB: row.MedianPricePerTB, LastDeals: h.LastDeals, Breaker: metric.BreakerState, Error: metric.Error, Duration: metric.FetchDuration}
		if report != nil {
			stat.LastScan = report.FinishedAt
		}
		out = append(out, stat)
	}
	for _, name := range names {
		add(name)
	}
	for name := range byName {
		add(name)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func isBlockedMetric(metric scanner.SourceMetrics) bool {
	if metric.BlockedByKeyword != "" {
		return true
	}
	text := strings.ToLower(metric.Error)
	for _, marker := range []string{"captcha", "waf", "blocked", "access denied"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func siteLabel(name string) string {
	labels := map[string]string{
		"alternate": "Alternate FR", "amazon.fr": "Amazon France", "amazon.de": "Amazon Germany",
		"diskprices": "DiskPrices (Amazon)", "pricepergig": "PricePerGig (Amazon)", "pricepertb": "PricePerTB (Amazon)", "keepa": "Keepa (Amazon)",
		"materiel": "Materiel.net", "pccomponentes": "PCComponentes FR", "topachat": "TopAchat", "rueducommerce": "Rue du Commerce", "asus-shop-fr": "Asus Shop FR", "nvidia-fr": "Nvidia FR",
	}
	if label := labels[name]; label != "" {
		return label
	}
	return name
}

func (s *Server) sites(w http.ResponseWriter, r *http.Request) {
	var quality *db.QualityStats
	var err error
	if s.db != nil {
		quality, err = s.db.QualityStats(r.Context())
	}
	var health []scanner.SourceHealthEntry
	if s.scanner != nil {
		health = s.scanner.SourceHealth()
	}
	data := s.baseWithRequest(r, "Sites", "sites")
	loc := i18n.ParseLocale(fmtSprint(data["Locale"]))
	data["Title"] = i18n.T("web.sites.title", loc)
	names := append(append([]string(nil), s.liveSourceNames()...), "asus-shop-fr", "nvidia-fr")
	data["Sites"] = buildSiteStats(names, quality, health, lastReport(s.scanner))
	data["Error"] = err
	data["Saved"] = r.URL.Query().Get("saved") == "1"
	cfg := s.liveConfig()
	byparrOK := cfg != nil && strings.TrimSpace(cfg.ByparrURL) != ""
	data["ByparrOK"] = byparrOK
	enabled := map[string]bool{}
	if cfg != nil {
		if len(cfg.EnabledSources) == 0 {
			for _, name := range config.FrenchMerchantNames {
				enabled[name] = true
			}
		} else {
			for _, name := range cfg.EnabledSources {
				enabled[name] = true
			}
		}
	}
	toggles := make([]merchantToggle, 0, len(config.FrenchMerchantNames))
	for _, name := range config.FrenchMerchantNames {
		toggles = append(toggles, merchantToggle{Name: name, Label: siteLabel(name), Enabled: enabled[name]})
	}
	data["MerchantToggles"] = toggles
	render(w, sitesTpl, data)
}

func fmtSprint(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (s *Server) saveSiteSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	selected := cleanValues(r.Form["merchant"])
	allowed := map[string]bool{}
	for _, name := range config.FrenchMerchantNames {
		allowed[name] = true
	}
	frSelected := map[string]bool{}
	enabledFR := make([]string, 0, len(selected))
	for _, name := range selected {
		if allowed[name] {
			frSelected[name] = true
			enabledFR = append(enabledFR, name)
		}
	}
	out := make([]string, 0)
	for _, name := range s.liveSourceNames() {
		if allowed[name] {
			if frSelected[name] {
				out = append(out, name)
			}
			continue
		}
		out = append(out, name)
	}
	for _, name := range enabledFR {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	existing, err := s.db.ListAppConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	existing["ENABLED_SOURCES"] = strings.Join(out, ",")
	ensureMerchantDefaults(existing, enabledFR)
	if err := s.db.SetAppConfig(r.Context(), existing); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.reloadSources(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/sites?saved=1", http.StatusSeeOther)
}

func ensureMerchantDefaults(values map[string]string, enabled []string) {
	defaults := config.DefaultValues()
	keyFor := map[string]string{
		"alternate": "ALTERNATE_URLS", "boulanger": "BOULANGER_URLS", "cdiscount": "CDISCOUNT_URLS",
		"corsair": "CORSAIR_URLS", "cybertek": "CYBERTEK_URLS", "darty": "DARTY_URLS",
		"fnac": "FNAC_URLS", "grosbill": "GROSBILL_URLS", "ldlc": "LDLC_URLS",
		"materiel": "MATERIEL_URLS", "pccomponentes": "PCCOMPONENTES_URLS", "rueducommerce": "RUEDUCOMMERCE_URLS",
		"topachat": "TOPACHAT_URLS", "topbiz": "TOPBIZ_URLS",
	}
	for _, name := range enabled {
		key := keyFor[name]
		if key == "" {
			continue
		}
		if strings.TrimSpace(values[key]) == "" {
			values[key] = defaults[key]
		}
	}
}

type logEntry struct {
	Level, Message string
	At             time.Time
}

func scanLogEntries(report *scanner.ScanReport) []logEntry {
	if report == nil {
		return nil
	}
	at := report.FinishedAt
	if at.IsZero() {
		at = report.StartedAt
	}
	level := "success"
	if len(report.Errors) > 0 {
		level = "warning"
	}
	out := []logEntry{{Level: level, At: at, Message: "Scan terminé : " + formatScanSummary(report)}}
	for _, metric := range report.SourceMetrics {
		entryLevel := "success"
		message := metric.Name + ": " + strconv.Itoa(metric.DealsFetched) + " offres"
		if isBlockedMetric(metric) {
			entryLevel = "error"
			message = strings.ToLower(metric.Name) + ": bloqué par CAPTCHA (" + strconv.Itoa(metric.DealsFetched) + " offre)"
		} else if metric.Error != "" {
			entryLevel, message = "error", metric.Name+": "+metric.Error
		} else if metric.DealsFetched == 0 {
			entryLevel = "warning"
			message = metric.Name + ": 0 offres"
		}
		out = append(out, logEntry{Level: entryLevel, At: at, Message: message})
	}
	for _, warn := range report.SourceWarnings {
		out = append(out, logEntry{Level: "warning", At: at, Message: warn.Name + ": " + warn.Message})
	}
	for _, err := range report.Errors {
		out = append(out, logEntry{Level: "error", At: at, Message: err})
	}
	return out
}

func formatScanSummary(report *scanner.ScanReport) string {
	return "fetched=" + strconv.Itoa(report.Fetched) + ", accepted=" + strconv.Itoa(report.Accepted) + ", rejected=" + strconv.Itoa(report.Rejected) + ", matched=" + strconv.Itoa(report.Matched) + ", notified=" + strconv.Itoa(report.Notified)
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	data := s.baseWithRequest(r, "Logs", "logs")
	loc := i18n.ParseLocale(fmtSprint(data["Locale"]))
	data["Title"] = i18n.T("web.nav.logs", loc)
	if s.scanner != nil {
		data["Logs"] = scanLogEntries(s.scanner.LastReport())
	}
	render(w, logsTpl, data)
}

const sitesTpl = `{{define "body"}}
{{if .Saved}}<div class="notice">{{call .T "web.common.config_saved"}}</div>{{end}}
<section class="panel"><div class="panel-head"><div><h2>{{call .T "web.sites.merchants_toggle_title"}}</h2><div class="hint">{{call .T "web.sites.merchants_toggle_hint"}}</div></div>
<span class="badge {{if .ByparrOK}}good{{else}}warn{{end}}">{{if .ByparrOK}}{{call .T "web.sites.byparr_ok"}}{{else}}{{call .T "web.sites.byparr_off"}}{{end}}</span></div>
<form method="post" action="/sites/sources" class="panel-body"><div class="check-grid">{{range .MerchantToggles}}<label class="check-chip"><input type="checkbox" name="merchant" value="{{.Name}}" {{if .Enabled}}checked{{end}}> {{.Label}}</label>{{end}}</div>
<div style="margin-top:14px"><button type="submit">{{call .T "web.sites.save_merchants"}}</button></div></form></section>
<div class="panel section"><div class="panel-head"><h2>{{call .T "web.sites.title"}}</h2><span class="hint">{{call .T "web.sites.subtitle"}}</span></div><div class="table-wrap"><table><thead><tr><th>{{call .T "web.sites.site"}}</th><th>{{call .T "web.sites.status"}}</th><th>{{call .T "web.sites.offers"}}</th><th>{{call .T "web.sites.products"}}</th><th>{{call .T "web.sites.observations"}}</th><th>{{call .T "web.sites.rejects"}}</th><th>{{call .T "web.sites.last_refresh"}}</th><th>{{call .T "web.sites.duration"}}</th><th>{{call .T "web.sites.breaker"}}</th><th>{{call .T "web.sites.median"}}</th></tr></thead><tbody>{{range .Sites}}<tr><td><strong>{{.Label}}</strong>{{if ne .Label .Name}}<div class="hint">{{.Name}}</div>{{end}}{{if .Error}}<div class="hint">{{.Error}}</div>{{end}}</td><td><span class="badge {{if eq .Status "actif"}}good{{else if or (eq .Status "erreur") (eq .Status "captcha")}}bad{{else}}warn{{end}}">{{.Status}}</span></td><td>{{.LastDeals}}</td><td>{{.Products}}</td><td>{{.Observations}}</td><td>{{.Rejected}}</td><td>{{if .LastScan.IsZero}}—{{else}}{{tsv .LastScan}}{{end}}</td><td>{{if .Duration}}{{.Duration.Round 1000000}}{{else}}—{{end}}</td><td>{{if .Breaker}}{{.Breaker}}{{else}}—{{end}}</td><td>{{if .MedianPricePerTB}}{{printf "%.2f" .MedianPricePerTB}}{{else}}—{{end}}</td></tr>{{else}}<tr><td colspan="10" class="empty">{{call $.T "web.sites.none"}}</td></tr>{{end}}</tbody></table></div>{{if .Error}}<div class="warnbox">{{call .T "web.common.error_prefix"}} {{.Error}}</div>{{end}}</div>
{{end}}`

const logsTpl = `{{define "body"}}
<div class="panel"><div class="panel-head"><h2>{{call .T "web.logs.title"}}</h2><span class="hint">{{call .T "web.logs.subtitle"}}</span></div><div class="panel-body log-list">{{range .Logs}}<div class="log-entry log-{{.Level}}"><span class="badge">{{.Level}}</span><time>{{tsv .At}}</time><span>{{.Message}}</span></div>{{else}}<div class="empty">{{call $.T "web.logs.none"}}</div>{{end}}</div></div>
<style>.log-list{display:grid;gap:8px}.log-entry{display:grid;grid-template-columns:90px 180px 1fr;gap:12px;align-items:center;padding:11px 12px;border-left:4px solid var(--brand);background:var(--soft)}.log-success{border-left-color:var(--good)}.log-warning{border-left-color:var(--warn)}.log-error{border-left-color:var(--bad)}.log-entry time{color:var(--muted);font-size:12px}@media(max-width:560px){.log-entry{grid-template-columns:1fr;gap:4px}}</style>
{{end}}
`
