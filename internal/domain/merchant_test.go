package domain

import "testing"

func TestMerchantFromURLAmazon(t *testing.T) {
	slug, display, ok := MerchantFromURL("https://www.amazon.fr/dp/B012345678")
	if !ok || slug != "amazon.fr" || display != "Amazon France" {
		t.Fatalf("amazon.fr: slug=%q display=%q ok=%v", slug, display, ok)
	}
	slug, _, ok = MerchantFromURL("https://www.amazon.de/gp/product/B0TEST")
	if !ok || slug != "amazon.de" {
		t.Fatalf("amazon.de: slug=%q ok=%v", slug, ok)
	}
}

func TestMerchantFromURLFrenchRetailers(t *testing.T) {
	cases := []struct {
		url, wantSlug string
	}{
		{"https://www.ldlc.com/fiche/PB1.html", "ldlc"},
		{"https://www.fnac.com/a12345678", "fnac"},
		{"https://www.darty.com/nav/achat", "darty"},
	}
	for _, c := range cases {
		slug, _, ok := MerchantFromURL(c.url)
		if !ok || slug != c.wantSlug {
			t.Fatalf("%s: slug=%q ok=%v want %q", c.url, slug, ok, c.wantSlug)
		}
	}
}

func TestResolveMerchantFromAggregatorFetcher(t *testing.T) {
	d := Deal{
		Source: "diskprices",
		URL:    "https://www.amazon.fr/dp/B012345678",
	}
	if !ResolveMerchant(&d) {
		t.Fatal("expected resolve")
	}
	if d.Source != "amazon.fr" || d.Merchant == nil || *d.Merchant != "Amazon France" {
		t.Fatalf("unexpected deal: source=%q merchant=%v", d.Source, d.Merchant)
	}
}

func TestNeedsConcreteMerchantRejectsGeizhalsListing(t *testing.T) {
	d := Deal{
		Source: "geizhals",
		URL:    "https://geizhals.de/lexar-a2956595.html#offerlist",
	}
	if !NeedsConcreteMerchant(d) {
		t.Fatal("geizhals listing should need concrete merchant")
	}
	ResolveMerchant(&d)
	if !NeedsConcreteMerchant(d) {
		t.Fatal("still aggregator after failed resolve")
	}
}
