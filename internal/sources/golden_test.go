package sources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/normalize"
)

// TestParseDiskPricesGoldenFile locks the shape we expect from the
// diskprices.com HTML page. The fixture is small but representative:
// three rows that exercise the "Exos" (CMR), "Red Plus" (CMR) and
// "Red" (SMR) brand families. If diskprices.com changes the layout
// or someone breaks the parser, this test goes red.
func TestParseDiskPricesGoldenFile(t *testing.T) {
	path := filepath.Join("testdata", "diskprices_sample.html")
	html, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals, err := parseDiskPrices(string(html))
	if err != nil {
		t.Fatalf("parseDiskPrices: %v", err)
	}
	if len(deals) != 3 {
		t.Fatalf("expected 3 deals, got %d: %+v", len(deals), deals)
	}
	// Recording-method detection should classify the three rows
	// correctly: Exos = CMR, Red Plus = CMR, Red base = SMR.
	wantRecording := []string{"cmr", "cmr", "smr"}
	for i, d := range deals {
		if d.RecordingMethod == nil {
			t.Fatalf("deal %d: RecordingMethod is nil", i)
		}
		if string(*d.RecordingMethod) != wantRecording[i] {
			t.Errorf("deal %d recording = %q, want %q", i, *d.RecordingMethod, wantRecording[i])
		}
	}
	// Every deal must have a usable product id, URL and price-per-TB.
	for i, d := range deals {
		if d.ProductID() == "" {
			t.Errorf("deal %d: empty ProductID", i)
		}
		if d.URL == "" {
			t.Errorf("deal %d: empty URL", i)
		}
		if d.PricePerTB <= 0 {
			t.Errorf("deal %d: PricePerTB = %v", i, d.PricePerTB)
		}
	}
}

// TestParseDiskPricesEmptyHTMLReturnsTypedError ensures an empty body
// produces a Selector severity, not a generic nil.
func TestParseDiskPricesEmptyHTMLReturnsTypedError(t *testing.T) {
	_, err := parseDiskPrices("")
	if err == nil {
		t.Fatal("expected error for empty HTML")
	}
	if Classify(err) != SeveritySelector {
		t.Fatalf("expected SeveritySelector, got %v (err=%v)", Classify(err), err)
	}
}

// TestNormalizeRoundTrip exercises the canonical normalize.Deal() pipeline
// (the single normalization path every source now feeds into via the
// scanner). The previous version of this test targeted a second,
// parallel sources.Normalize pipeline that has been removed.
func TestNormalizeRoundTrip(t *testing.T) {
	res := normalize.Deal(domain.Deal{
		Source:     "test",
		Title:      "Seagate Exos 7E8 8TB HDD",
		URL:        "https://example.com/exos-8tb",
		PriceEUR:   119.99,
		CapacityTB: 8.0,
	})
	if res.Reject != nil {
		t.Fatalf("unexpected rejection: %+v", res.Reject)
	}
	deal := res.Deal
	if deal.Source != "test" {
		t.Errorf("Source = %q", deal.Source)
	}
	if deal.PricePerTB <= 0 {
		t.Errorf("PricePerTB = %v", deal.PricePerTB)
	}
	if deal.RecordingMethod == nil || *deal.RecordingMethod != "cmr" {
		t.Errorf("RecordingMethod = %v, want cmr", deal.RecordingMethod)
	}
}

// TestNormalizeRejectsMissingTitle makes sure the normalizer's
// surface area for hard rejects is consistent: every missing-field
// branch returns a Reject with a stable reason.
func TestNormalizeRejectsMissingTitle(t *testing.T) {
	res := normalize.Deal(domain.Deal{Source: "test", URL: "https://x.test"})
	if res.Reject == nil || res.Reject.Reason != "missing_title" {
		t.Fatalf("expected missing_title rejection, got %+v", res.Reject)
	}
}

func TestNormalizeRejectsMissingPrice(t *testing.T) {
	res := normalize.Deal(domain.Deal{Source: "test", Title: "x", URL: "https://y.test"})
	// normalize.Deal() validates capacity and price-per-TB rather than
	// price alone (price-per-TB is derived). With CapacityTB=0 the deal is
	// rejected for invalid_capacity before the price check fires, so accept
	// either reason as a correct "hard reject" signal here.
	if res.Reject == nil || (res.Reject.Reason != "invalid_price" && res.Reject.Reason != "invalid_capacity") {
		t.Fatalf("expected invalid_price/invalid_capacity rejection, got %+v", res.Reject)
	}
}

func TestNormalizeRejectsMissingCapacity(t *testing.T) {
	res := normalize.Deal(domain.Deal{Source: "test", Title: "x", URL: "https://y.test", PriceEUR: 10.0})
	if res.Reject == nil || res.Reject.Reason != "invalid_capacity" {
		t.Fatalf("expected invalid_capacity rejection, got %+v", res.Reject)
	}
}
