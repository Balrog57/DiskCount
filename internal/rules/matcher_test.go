package rules

import (
	"testing"

	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/domain"
)

func condNew() *domain.Condition { c := domain.ConditionNew; return &c }
func mediaRot() *domain.MediaType { m := domain.MediaTypeRotational; return &m }

func baseDeal() domain.Deal {
	return domain.Deal{
		Source:     "diskprices",
		Title:      "Seagate Exos X20 18TB CMR HDD",
		URL:        "https://example.com/exos",
		PriceEUR:   300,
		PricePerTB: 16.67,
		CapacityTB: 18,
		Condition:  condNew(),
		MediaType:  mediaRot(),
	}
}

func TestAlertMatchesBrand(t *testing.T) {
	deal := baseDeal()
	seagate := "Seagate"
	deal.Brand = &seagate

	t.Run("brand match", func(t *testing.T) {
		a := &db.Alert{Brands: []string{"Seagate"}}
		if !AlertMatches(a, deal) {
			t.Fatal("expected brand Seagate to match")
		}
	})
	t.Run("brand mismatch", func(t *testing.T) {
		a := &db.Alert{Brands: []string{"WD"}}
		if AlertMatches(a, deal) {
			t.Fatal("expected WD brand not to match Seagate deal")
		}
	})
	t.Run("brand case insensitive", func(t *testing.T) {
		a := &db.Alert{Brands: []string{"seagate"}}
		if !AlertMatches(a, deal) {
			t.Fatal("brand match should be case-insensitive")
		}
	})
	t.Run("no brand restriction matches all", func(t *testing.T) {
		a := &db.Alert{}
		if !AlertMatches(a, deal) {
			t.Fatal("empty brand list should match")
		}
	})
	t.Run("brand restriction rejects unknown brand", func(t *testing.T) {
		dealNoBrand := deal
		dealNoBrand.Brand = nil
		a := &db.Alert{Brands: []string{"Seagate"}}
		if AlertMatches(a, dealNoBrand) {
			t.Fatal("deal with no brand should not match a brand-restricted alert")
		}
	})
}

func TestAlertMatchesKeyword(t *testing.T) {
	deal := baseDeal()

	t.Run("include keyword present", func(t *testing.T) {
		a := &db.Alert{Keywords: []string{"Exos"}}
		if !AlertMatches(a, deal) {
			t.Fatal("keyword Exos should match title")
		}
	})
	t.Run("include keyword absent", func(t *testing.T) {
		a := &db.Alert{Keywords: []string{"IronWolf"}}
		if AlertMatches(a, deal) {
			t.Fatal("keyword IronWolf should not match")
		}
	})
	t.Run("include keyword case insensitive", func(t *testing.T) {
		a := &db.Alert{Keywords: []string{"exos x20"}}
		if !AlertMatches(a, deal) {
			t.Fatal("keyword should match case-insensitively")
		}
	})
	t.Run("all include keywords must be present", func(t *testing.T) {
		a := &db.Alert{Keywords: []string{"Exos", "Ultrastar"}}
		if AlertMatches(a, deal) {
			t.Fatal("not all keywords present should not match")
		}
	})
	t.Run("exclude keyword present rejects", func(t *testing.T) {
		a := &db.Alert{ExcludeKeywords: []string{"CMR"}}
		if AlertMatches(a, deal) {
			t.Fatal("exclude keyword CMR present should reject")
		}
	})
	t.Run("exclude keyword absent allows", func(t *testing.T) {
		a := &db.Alert{ExcludeKeywords: []string{"Archive"}}
		if !AlertMatches(a, deal) {
			t.Fatal("exclude keyword absent should allow")
		}
	})
}

func TestAlertMatchesRecordingMethod(t *testing.T) {
	cmr := domain.RecordingMethodCMR
	deal := baseDeal()
	deal.RecordingMethod = &cmr

	t.Run("cmr matches cmr-only", func(t *testing.T) {
		a := &db.Alert{RecordingMethods: []string{"cmr"}}
		if !AlertMatches(a, deal) {
			t.Fatal("CMR deal should match CMR-only alert")
		}
	})
	t.Run("cmr does not match smr-only", func(t *testing.T) {
		a := &db.Alert{RecordingMethods: []string{"smr"}}
		if AlertMatches(a, deal) {
			t.Fatal("CMR deal should not match SMR-only alert")
		}
	})
	t.Run("no restriction matches", func(t *testing.T) {
		a := &db.Alert{}
		if !AlertMatches(a, deal) {
			t.Fatal("no recording restriction should match")
		}
	})
	t.Run("unknown method allowed through restriction", func(t *testing.T) {
		dealUnknown := deal
		dealUnknown.RecordingMethod = nil
		a := &db.Alert{RecordingMethods: []string{"cmr"}}
		if !AlertMatches(a, dealUnknown) {
			t.Fatal("unknown recording method should be allowed through (not excluded)")
		}
	})
}

func TestAlertMatchesCapacityPresetMedia(t *testing.T) {
	hdd := baseDeal()
	hdd.CapacityTB = 1.0
	ssd := baseDeal()
	ssd.CapacityTB = 1.0
	m := domain.MediaTypeSolidState
	ssd.MediaType = &m

	a := &db.Alert{CapacityPresets: []string{"ssd_1"}}
	if AlertMatches(a, hdd) {
		t.Fatal("SSD ~1 To preset must not match a 1 To HDD")
	}
	if !AlertMatches(a, ssd) {
		t.Fatal("SSD ~1 To preset must match a 1 To SSD")
	}
}
