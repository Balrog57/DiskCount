package domain

import "testing"

func TestCanonicalProductKeyRequiresStableIdentity(t *testing.T) {
	brand, model := "Seagate", "IronWolf Pro ST16000NE000"
	if got := CanonicalProductKey(&brand, &model, 16); got != "seagate|ironwolfprost16000ne000|16.000" {
		t.Fatalf("unexpected key: %q", got)
	}
	if got := CanonicalProductKey(&brand, nil, 16); got != "" {
		t.Fatalf("missing model must not group: %q", got)
	}
}

func TestCanonicalProductKeyNormalizesWesternDigitalAlias(t *testing.T) {
	wd, westernDigital, model := "WD", "Western Digital", "WD161KFGX"
	if a, b := CanonicalProductKey(&wd, &model, 16), CanonicalProductKey(&westernDigital, &model, 16); a != b {
		t.Fatalf("brand aliases produced different keys: %q != %q", a, b)
	}
}
