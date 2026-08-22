package i18n

import (
	"testing"
)

func TestTReturnsTranslation(t *testing.T) {
	if got := T("web.nav.alerts", FR); got != "Alertes" {
		t.Fatalf("FR web.nav.alerts = %q", got)
	}
	if got := T("web.nav.alerts", EN); got != "Alerts" {
		t.Fatalf("EN web.nav.alerts = %q", got)
	}
}

func TestTFallsBackToDefaultOnMissingLocale(t *testing.T) {
	got := T("web.nav.alerts", Locale("xx"))
	if got != T("web.nav.alerts", Default) {
		t.Fatalf("unknown locale should fall back to default: %q", got)
	}
}

func TestTFallsBackToKeyWhenMissing(t *testing.T) {
	got := T("not.a.real.key", FR)
	if got != "not.a.real.key" {
		t.Fatalf("missing key should return itself, got %q", got)
	}
}

func TestTEmptyLocaleUsesDefault(t *testing.T) {
	got := T("web.nav.alerts", "")
	if got != T("web.nav.alerts", Default) {
		t.Fatalf("empty locale should use default, got %q", got)
	}
}

func TestParseLocale(t *testing.T) {
	cases := map[string]Locale{
		"fr":    FR,
		"fr-FR": FR,
		"en":    EN,
		"en-US": EN,
		"de":    Default,
		"":      Default,
		"f":     Default, // too short
	}
	for in, want := range cases {
		if got := ParseLocale(in); got != want {
			t.Errorf("ParseLocale(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetDefault(t *testing.T) {
	prev := Default
	defer SetDefault(prev)
	SetDefault(EN)
	if T("web.nav.alerts", "") != T("web.nav.alerts", EN) {
		t.Fatal("SetDefault(EN) should make empty locale resolve to EN")
	}
	SetDefault(FR)
	if T("web.nav.alerts", "") != T("web.nav.alerts", FR) {
		t.Fatal("SetDefault(FR) should make empty locale resolve to FR")
	}
}

func TestKnownLocalesIncludesFRAndEN(t *testing.T) {
	locs := KnownLocales()
	has := map[Locale]bool{}
	for _, l := range locs {
		has[l] = true
	}
	if !has[FR] || !has[EN] {
		t.Fatalf("expected both FR and EN, got %v", locs)
	}
}

// Both locales must share the same set of keys — otherwise the fallback
// strategy above would silently produce mixed-language UI. This test
// catches accidental drift.
func TestCatalogsHaveSameKeys(t *testing.T) {
	fr := catalogs[FR]
	en := catalogs[EN]
	for k := range fr {
		if _, ok := en[k]; !ok {
			t.Errorf("EN catalog missing key %q (present in FR)", k)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("FR catalog missing key %q (present in EN)", k)
		}
	}
}
