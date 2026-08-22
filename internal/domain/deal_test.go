package domain

import "testing"

func TestDealCanonicalProductKeyRequiresIdentifier(t *testing.T) {
	ean := "5901234123457"
	d := Deal{EAN: &ean, CapacityTB: 16}
	if got := d.CanonicalProductKey(); got != "ean:5901234123457" {
		t.Fatalf("unexpected key: %q", got)
	}
	d.EAN = nil
	if got := d.CanonicalProductKey(); got != "" {
		t.Fatalf("missing identifier must not group: %q", got)
	}
}

func TestDealCarriesSKUAndImageURL(t *testing.T) {
	sku, ean, img := "ST16000NM000J", "5901234123457", "https://example.test/drive.jpg"
	d := Deal{SKU: &sku, EAN: &ean, ImageURL: &img}
	if d.SKU == nil || *d.SKU != sku || d.EAN == nil || *d.EAN != ean || d.ImageURL == nil || *d.ImageURL != img {
		t.Fatalf("sku/ean/image not retained: %#v", d)
	}
}
