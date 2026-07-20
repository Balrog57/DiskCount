package sources

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLDLCGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "ldlc_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseLDLC(string(html), "https://www.ldlc.com/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	// Seagate IronWolf 4 To at 219€95 (LDLC price format: € separates euros/centimes)
	if deals[0].PriceEUR != 219.95 || deals[0].CapacityTB != 4 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[0].URL != "https://www.ldlc.com/fiche/PB00494230.html" {
		t.Errorf("deal 0 URL: got %q", deals[0].URL)
	}
	// Seagate IronWolf 8 To at 359€95
	if deals[1].PriceEUR != 359.95 || deals[1].CapacityTB != 8 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseLDLCPriceFormat(t *testing.T) {
	cases := []struct{ in string; want float64 }{
		{"219€95", 219.95},
		{"359€95", 359.95},
		{"1 199€95", 1199.95},
		{"59€99", 59.99},
		{"1199€95", 1199.95},
		{"129€90", 129.90},
		{"€129,90", 129.90},  // fallback to parseFloatClean
		{"129.90", 129.90},   // fallback to parseFloatClean
	}
	for _, c := range cases {
		got, err := parseLDLCPrice(c.in)
		if err != nil {
			t.Errorf("parseLDLCPrice(%q): err=%v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseLDLCPrice(%q) = %.2f, want %.2f", c.in, got, c.want)
		}
	}
}

func TestParseTopachatGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "topachat_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseTopachat(string(html), "https://www.topachat.com/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	if deals[0].PriceEUR != 269.99 || deals[0].CapacityTB != 1 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[0].URL != "https://www.topachat.com/pages/detail2_cat_est_micro_puis_rubrique_est_w_ssd_puis_ref_est_in20030499.html" {
		t.Errorf("deal 0 URL: got %q", deals[0].URL)
	}
	if deals[1].PriceEUR != 189.99 || deals[1].CapacityTB != 2 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseGrosbillGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "grosbill_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseGrosbill(string(html), "https://www.grosbill.com/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	// Crucial P310 1To SSD at 139,99€ (content_prix_produit: "139,99")
	if deals[0].PriceEUR != 139.99 || deals[0].CapacityTB != 1 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[0].URL != "https://www.grosbill.com/disque-ssd/crucial-p310-1to-nvme-m-2-gen4-147622.aspx" {
		t.Errorf("deal 0 URL: got %q", deals[0].URL)
	}
	// Samsung 870 EVO 4To SSD at 329,00€
	if deals[1].PriceEUR != 329.00 || deals[1].CapacityTB != 4 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseFnacGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "fnac_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseFnac(string(html), "https://www.fnac.com/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	// Samsung 870 EVO 2To SSD at 139,90€
	if deals[0].PriceEUR != 139.90 || deals[0].CapacityTB != 2 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[0].URL != "https://www.fnac.com/p/a123456/disque-ssd-samsung-870-evo-2to" {
		t.Errorf("deal 0 URL: got %q", deals[0].URL)
	}
	// Seagate IronWolf 8To HDD at 299,99€
	if deals[1].PriceEUR != 299.99 || deals[1].CapacityTB != 8 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseBoulangerGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "boulanger_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseBoulanger(string(html), "https://www.boulanger.com/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	// SANDISK Extreme 4To SSD at 329.99 (from data-analytics_product_unitprice_ati)
	if deals[0].PriceEUR != 329.99 || deals[0].CapacityTB != 4 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[0].URL != "https://www.boulanger.com/ref/9000438236" {
		t.Errorf("deal 0 URL: got %q", deals[0].URL)
	}
	// SEAGATE IronWolf 8To HDD at 359.95
	if deals[1].PriceEUR != 359.95 || deals[1].CapacityTB != 8 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseCdiscountGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "cdiscount_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseCdiscount(string(html), "https://www.cdiscount.com/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d", len(deals))
	}
	if deals[0].PriceEUR != 84.99 || deals[0].CapacityTB != 4 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[1].PriceEUR != 134.90 || deals[1].CapacityTB != 2 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseRakutenGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "rakuten_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseRakuten(string(html), "https://www.rakuten.com/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d", len(deals))
	}
	if deals[0].PriceEUR != 319.00 || deals[0].CapacityTB != 14 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[1].PriceEUR != 49.99 || deals[1].CapacityTB != 1 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseRueDuCommerceGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "rueducommerce_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseRueDuCommerce(string(html), "https://www.rueducommerce.fr/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	// Seagate IronWolf 4TB at 198,15€
	if deals[0].PriceEUR != 198.15 || deals[0].CapacityTB != 4 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[0].URL != "https://www.rueducommerce.fr/p/r24060016844.html" {
		t.Errorf("deal 0 URL: got %q", deals[0].URL)
	}
	if deals[0].ExternalID == nil || *deals[0].ExternalID != "AR202204010054" {
		t.Errorf("deal 0 ExternalID: got %v", deals[0].ExternalID)
	}
	// Samsung 870 EVO 2TB SSD at 149,99€
	if deals[1].PriceEUR != 149.99 || deals[1].CapacityTB != 2 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}
