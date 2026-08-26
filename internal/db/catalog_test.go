package db

import (
	"context"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestCatalogEntriesMapBatchLookup(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	key := "mpn:testcatalogbatch"
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM product_catalog WHERE canonical_key=$1`, key)
	})
	brand, model, sku := "Seagate", "IronWolf", "ST8000VN004"
	deal := domain.Deal{
		Source: "test-shop", ExternalID: strPtr("batch"), Title: "IronWolf 8 To",
		URL: "https://example.test/ironwolf", CapacityTB: 8, PriceEUR: 200, PricePerTB: 25,
		Brand: &brand, Model: &model, SKU: &sku, QualityScore: 90,
	}
	if err := d.UpsertCatalogEntry(ctx, deal); err != nil {
		t.Fatal(err)
	}
	got, err := d.CatalogEntriesMap(ctx, []string{key, "missing:key"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one entry, got %#v", got)
	}
	cat := got[key]
	if cat == nil || cat.Brand == nil || *cat.Brand != brand || cat.SKU == nil || *cat.SKU != sku {
		t.Fatalf("unexpected catalog row: %#v", cat)
	}
}
