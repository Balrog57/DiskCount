package scanner

import (
	"context"
	"errors"
	"testing"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/sources"
)

type emptySource struct{}

func (emptySource) Name() string { return "empty" }
func (emptySource) Fetch(context.Context) ([]domain.Deal, error) {
	return nil, nil
}

type invalidSource struct{}

func (invalidSource) Name() string { return "invalid" }
func (invalidSource) Fetch(context.Context) ([]domain.Deal, error) {
	return []domain.Deal{{Source: "invalid", Title: "", URL: "", PriceEUR: 0, CapacityTB: 0}}, nil
}

type failingSource struct{ n int }

func (f failingSource) Name() string { return "failing" }
func (f failingSource) Fetch(context.Context) ([]domain.Deal, error) {
	return nil, errors.New("boom")
}

type blockedSource struct{}

func (blockedSource) Name() string { return "darty" }
func (blockedSource) Fetch(context.Context) ([]domain.Deal, error) {
	return nil, sources.Blocked("darty", errors.New("captcha challenge"))
}

func TestRunOnceStoresLastReport(t *testing.T) {
	cfg := config.LoadWithAppValues(map[string]string{
		"REQUEST_TIMEOUT_SECONDS": "1",
	})
	scan := New(cfg, nil, []sources.Source{emptySource{}}, nil)
	report := scan.RunOnce(context.Background(), true)
	if report.Fetched != 0 || !report.DryRun {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.StartedAt.IsZero() || report.FinishedAt.IsZero() {
		t.Fatalf("report timestamps not set: %#v", report)
	}
	last := scan.LastReport()
	if last == nil {
		t.Fatal("last report not stored")
	}
	if last.FinishedAt != report.FinishedAt {
		t.Fatalf("last report mismatch")
	}
}

func TestRunOncePersistsFetchErrorInSourceMetrics(t *testing.T) {
	cfg := config.LoadWithAppValues(map[string]string{
		"REQUEST_TIMEOUT_SECONDS": "1",
	})
	report := New(cfg, nil, []sources.Source{blockedSource{}}, nil).RunOnce(context.Background(), true)
	if len(report.SourceMetrics) != 1 {
		t.Fatalf("expected one source metric, got %#v", report.SourceMetrics)
	}
	metric := report.SourceMetrics[0]
	if metric.Error == "" {
		t.Fatal("fetch error was not persisted in source metrics")
	}
	if metric.BlockedByKeyword == "" {
		t.Fatal("blocked keyword was not persisted in source metrics")
	}
}

func TestRunOnceCountsRejectedDeals(t *testing.T) {
	cfg := config.LoadWithAppValues(map[string]string{
		"REQUEST_TIMEOUT_SECONDS": "1",
	})
	scan := New(cfg, nil, []sources.Source{invalidSource{}}, nil)
	report := scan.RunOnce(context.Background(), true)
	if report.Fetched != 1 || report.Accepted != 0 || report.Rejected != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.RejectReasons["missing_title"] != 1 {
		t.Fatalf("missing reject reason: %#v", report.RejectReasons)
	}
}

func TestCircuitBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	cfg := config.LoadWithAppValues(map[string]string{
		"REQUEST_TIMEOUT_SECONDS":         "1",
		"CIRCUIT_BREAKER_ENABLED":         "true",
		"CIRCUIT_BREAKER_THRESHOLD":       "2",
		"CIRCUIT_BREAKER_TIMEOUT_SECONDS": "60",
	})
	scan := New(cfg, nil, []sources.Source{failingSource{}}, nil)

	for i := 0; i < 3; i++ {
		_ = scan.RunOnce(context.Background(), true)
	}
	snap := scan.BreakerSnapshot()
	state, ok := snap["failing"]
	if !ok {
		t.Fatalf("breaker not registered: %#v", snap)
	}
	if state == "closed" {
		t.Fatalf("breaker should have opened: %s", state)
	}
	// The 4th call should be skipped and counted as a breaker_skip.
	r := scan.RunOnce(context.Background(), true)
	if _, skipped := r.BreakerSkips["failing"]; !skipped {
		t.Fatalf("expected failing source to be skipped by breaker: %#v", r.BreakerSkips)
	}
}

func TestResetBreaker(t *testing.T) {
	cfg := config.LoadWithAppValues(map[string]string{
		"REQUEST_TIMEOUT_SECONDS":         "1",
		"CIRCUIT_BREAKER_ENABLED":         "true",
		"CIRCUIT_BREAKER_THRESHOLD":       "1",
		"CIRCUIT_BREAKER_TIMEOUT_SECONDS": "60",
	})
	scan := New(cfg, nil, []sources.Source{failingSource{}}, nil)
	_ = scan.RunOnce(context.Background(), true)
	if state := scan.BreakerSnapshot()["failing"]; state == "closed" {
		t.Fatalf("breaker should be open after one failure: %s", state)
	}
	// Resetting the breaker removes its entry. The next snapshot
	// reflects only the breakers that have actually been touched by
	// a Fetch call.
	scan.ResetBreaker("failing")
	if _, present := scan.BreakerSnapshot()["failing"]; present {
		t.Fatal("breaker should be cleared after reset")
	}
}

// recordScanResult must bump the streak on a zero-deal scan, flag the
// source in SourceHealth() once the threshold is reached, and reset the
// counter the first time the source returns a deal again.
func TestRecordScanResultZeroStreak(t *testing.T) {
	cfg := config.LoadWithAppValues(map[string]string{
		"SOURCE_HEALTH_STREAK_THRESHOLD": "3",
		"SOURCE_HEALTH_NOTIFY":           "false",
	})
	scan := New(cfg, nil, nil, nil)

	// First two zero-deal scans: under threshold, no warning.
	for i := 0; i < 2; i++ {
		scan.recordScanResult("alpha", 0, &ScanReport{})
	}
	health := scan.SourceHealth()
	if len(health) != 0 {
		// Source is not registered in s.srcs, so SourceHealth returns
		// nothing for it. Verify the streak directly instead.
	}
	scan.zeroStreakMu.RLock()
	streak := scan.zeroStreak["alpha"]
	scan.zeroStreakMu.RUnlock()
	if streak != 2 {
		t.Fatalf("streak should be 2, got %d", streak)
	}

	// Third zero-deal scan crosses the threshold and emits a warning.
	r := &ScanReport{}
	scan.recordScanResult("alpha", 0, r)
	if len(r.SourceWarnings) != 1 {
		t.Fatalf("expected one warning, got %d: %#v", len(r.SourceWarnings), r.SourceWarnings)
	}
	if r.SourceWarnings[0].ConsecutiveZeros != 3 {
		t.Fatalf("warning streak = %d, want 3", r.SourceWarnings[0].ConsecutiveZeros)
	}

	// A successful scan clears the streak.
	scan.recordScanResult("alpha", 5, &ScanReport{})
	scan.zeroStreakMu.RLock()
	streak = scan.zeroStreak["alpha"]
	scan.zeroStreakMu.RUnlock()
	if streak != 0 {
		t.Fatalf("streak should be 0 after a deal, got %d", streak)
	}
}

// TestSourceHealthExposesRegisteredSources builds a real Scanner with one
// source and verifies that SourceHealth() returns an entry for it (with
// the correct streak) before the first scan runs.
func TestSourceHealthExposesRegisteredSources(t *testing.T) {
	cfg := config.LoadWithAppValues(map[string]string{
		"SOURCE_HEALTH_STREAK_THRESHOLD": "2",
	})
	scan := New(cfg, nil, []sources.Source{emptySource{}}, nil)
	scan.recordScanResult("empty", 0, &ScanReport{})
	health := scan.SourceHealth()
	if len(health) != 1 {
		t.Fatalf("expected 1 entry, got %d: %#v", len(health), health)
	}
	if health[0].Name != "empty" || health[0].ConsecutiveZeros != 1 || health[0].Flagged {
		t.Fatalf("unexpected entry: %#v", health[0])
	}
	// Cross the threshold.
	scan.recordScanResult("empty", 0, &ScanReport{})
	health = scan.SourceHealth()
	if !health[0].Flagged {
		t.Fatalf("source should be flagged at streak=2: %#v", health[0])
	}
}
