package normalize

import (
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestDealRejectsIncompleteData(t *testing.T) {
	res := Deal(domain.Deal{Source: "test", Title: "", URL: "", CapacityTB: 0, PriceEUR: 0})
	if res.Reject == nil || res.Reject.Reason != "missing_title" {
		t.Fatalf("expected missing_title rejection, got %#v", res.Reject)
	}
}

func TestDealEnrichesAndScoresValidData(t *testing.T) {
	res := Deal(domain.Deal{
		Source:     "test",
		Title:      "Samsung 990 PRO SSD 2 To M.2 NVMe",
		URL:        "https://example.test/product?utm_source=x",
		CapacityTB: 2,
		PriceEUR:   149.99,
	})
	if res.Reject != nil {
		t.Fatalf("valid deal rejected: %#v", res.Reject)
	}
	if res.Deal.QualityScore < MinQualityForAlert {
		t.Fatalf("score too low: %d", res.Deal.QualityScore)
	}
	if res.Deal.MediaType == nil || *res.Deal.MediaType != domain.MediaTypeSolidState {
		t.Fatalf("media not enriched: %#v", res.Deal.MediaType)
	}
	if res.Deal.DriveCategory == nil || *res.Deal.DriveCategory != domain.DriveCategoryM2NVMe {
		t.Fatalf("category not enriched: %#v", res.Deal.DriveCategory)
	}
	if res.Deal.CanonicalURL != "https://example.test/product" {
		t.Fatalf("canonical URL mismatch: %q", res.Deal.CanonicalURL)
	}
}

func TestDealRejectsAberrantPricePerTB(t *testing.T) {
	res := Deal(domain.Deal{
		Source:     "test",
		Title:      "Bad Drive 1 To",
		URL:        "https://example.test/bad",
		CapacityTB: 1,
		PriceEUR:   9000,
	})
	if res.Reject == nil || res.Reject.Reason != "invalid_price_per_tb" {
		t.Fatalf("expected invalid price per TB, got %#v", res.Reject)
	}
}

func TestDealRejectsUnknownMedia(t *testing.T) {
	res := Deal(domain.Deal{
		Source:     "test",
		Title:      "DDR5 RAM 32 Go kit",
		URL:        "https://example.test/ram",
		CapacityTB: 0.032,
		PriceEUR:   99,
		PricePerTB: 3093.75,
	})
	if res.Reject == nil || res.Reject.Reason != "invalid_capacity" {
		t.Fatalf("expected invalid capacity before media acceptance, got %#v", res.Reject)
	}

	res = Deal(domain.Deal{
		Source:     "test",
		Title:      "DDR5 RAM kit 1 To",
		URL:        "https://example.test/ram",
		CapacityTB: 1,
		PriceEUR:   99,
		PricePerTB: 99,
	})
	if res.Reject == nil || res.Reject.Reason != "unsupported_product" {
		t.Fatalf("expected unsupported product rejection, got %#v", res.Reject)
	}
}

func TestDealRejectsMemoryCards(t *testing.T) {
	res := Deal(domain.Deal{
		Source:     "test",
		Title:      "Lexar Professional CFexpress 128 Go Type B Carte Memoire PCIe NVMe",
		URL:        "https://example.test/cfexpress",
		CapacityTB: 0.128,
		PriceEUR:   180,
		PricePerTB: 1406.25,
	})
	if res.Reject == nil || res.Reject.Reason != "unsupported_product" {
		t.Fatalf("expected unsupported memory card rejection, got %#v", res.Reject)
	}
}

func TestDealAdjustsPackCapacity(t *testing.T) {
	res := Deal(domain.Deal{
		Source:     "test",
		Title:      "Gigastone SSD 1To NAS SSD Disque Lot de 12 SATA",
		URL:        "https://example.test/pack",
		CapacityTB: 1,
		PriceEUR:   1200,
		PricePerTB: 1200,
	})
	if res.Reject != nil {
		t.Fatalf("pack deal rejected: %#v", res.Reject)
	}
	if res.Deal.CapacityTB != 12 || res.Deal.PricePerTB != 100 {
		t.Fatalf("pack capacity not adjusted: %#v", res.Deal)
	}
}

func TestDealDoesNotTreatXboxAsPack(t *testing.T) {
	res := Deal(domain.Deal{
		Source:     "test",
		Title:      "Seagate Game Drive pour Xbox 5To HDD Portable USB",
		URL:        "https://example.test/xbox-drive",
		CapacityTB: 5,
		PriceEUR:   153,
		PricePerTB: 30.6,
	})
	if res.Reject != nil {
		t.Fatalf("xbox drive rejected: %#v", res.Reject)
	}
	if res.Deal.CapacityTB != 5 || res.Deal.PricePerTB != 30.6 {
		t.Fatalf("xbox text treated as pack: %#v", res.Deal)
	}
}

// TestInferBrandMatchesMidTitle locks in the fix for the previous
// implementation, which took the first whitespace token as the brand.
// A title like "2 To Seagate IronWolf" used to be branded "2"; it must
// now be "Seagate" so brand-based alerts actually match.
func TestInferBrandMatchesMidTitle(t *testing.T) {
	cases := map[string]string{
		"2 To Seagate IronWolf Pro 16 To":       "Seagate",
		"WD Red Plus 4 To NAS HDD":              "WD",
		"Western Digital Red 8 To":              "Western Digital",
		"Toshiba MG08 16 To Enterprise":         "Toshiba",
		"samsung 990 pro 2 to ssd":              "Samsung", // case-insensitive
		"Disque Générique 4 To Marque Inconnue": "",        // unknown → no junk brand
		"4 To": "", // capacity only, no brand
	}
	for title, want := range cases {
		if got := inferBrand(title); got != want {
			t.Errorf("inferBrand(%q) = %q, want %q", title, got, want)
		}
	}
}

// TestDealEnrichesBrandFromTitle verifies the canonical pipeline actually
// populates Deal.Brand via inferBrand for a realistic title.
func TestDealEnrichesBrandFromTitle(t *testing.T) {
	res := Deal(domain.Deal{
		Source:     "test",
		Title:      "2 To Seagate IronWolf HDD",
		URL:        "https://example.test/ironwolf",
		CapacityTB: 2,
		PriceEUR:   60,
	})
	if res.Reject != nil {
		t.Fatalf("deal rejected: %#v", res.Reject)
	}
	if res.Deal.Brand == nil || *res.Deal.Brand != "Seagate" {
		t.Fatalf("brand not enriched: %#v", res.Deal.Brand)
	}
}

func TestDealInfersConservativeStorageModel(t *testing.T) {
	res := Deal(domain.Deal{Source: "test", Title: "Seagate Exos 7E8 8TB ST8000NM000A HDD", URL: "https://example.test/d", CapacityTB: 8, PriceEUR: 120})
	if res.Reject != nil || res.Deal.Model == nil || *res.Deal.Model != "ST8000NM000A" {
		t.Fatalf("expected part number model, got %#v (%v)", res.Deal.Model, res.Reject)
	}
	noModel := Deal(domain.Deal{Source: "test", Title: "Disque dur 8 To pas de reference", URL: "https://example.test/e", CapacityTB: 8, PriceEUR: 120})
	if noModel.Deal.Model != nil {
		t.Fatalf("generic title must remain ungrouped: %q", *noModel.Deal.Model)
	}
}
