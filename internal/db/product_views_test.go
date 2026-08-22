package db

import (
	"context"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestProductAvailabilityRequiresSuccessfulMissesAndRestores(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	const id = "availability-shop:test-availability"
	_, _ = d.Pool.Exec(ctx, `DELETE FROM price_observations WHERE product_id=$1`, id)
	_, _ = d.Pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, id)
	t.Cleanup(func() { _, _ = d.Pool.Exec(context.Background(), `DELETE FROM products WHERE id=$1`, id) })
	_, err := d.Pool.Exec(ctx, `INSERT INTO products(id,source,title,url,capacity_tb) VALUES($1,'availability-shop','Drive','https://example.test/availability',1)`, id)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := d.MarkSourceMissing(ctx, "availability-shop", nil, 3); err != nil {
			t.Fatal(err)
		}
		p, err := d.GetProduct(ctx, id)
		if err != nil || p.Availability != domain.AvailabilityAvailable {
			t.Fatalf("miss %d changed availability: %#v, %v", i+1, p, err)
		}
	}
	if err := d.MarkSourceMissing(ctx, "availability-shop", nil, 3); err != nil {
		t.Fatal(err)
	}
	p, err := d.GetProduct(ctx, id)
	if err != nil || p.Availability != domain.AvailabilityUnavailable {
		t.Fatalf("expected unavailable after threshold: %#v, %v", p, err)
	}
	externalID := id
	deal := domain.Deal{Source: "availability-shop", ExternalID: &externalID, Title: "Drive", URL: "https://example.test/availability", CapacityTB: 1, PriceEUR: 10, PricePerTB: 10}
	if err := d.UpsertProduct(ctx, deal); err != nil {
		t.Fatal(err)
	}
	p, err = d.GetProduct(ctx, deal.ProductID())
	if err != nil || p.Availability != domain.AvailabilityAvailable || p.AvailabilityMissCount != 0 {
		t.Fatalf("expected restock reset: %#v, %v", p, err)
	}
}

func TestProductOffersGroupsOnlyCanonicalIdentity(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	ids := []string{"test-group-a", "test-group-b"}
	_, _ = d.Pool.Exec(ctx, `DELETE FROM price_observations WHERE product_id=ANY($1)`, ids)
	_, _ = d.Pool.Exec(ctx, `DELETE FROM products WHERE id=ANY($1)`, ids)
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM price_observations WHERE product_id=ANY($1)`, ids)
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM products WHERE id=ANY($1)`, ids)
	})
	_, err := d.Pool.Exec(ctx, `INSERT INTO products(id,source,title,url,capacity_tb,condition,quality_score,brand,model,canonical_key) VALUES
		('test-group-a','shop-a','Drive A','https://example.test/a',16,'new',90,'Seagate','ST16000NM000J','seagate|st16000nm000j|16.000'),
		('test-group-b','shop-b','Drive B','https://example.test/b',16,'used',90,'Seagate','ST16000NM000J','seagate|st16000nm000j|16.000')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Pool.Exec(ctx, `INSERT INTO price_observations(product_id,source,price_eur,price_per_tb,quality_score) VALUES
		('test-group-a','shop-a',320,20,90),('test-group-b','shop-b',288,18,90)`)
	if err != nil {
		t.Fatal(err)
	}
	offers, err := d.ProductOffers(ctx, "seagate|st16000nm000j|16.000")
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 2 || offers[0].Source != "shop-b" || offers[0].Condition == nil || *offers[0].Condition != "used" {
		t.Fatalf("unexpected grouped offers: %#v", offers)
	}
}

func TestPriceDropsReturnsOnlyCurrentDecrease(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	const id = "test-current-drop"
	_, _ = d.Pool.Exec(ctx, `DELETE FROM price_observations WHERE product_id=$1`, id)
	_, _ = d.Pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, id)
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM price_observations WHERE product_id=$1`, id)
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM products WHERE id=$1`, id)
	})
	_, err := d.Pool.Exec(ctx, `INSERT INTO products(id,source,title,url,capacity_tb,quality_score) VALUES($1,'test','Drive drop','https://example.test/drop',10,90)`, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Pool.Exec(ctx, `INSERT INTO price_observations(product_id,source,observed_at,price_eur,price_per_tb,quality_score) VALUES
		($1,'test',NOW()-INTERVAL '2 hours',300,30,90),($1,'test',NOW()-INTERVAL '1 hour',200,20,90)`, id)
	if err != nil {
		t.Fatal(err)
	}
	drops, err := d.PriceDrops(ctx, 7, 30, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, drop := range drops {
		if drop.ProductID == id {
			found = drop.PricePerTB == 20 && drop.PreviousPricePerTB == 30 && drop.DropPct > 33
		}
	}
	if !found {
		t.Fatalf("current price decrease not returned: %#v", drops)
	}
}

func TestMarketIndexComputesDailyCapacityMedian(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	const id = "test-market-index"
	_, _ = d.Pool.Exec(ctx, `DELETE FROM price_observations WHERE product_id=$1`, id)
	_, _ = d.Pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, id)
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM price_observations WHERE product_id=$1`, id)
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM products WHERE id=$1`, id)
	})
	_, err := d.Pool.Exec(ctx, `INSERT INTO products(id,source,title,url,capacity_tb,quality_score) VALUES($1,'test','Market drive','https://example.test/market',10,90)`, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Pool.Exec(ctx, `INSERT INTO price_observations(product_id,source,price_eur,price_per_tb,quality_score) VALUES($1,'test',100,10,90),($1,'test',200,20,90)`, id)
	if err != nil {
		t.Fatal(err)
	}
	points, err := d.MarketIndex(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, point := range points {
		if point.Band == "8–12 To" && point.MedianEUR == 20 && point.Samples == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("market median missing: %#v", points)
	}
}
