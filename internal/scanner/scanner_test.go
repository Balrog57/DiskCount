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
		"REQUEST_TIMEOUT_SECONDS":      "1",
		"CIRCUIT_BREAKER_ENABLED":      "true",
		"CIRCUIT_BREAKER_THRESHOLD":    "2",
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
		"REQUEST_TIMEOUT_SECONDS":      "1",
		"CIRCUIT_BREAKER_ENABLED":      "true",
		"CIRCUIT_BREAKER_THRESHOLD":    "1",
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
