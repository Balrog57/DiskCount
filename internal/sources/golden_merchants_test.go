package sources

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMindfactoryGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "mindfactory_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseMindfactory(string(html), "https://www.mindfactory.de/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	// Seagate Exos X20 18TB
	if deals[0].PriceEUR != 289.99 || deals[0].CapacityTB != 18 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[0].Source != "mindfactory" {
		t.Errorf("deal 0: source=%s", deals[0].Source)
	}
	// WD Red Plus 8TB SSD
	if deals[1].PriceEUR != 149.50 || deals[1].CapacityTB != 8 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseAlternateGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "alternate_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseAlternate(string(html), "https://www.alternate.de/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	if deals[0].PriceEUR != 129.90 || deals[0].CapacityTB != 2 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[0].URL != "https://www.alternate.de/Seagate/ST2000DM008-2-TB-Festplatte/html/product/1431718" {
		t.Errorf("deal 0 URL: got %q", deals[0].URL)
	}
	if deals[1].PriceEUR != 289.00 || deals[1].CapacityTB != 4 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseComputeruniverseGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "computeruniverse_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseComputeruniverse(string(html), "https://www.computeruniverse.de/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	if deals[0].PriceEUR != 249.99 || deals[0].CapacityTB != 12 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[1].PriceEUR != 54.90 || deals[1].CapacityTB != 1 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseProshopGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "proshop_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseProshop(string(html), "https://www.proshop.de/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	if deals[0].PriceEUR != 44.99 || deals[0].CapacityTB != 1 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[1].PriceEUR != 259.00 || deals[1].CapacityTB != 16 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
}

func TestParseGeizhalsGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "geizhals_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseGeizhals(string(html), "https://geizhals.de/")
	if len(deals) != 0 {
		t.Fatalf("listing-only HTML should not emit deals without merchant offers, got %d: %+v", len(deals), deals)
	}
}

func TestParseGeizhalsOffersGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "geizhals_offers_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseGeizhals(string(html), "https://geizhals.de/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 merchant offers, got %d: %+v", len(deals), deals)
	}
	if deals[0].URL != "https://www.mindfactory.de/product/123456.html" {
		t.Errorf("offer 0 URL: %q", deals[0].URL)
	}
	if deals[0].PriceEUR != 241.90 {
		t.Errorf("offer 0 price: %.2f", deals[0].PriceEUR)
	}
	if deals[1].URL != "https://www.amazon.de/dp/B0TESTDE12" {
		t.Errorf("offer 1 URL: %q", deals[1].URL)
	}
}
