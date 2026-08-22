package db

import (
	"context"
	"testing"
)

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
