package sources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestParseBackmarketGoldenFile(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("testdata", "backmarket_sample.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	deals := parseBackmarket(string(html), "https://www.backmarket.fr/")
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d: %+v", len(deals), deals)
	}
	if deals[0].PriceEUR != 179.99 || deals[0].CapacityTB != 8 {
		t.Errorf("deal 0: price=%.2f capacity=%.2f", deals[0].PriceEUR, deals[0].CapacityTB)
	}
	if deals[0].Condition == nil || *deals[0].Condition != domain.ConditionUsed {
		t.Error("deal 0: expected ConditionUsed")
	}
	if deals[1].PriceEUR != 54.90 || deals[1].CapacityTB != 1 {
		t.Errorf("deal 1: price=%.2f capacity=%.2f", deals[1].PriceEUR, deals[1].CapacityTB)
	}
	if deals[1].Condition == nil || *deals[1].Condition != domain.ConditionUsed {
		t.Error("deal 1: expected ConditionUsed")
	}
}
