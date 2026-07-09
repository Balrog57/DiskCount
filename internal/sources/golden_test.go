package sources

import (
	"os"
	"path/filepath"
	"testing"
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

// TestNormalizeRoundTrip checks that the shared normalizer turns a
// fully-formed RawDeal into a domain.Deal with consistent fields.
func TestNormalizeRoundTrip(t *testing.T) {
	price := 119.99
	capTB := 8.0
	deal, rej := Normalize(RawDeal{
		Source:     "test",
		Title:      "Seagate Exos 7E8 8TB",
		URL:        "https://example.com/exos-8tb",
		PriceEUR:   &price,
		CapacityTB: &capTB,
		Condition:  "new",
		MediaHint:  "HDD",
		Brand:      "Seagate",
		Model:      "Exos 7E8",
	})
	if rej != nil {
		t.Fatalf("unexpected rejection: %+v", rej)
	}
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
// branch returns a Rejection with a stable reason.
func TestNormalizeRejectsMissingTitle(t *testing.T) {
	_, rej := Normalize(RawDeal{Source: "test", URL: "x"})
	if rej == nil || rej.Reason != "missing_title" {
		t.Fatalf("expected missing_title rejection, got %+v", rej)
	}
}

func TestNormalizeRejectsMissingPrice(t *testing.T) {
	_, rej := Normalize(RawDeal{Source: "test", Title: "x", URL: "y"})
	if rej == nil || rej.Reason != "missing_price" {
		t.Fatalf("expected missing_price rejection, got %+v", rej)
	}
}

func TestNormalizeRejectsMissingCapacity(t *testing.T) {
	price := 10.0
	_, rej := Normalize(RawDeal{Source: "test", Title: "x", URL: "y", PriceEUR: &price})
	if rej == nil || rej.Reason != "missing_capacity" {
		t.Fatalf("expected missing_capacity rejection, got %+v", rej)
	}
}
