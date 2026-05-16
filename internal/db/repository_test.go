package db

import (
	"context"
	"os"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	url := os.Getenv("DISKCOUNT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set DISKCOUNT_TEST_DATABASE_URL to run repository integration tests")
	}
	d, err := New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAppConfigImportDoesNotOverwrite(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	_, _ = d.Pool.Exec(ctx, `DELETE FROM app_config WHERE key IN ('TEST_IMPORT_KEY')`)

	n, err := d.ImportAppConfig(ctx, map[string]string{"TEST_IMPORT_KEY": "first"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected one inserted key, got %d", n)
	}
	n, err = d.ImportAppConfig(ctx, map[string]string{"TEST_IMPORT_KEY": "second"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected no overwrite, got %d inserts", n)
	}
	values, err := d.ListAppConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if values["TEST_IMPORT_KEY"] != "first" {
		t.Fatalf("import overwrote existing value: %q", values["TEST_IMPORT_KEY"])
	}
}

func TestAuthorizedUserManagementAndStats(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	const uid int64 = 42
	_, _ = d.Pool.Exec(ctx, `DELETE FROM authorized_users WHERE telegram_user_id=$1`, uid)

	if err := d.UpsertAuthorizedUser(ctx, uid, "Test User", true); err != nil {
		t.Fatal(err)
	}
	if err := d.SetAuthorizedUserEnabled(ctx, uid, false); err != nil {
		t.Fatal(err)
	}
	allowed, err := d.IsUserAllowed(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatalf("disabled user should not be allowed")
	}
	if _, err := d.Stats(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLatestPricesUsesLatestObservationAndSortsByPricePerTB(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	ids := []string{"test-current-a", "test-current-b"}
	_, _ = d.Pool.Exec(ctx, `DELETE FROM price_observations WHERE product_id = ANY($1)`, ids)
	_, _ = d.Pool.Exec(ctx, `DELETE FROM products WHERE id = ANY($1)`, ids)
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM price_observations WHERE product_id = ANY($1)`, ids)
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM products WHERE id = ANY($1)`, ids)
	})

	_, err := d.Pool.Exec(ctx, `INSERT INTO products(id,source,title,url,capacity_tb) VALUES
		('test-current-a','test','Drive A','https://example.test/a',10),
		('test-current-b','test','Drive B','https://example.test/b',20)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Pool.Exec(ctx, `INSERT INTO price_observations(product_id,source,observed_at,price_eur,price_per_tb,quality_score) VALUES
		('test-current-a','test',NOW() - INTERVAL '2 hours',300,30,90),
		('test-current-a','test',NOW() - INTERVAL '1 hour',180,18,90),
		('test-current-b','test',NOW(),500,25,90)`)
	if err != nil {
		t.Fatal(err)
	}

	prices, err := d.LatestPrices(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) < 2 {
		t.Fatalf("expected at least 2 prices, got %d", len(prices))
	}
	if prices[0].ProductID != "test-current-a" || prices[0].PricePerTB != 18 {
		t.Fatalf("expected latest cheapest observation first, got %#v", prices[0])
	}
}

func TestRejectedDealsAndQualityStats(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	_, _ = d.Pool.Exec(ctx, `DELETE FROM rejected_deals WHERE source='test-reject'`)
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM rejected_deals WHERE source='test-reject'`)
	})

	err := d.RecordRejectedDeal(ctx, domain.Deal{Source: "test-reject", Title: "", URL: ""}, "missing_title", "title is empty")
	if err != nil {
		t.Fatal(err)
	}
	qs, err := d.QualityStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range qs.Reasons {
		if r.Source == "test-reject" && r.Reason == "missing_title" && r.Count > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("quality stats did not include rejected deal: %#v", qs.Reasons)
	}
}
