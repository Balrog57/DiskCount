package sources

import (
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

func TestApplyListingJSONLDSkipsCompleteDeals(t *testing.T) {
	ean := "3760123456789"
	sku := "ST1000"
	brand := "Seagate"
	img := "https://example.com/disk.jpg"
	deals := []domain.Deal{{
		EAN:      &ean,
		SKU:      &sku,
		Brand:    &brand,
		ImageURL: &img,
	}}
	// HTML with JSON-LD that would overwrite fields if parsed.
	html := `<script type="application/ld+json">{"@type":"Product","gtin13":"999","sku":"OTHER","brand":"WD"}</script>`
	out := applyListingJSONLD(html, deals)
	if out[0].EAN == nil || *out[0].EAN != ean {
		t.Fatalf("ean should be unchanged, got %v", out[0].EAN)
	}
	if out[0].SKU == nil || *out[0].SKU != sku {
		t.Fatalf("sku should be unchanged, got %v", out[0].SKU)
	}
}
