package db

import (
	"context"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestCatalogMapReturnsEntriesForRequestedKeys(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	const key = "mpn:st16000nm000j"
	_, _ = d.Pool.Exec(ctx, `DELETE FROM product_catalog WHERE canonical_key=$1`, key)
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM product_catalog WHERE canonical_key=$1`, key)
	})

	_, err := d.Pool.Exec(ctx, `
INSERT INTO product_catalog(
  canonical_key, brand, model, capacity_tb, media_type, spec_source, interfaces
) VALUES ($1, 'Seagate', 'ST16000NM000J', 16, 'rotational', 'heuristic', '[]')`, key)
	if err != nil {
		t.Fatal(err)
	}

	got, err := d.CatalogMap(ctx, []string{key, "missing:key"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one catalog entry, got %d", len(got))
	}
	entry, ok := got[key]
	if !ok || entry.Brand == nil || *entry.Brand != "Seagate" {
		t.Fatalf("unexpected entry: %#v", entry)
	}
}

func TestCatalogMapEmptyKeys(t *testing.T) {
	d := &DB{}
	got, err := d.CatalogMap(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %d", len(got))
	}
}

func TestApplyCatalogToDealFillsMissingSpecs(t *testing.T) {
	brand := "Seagate"
	deal := domain.Deal{}
	ApplyCatalogToDeal(&ProductCatalog{Brand: &brand, CapacityTB: 16}, &deal)
	if deal.Brand == nil || *deal.Brand != brand {
		t.Fatalf("brand not applied: %#v", deal.Brand)
	}
	if deal.CapacityTB != 16 {
		t.Fatalf("capacity not applied: %v", deal.CapacityTB)
	}
}
