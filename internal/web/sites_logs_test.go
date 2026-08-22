package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/scanner"
)

func TestBuildSiteStatsIncludesConfiguredAndObservedSources(t *testing.T) {
	median := 8.5
	got := buildSiteStats([]string{"zeta", "alpha"}, &db.QualityStats{Sources: []db.SourceQuality{{Source: "alpha", Products: 2, MedianPricePerTB: &median}, {Source: "orphan", Observations: 4}}}, []scanner.SourceHealthEntry{{Name: "zeta", Flagged: true, LastDeals: 3}}, nil)
	if len(got) != 3 || got[0].Name != "alpha" || got[1].Name != "orphan" || got[2].Status != "a surveiller" {
		t.Fatalf("unexpected site stats: %#v", got)
	}
	if got[0].MedianPricePerTB == nil || *got[0].MedianPricePerTB != median {
		t.Fatalf("median missing: %#v", got[0])
	}
}

func TestSitesAndLogsTemplatesRenderOperationalData(t *testing.T) {
	now := time.Now().UTC()
	rec := httptest.NewRecorder()
	render(rec, sitesTpl, map[string]any{"Title": "Sites", "Active": "sites", "Sites": []siteStat{{Name: "pricepergig", Label: "Amazon via PricePerGig", Status: "actif", LastDeals: 50, LastScan: now, Duration: time.Second, Breaker: "closed"}}})
	for _, required := range []string{"Amazon via PricePerGig", "Dernier refresh", "closed", "/logs", "Creer une alerte"} {
		if !strings.Contains(rec.Body.String(), required) {
			t.Fatalf("sites page missing %q: %s", required, rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	render(rec, logsTpl, map[string]any{"Title": "Logs", "Active": "logs", "Logs": []logEntry{{Level: "success", At: now, Message: "scan ok"}, {Level: "warning", At: now, Message: "vide"}, {Level: "error", At: now, Message: "timeout"}}})
	for _, required := range []string{"log-success", "log-warning", "log-error", "scan ok"} {
		if !strings.Contains(rec.Body.String(), required) {
			t.Fatalf("logs page missing %q: %s", required, rec.Body.String())
		}
	}
}

func TestScanLogEntriesColorLevelsAndSummary(t *testing.T) {
	report := &scanner.ScanReport{Fetched: 3, Accepted: 2, Rejected: 1, FinishedAt: time.Unix(10, 0), SourceWarnings: []scanner.SourceWarning{{Name: "shop", Message: "vide"}}, Errors: []string{"db: timeout"}}
	logs := scanLogEntries(report)
	if len(logs) != 3 || logs[0].Level != "warning" || logs[1].Level != "warning" || logs[2].Level != "error" {
		t.Fatalf("unexpected levels: %#v", logs)
	}
	if !strings.Contains(logs[0].Message, "fetched=3") || !strings.Contains(logs[2].Message, "timeout") {
		t.Fatalf("unexpected messages: %#v", logs)
	}
}
