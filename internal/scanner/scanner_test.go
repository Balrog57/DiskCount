package scanner

import (
	"context"
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
