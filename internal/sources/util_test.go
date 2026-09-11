package sources

import (
	"strings"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func strVal(s string) *string { return &s }

func TestListingDealsNeedJSONLD(t *testing.T) {
	complete := domain.Deal{
		EAN: strVal("123"), SKU: strVal("ST1000"), Brand: strVal("Seagate"), ImageURL: strVal("https://example/img.jpg"),
	}
	if listingDealsNeedJSONLD([]domain.Deal{complete}) {
		t.Fatal("fully enriched deal should not need page JSON-LD")
	}
	incomplete := complete
	incomplete.SKU = nil
	if !listingDealsNeedJSONLD([]domain.Deal{incomplete}) {
		t.Fatal("deal missing SKU should still need page JSON-LD")
	}
}

func TestApplyListingJSONLDSkipsParseWhenEnriched(t *testing.T) {
	// Large HTML forces a real goquery parse when the slow path runs.
	html := `<html><head>` + strings.Repeat(`<script type="application/ld+json">{"@type":"Product","sku":"SHOULD-NOT-APPLY"}</script>`, 20) + `</head><body></body></html>`
	deals := []domain.Deal{{
		EAN: strVal("123"), SKU: strVal("ST1000"), Brand: strVal("Seagate"), ImageURL: strVal("https://example/img.jpg"),
	}}
	out := applyListingJSONLD(html, deals)
	if out[0].SKU == nil || *out[0].SKU != "ST1000" {
		t.Fatalf("enriched deal should be unchanged, got SKU=%v", out[0].SKU)
	}
}

func TestApplyListingJSONLDFillsMissingFields(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"Product","brand":"WD","sku":"WDS100T","gtin13":"999","image":"https://example/wd.jpg"}</script>`
	deals := []domain.Deal{{Title: "WD Blue 1 To"}}
	out := applyListingJSONLD(html, deals)
	if out[0].Brand == nil || *out[0].Brand != "WD" {
		t.Fatalf("brand: got %v", out[0].Brand)
	}
	if out[0].SKU == nil || *out[0].SKU != "WDS100T" {
		t.Fatalf("sku: got %v", out[0].SKU)
	}
	if out[0].EAN == nil || *out[0].EAN != "999" {
		t.Fatalf("ean: got %v", out[0].EAN)
	}
	if out[0].ImageURL == nil || *out[0].ImageURL != "https://example/wd.jpg" {
		t.Fatalf("image: got %v", out[0].ImageURL)
	}
}

func BenchmarkApplyListingJSONLDEnrichedSkip(b *testing.B) {
	html := `<html><head>` + strings.Repeat(`<script type="application/ld+json">{"@type":"Product","sku":"X"}</script>`, 50) + `</head><body>` + strings.Repeat("<div>item</div>", 500) + `</body></html>`
	deals := make([]domain.Deal, 48)
	for i := range deals {
		deals[i] = domain.Deal{
			EAN: strVal("123"), SKU: strVal("ST1000"), Brand: strVal("Seagate"), ImageURL: strVal("https://example/img.jpg"),
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		applyListingJSONLD(html, deals)
	}
}

func BenchmarkApplyListingJSONLDNeedsParse(b *testing.B) {
	html := `<html><head><script type="application/ld+json">{"@type":"Product","brand":"WD","sku":"WDS100T"}</script></head><body>` + strings.Repeat("<div>item</div>", 500) + `</body></html>`
	deals := []domain.Deal{{Title: "WD Blue 1 To"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		applyListingJSONLD(html, deals)
	}
}

func TestAmazonImageURL(t *testing.T) {
	if got := amazonImageURL(" b012345678 "); got != "https://m.media-amazon.com/images/P/B012345678.jpg" {
		t.Fatalf("amazonImageURL: got %q", got)
	}
	if got := amazonImageURL("short"); got != "" {
		t.Fatalf("invalid ASIN: got %q", got)
	}
}
