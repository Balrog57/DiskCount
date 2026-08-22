package domain

import "testing"

func TestCanonicalProductKeyPrefersEAN(t *testing.T) {
	ean, sku := "5901234123457", "ST16000NM000J"
	if got := CanonicalProductKey(&ean, &sku); got != "ean:5901234123457" {
		t.Fatalf("ean key: %q", got)
	}
}

func TestCanonicalProductKeyUsesMPN(t *testing.T) {
	sku := "ST16000NM000J"
	if got := CanonicalProductKey(nil, &sku); got != "mpn:st16000nm000j" {
		t.Fatalf("mpn key: %q", got)
	}
}

func TestCanonicalProductKeyUsesMerchantSKU(t *testing.T) {
	sku := "AR202204010054"
	if got := CanonicalProductKey(nil, &sku); got != "sku:ar202204010054" {
		t.Fatalf("merchant sku key: %q", got)
	}
}

func TestCanonicalProductKeyAcceptsASIN(t *testing.T) {
	asin := "B0BBJJVMT6"
	if got := CanonicalProductKey(nil, &asin); got != "asin:b0bbjjvmt6" {
		t.Fatalf("asin key: %q", got)
	}
}

func TestHasProductIdentifier(t *testing.T) {
	ean, sku := "5901234123457", "ST16000NM000J"
	if !HasProductIdentifier(&ean, nil) || !HasProductIdentifier(nil, &sku) || HasProductIdentifier(nil, nil) {
		t.Fatal("identifier presence mismatch")
	}
}

func TestNormalizeEAN(t *testing.T) {
	raw := "5 901234 123457"
	if got := NormalizeEAN(&raw); got != "5901234123457" {
		t.Fatalf("normalize ean: %q", got)
	}
}

func TestNormalizePartNumberExtractsFromTitle(t *testing.T) {
	if got := NormalizePartNumber("IronWolf Pro ST16000NE000"); got != "st16000ne000" {
		t.Fatalf("extract: %q", got)
	}
}
