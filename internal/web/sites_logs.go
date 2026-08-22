package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Balrog57/DiskCount/internal/db"
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

func siteLabel(name string) string {
	labels := map[string]string{
		"alternate": "Alternate FR", "diskprices": "Amazon via DiskPrices", "pricepergig": "Amazon via PricePerGig", "pricepertb": "Amazon via PricePerTB", "keepa": "Amazon via Keepa",
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
	names := append(append([]string(nil), s.sourceNames...), "asus-shop-fr", "nvidia-fr")
	data["Sites"] = buildSiteStats(names, quality, health, lastReport(s.scanner))
	data["Error"] = err
	render(w, sitesTpl, data)
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
		if metric.Error != "" {
			entryLevel, message = "error", metric.Name+": "+metric.Error
		} else if metric.DealsFetched == 0 {
			entryLevel, message = "warning", metric.Name+": aucune offre"
		}
		out = append(out, logEntry{Level: entryLevel, At: at, Message: message})
	}
	for _, warning := range report.SourceWarnings {
		out = append(out, logEntry{Level: "warning", At: at, Message: warning.Name + ": " + warning.Message})
	}
	for _, message := range report.Errors {
		out = append(out, logEntry{Level: "error", At: at, Message: message})
	}
	return out
}

func formatScanSummary(report *scanner.ScanReport) string {
	return "fetched=" + strconv.Itoa(report.Fetched) + ", accepted=" + strconv.Itoa(report.Accepted) + ", rejected=" + strconv.Itoa(report.Rejected) + ", erreurs=" + strconv.Itoa(len(report.Errors))
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	data := s.baseWithRequest(r, "Logs", "logs")
	if s.scanner != nil {
		data["Logs"] = scanLogEntries(s.scanner.LastReport())
	}
	render(w, logsTpl, data)
}

const sitesTpl = `{{define "body"}}
<div class="panel"><div class="panel-head"><h2>Statistiques des sites</h2><span class="hint">État du dernier scan et qualité des offres par fournisseur</span></div><div class="table-wrap"><table><thead><tr><th>Site</th><th>Statut</th><th>Offres</th><th>Produits</th><th>Observations</th><th>Rejets</th><th>Dernier refresh</th><th>Durée</th><th>Breaker</th><th>Médiane €/To</th></tr></thead><tbody>{{range .Sites}}<tr><td><strong>{{.Label}}</strong>{{if ne .Label .Name}}<div class="hint">{{.Name}}</div>{{end}}{{if .Error}}<div class="hint">{{.Error}}</div>{{end}}</td><td><span class="badge {{if eq .Status "actif"}}good{{else}}warn{{end}}">{{.Status}}</span></td><td>{{.LastDeals}}</td><td>{{.Products}}</td><td>{{.Observations}}</td><td>{{.Rejected}}</td><td>{{if .LastScan.IsZero}}—{{else}}{{tsv .LastScan}}{{end}}</td><td>{{if .Duration}}{{.Duration.Round 1000000}}{{else}}—{{end}}</td><td>{{if .Breaker}}{{.Breaker}}{{else}}—{{end}}</td><td>{{if .MedianPricePerTB}}{{printf "%.2f" .MedianPricePerTB}}{{else}}—{{end}}</td></tr>{{else}}<tr><td colspan="10" class="empty">Aucun fournisseur configuré.</td></tr>{{end}}</tbody></table></div>{{if .Error}}<div class="warnbox">Erreur de statistiques : {{.Error}}</div>{{end}}</div>
{{end}}`

const logsTpl = `{{define "body"}}
<div class="panel"><div class="panel-head"><h2>Journal du dernier scan</h2><span class="hint">Les erreurs sont rouges, les avertissements orange.</span></div><div class="panel-body log-list">{{range .Logs}}<div class="log-entry log-{{.Level}}"><span class="badge">{{.Level}}</span><time>{{tsv .At}}</time><span>{{.Message}}</span></div>{{else}}<div class="empty">Aucun scan enregistré.</div>{{end}}</div></div>
<style>.log-list{display:grid;gap:8px}.log-entry{display:grid;grid-template-columns:90px 180px 1fr;gap:12px;align-items:center;padding:11px 12px;border-left:4px solid var(--brand);background:var(--soft);border-radius:8px}.log-success{border-left-color:var(--good)}.log-warning{border-left-color:var(--warn)}.log-error{border-left-color:var(--bad)}.log-entry time{color:var(--muted);font-size:12px}@media(max-width:560px){.log-entry{grid-template-columns:1fr;gap:4px}}</style>
{{end}}`
