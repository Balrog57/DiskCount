package parsing

import (
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestParseCapacityTBHandlesFrenchAndSmallUnits(t *testing.T) {
	cases := map[string]float64{
		"Disque 16 To": 16,
		"SSD 500 Go":   0.5,
		"NVMe 1,92 TB": 1.92,
		"cache 512 Mo": 0.000512,
	}
	for input, want := range cases {
		got, err := ParseCapacityTB(input)
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if got != want {
			t.Fatalf("%q: got %v want %v", input, got, want)
		}
	}
}

func TestParsePriceEURIgnoresMonthlyAndShippingText(t *testing.T) {
	if got, _ := ParsePriceEUR("12,99 €/mois"); got != 0 {
		t.Fatalf("monthly price should be ignored, got %v", got)
	}
	if got, _ := ParsePriceEUR("199,99 €"); got != 199.99 {
		t.Fatalf("price mismatch: %v", got)
	}
}

func TestNormalizeDriveCategoryAndInterfaces(t *testing.T) {
	ssd := domain.MediaTypeSolidState
	if got := NormalizeDriveCategory("SSD M.2 NVMe PCIe 4.0", &ssd); got == nil || *got != domain.DriveCategoryM2NVMe {
		t.Fatalf("expected m2 nvme, got %#v", got)
	}
	hdd := domain.MediaTypeRotational
	if got := NormalizeDriveCategory("Disque dur externe portable 2,5 pouces USB", &hdd); got == nil || *got != domain.DriveCategoryExternal2_5 {
		t.Fatalf("expected external 2.5, got %#v", got)
	}
	ifaces := NormalizeInterfaces("SATA SAS NVMe USB")
	if len(ifaces) != 4 {
		t.Fatalf("expected all interfaces, got %#v", ifaces)
	}
	if media := NormalizeMediaType("Micron 5400 Pro 3D TLC NAND 960GB"); media == nil || *media != domain.MediaTypeSolidState {
		t.Fatalf("expected NAND/TLC SSD media, got %#v", media)
	}
}
