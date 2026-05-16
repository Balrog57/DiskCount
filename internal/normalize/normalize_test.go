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
