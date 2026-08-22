package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPricePerGigUsesFrenchAmazonAndPreservesAPIData(t *testing.T) {
	const exactURL = "https://www.amazon.fr/dp/B012345678?tag=pricepergig-21"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("marketplace") != "eq.amazon.fr" || r.URL.Query().Get("technology") != "in.(HDD,SSD)" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[
{"id":1,"name":"Seagate IronWolf 4 To HDD","url":"` + exactURL + `","price":120,"price_per_tb":30,"capacity_gb":4000,"technology":"HDD","marketplace":"amazon.fr","currency":"€","seller_name":"Amazon","last_updated":"2026-08-22T08:20:08Z"},
{"id":2,"name":"Wrong market 2 To SSD","url":"https://www.amazon.de/dp/X","price":100,"capacity_gb":2000,"technology":"SSD","marketplace":"amazon.de","currency":"€"},
{"id":3,"name":"Wrong merchant 2 To SSD","url":"https://example.com/x","price":100,"capacity_gb":2000,"technology":"SSD","marketplace":"amazon.fr","currency":"€"}
]`))
	}))
	defer srv.Close()

	source := &PricePerGig{http: NewTestFetcher(t, srv), apiURL: srv.URL, market: "amazon.fr"}
	deals, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(deals) != 1 || deals[0].URL != exactURL {
		t.Fatalf("unexpected deals: %#v", deals)
	}
	if deals[0].Merchant == nil || *deals[0].Merchant != "Amazon" {
		t.Fatalf("seller not preserved: %#v", deals[0].Merchant)
	}
	want := time.Date(2026, 8, 22, 8, 20, 8, 0, time.UTC)
	if !deals[0].ObservedAt.Equal(want) {
		t.Fatalf("last_updated not used: %s", deals[0].ObservedAt)
	}
}
