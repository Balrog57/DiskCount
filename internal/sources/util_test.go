package sources

import (
	"strings"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestAmazonImageURL(t *testing.T) {
	if got := amazonImageURL(" b012345678 "); got != "https://m.media-amazon.com/images/P/B012345678.jpg" {
		t.Fatalf("amazonImageURL: got %q", got)
	}
	if got := amazonImageURL("short"); got != "" {
		t.Fatalf("invalid ASIN: got %q", got)
	}
}

func TestApplyListingJSONLDSkipsWithoutScriptTag(t *testing.T) {
	deals := []domain.Deal{{Title: "Drive", URL: "https://example.test/d"}}
	html := strings.Repeat("<tr><td>table row</td></tr>", 10_000)
	out := applyListingJSONLD(html, deals)
	if len(out) != 1 || out[0].Brand != nil {
		t.Fatalf("expected unchanged deal without JSON-LD, got %#v", out[0])
	}
}

func TestApplyListingJSONLDEnrichesFromPageJSONLD(t *testing.T) {
	html := `<html><head><script type="application/ld+json">{"@type":"Product","brand":"Seagate","sku":"ST18000","gtin13":"123","image":"https://img.test/x.jpg"}</script></head></html>`
	deals := []domain.Deal{{Title: "Drive", URL: "https://example.test/d"}}
	out := applyListingJSONLD(html, deals)
	if out[0].Brand == nil || *out[0].Brand != "Seagate" {
		t.Fatalf("expected brand from JSON-LD, got %#v", out[0].Brand)
	}
	if out[0].SKU == nil || *out[0].SKU != "ST18000" {
		t.Fatalf("expected sku from JSON-LD, got %#v", out[0].SKU)
	}
}

func TestApplyListingJSONLDSkipsWhenDealsFullyEnriched(t *testing.T) {
	brand, sku, ean, image := "WD", "WD180", "999", "https://img.test/y.jpg"
	deals := []domain.Deal{{
		Title: "Drive", URL: "https://example.test/d",
		Brand: &brand, SKU: &sku, EAN: &ean, ImageURL: &image,
	}}
	html := `<html><head><script type="application/ld+json">{"@type":"Product","brand":"Other"}</script></head></html>`
	out := applyListingJSONLD(html, deals)
	if out[0].Brand == nil || *out[0].Brand != "WD" {
		t.Fatalf("fully enriched deals must not be overwritten, got %#v", out[0].Brand)
	}
}

func BenchmarkApplyListingJSONLDNoJSONLD(b *testing.B) {
	deals := []domain.Deal{{Title: "Drive", URL: "https://example.test/d"}}
	html := strings.Repeat("<tr><td>row</td></tr>", 50_000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		applyListingJSONLD(html, deals)
	}
}
