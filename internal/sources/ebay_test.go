package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/scraper"
)

func newTestHTTPFetcher(t *testing.T) *scraper.HTTPFetcher {
	t.Helper()
	return scraper.NewHTTPFetcherWithOptions(scraper.Options{
		UserAgent:             "DiskCountTest/1.0",
		PerRequestTimeout:     2 * time.Second,
		DisableBrowserHeaders: true,
	})
}

func TestEbayFetchParsesItems(t *testing.T) {
	var tokenReqPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity/v1/oauth2/token" {
			tokenReqPath = r.URL.Path
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "client_credentials" {
				t.Errorf("unexpected grant_type: %s", r.Form.Get("grant_type"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "test-token-123",
				"expires_in":   7200,
				"token_type":   "Bearer",
			})
			return
		}
		// Search endpoint
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-123" {
			t.Errorf("expected Bearer token, got %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"itemSummaries": []map[string]any{
				{
					"itemId":     "v1|123456789|0",
					"title":      "Seagate Exos X20 18TB HDD SATA",
					"itemWebUrl": "https://www.ebay.fr/itm/123456789",
					"price":      map[string]any{"value": "289.99", "currency": "EUR"},
					"condition":  map[string]any{"conditionId": "1000", "condition": "New"},
					"brand":      "Seagate",
				},
				{
					"itemId":     "v1|987654321|0",
					"title":      "WD Red Plus 8TB NAS",
					"itemWebUrl": "https://www.ebay.fr/itm/987654321",
					"price":      map[string]any{"value": "149.50", "currency": "EUR"},
					"condition":  map[string]any{"conditionId": "3000", "condition": "Used"},
					"brand":      "Western Digital",
				},
				// This item has no parseable capacity and should be skipped.
				{
					"itemId":     "v1|00000000|0",
					"title":      "Random accessory without capacity",
					"itemWebUrl": "https://www.ebay.fr/itm/00000000",
					"price":      map[string]any{"value": "10.00", "currency": "EUR"},
				},
			},
		})
	}))
	defer srv.Close()

	fetcher := newTestHTTPFetcher(t)
	s := &Ebay{
		http:      fetcher,
		clientID:  "test-client",
		secret:    "test-secret",
		queries:   []string{"disque dur"},
		apiBase:   srv.URL,
		oauthBase: srv.URL + "/identity/v1/oauth2/token",
	}

	deals, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if tokenReqPath == "" {
		t.Fatal("OAuth token endpoint was not called")
	}
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals, got %d", len(deals))
	}

	// First deal: Seagate Exos 18TB
	d0 := deals[0]
	if d0.Title != "Seagate Exos X20 18TB HDD SATA" {
		t.Fatalf("title: got %q", d0.Title)
	}
	if d0.PriceEUR != 289.99 {
		t.Fatalf("price: got %.2f", d0.PriceEUR)
	}
	if d0.CapacityTB != 18 {
		t.Fatalf("capacity: got %.2f", d0.CapacityTB)
	}
	if d0.Condition == nil || *d0.Condition != domain.ConditionNew {
		t.Fatalf("condition: expected new")
	}
	if d0.ExternalID == nil || *d0.ExternalID != "v1|123456789|0" {
		t.Fatalf("external ID: got %v", d0.ExternalID)
	}

	// Second deal: WD Red 8TB used
	d1 := deals[1]
	if d1.PriceEUR != 149.5 {
		t.Fatalf("price: got %.2f", d1.PriceEUR)
	}
	if d1.Condition == nil || *d1.Condition != domain.ConditionUsed {
		t.Fatalf("condition: expected used")
	}
}

func TestEbayTokenCaching(t *testing.T) {
	tokenCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity/v1/oauth2/token" {
			tokenCount++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "cached-token",
				"expires_in":   7200,
				"token_type":   "Bearer",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"itemSummaries":[]}`))
	}))
	defer srv.Close()

	fetcher := newTestHTTPFetcher(t)
	s := &Ebay{
		http:      fetcher,
		clientID:  "c", secret: "s", queries: []string{"ssd", "hdd"},
		apiBase:   srv.URL,
		oauthBase: srv.URL + "/identity/v1/oauth2/token",
	}

	// Two queries should only request the token once (cached).
	_, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if tokenCount != 1 {
		t.Fatalf("expected token requested once (cached), got %d", tokenCount)
	}
}

func TestEbayConditionMapping(t *testing.T) {
	cases := []struct {
		condID   string
		display  string
		want     domain.Condition
	}{
		{"1000", "New", domain.ConditionNew},
		{"1000", "Neuf", domain.ConditionNew},
		{"2000", "Manufacturer Refurbished", domain.ConditionUsed},
		{"3000", "Used", domain.ConditionUsed},
		{"", "Seller Refurbished", domain.ConditionUsed},
	}
	for _, c := range cases {
		got := conditionFromEbay(c.condID, c.display)
		if got != c.want {
			t.Errorf("conditionFromEbay(%q, %q) = %s, want %s", c.condID, c.display, got, c.want)
		}
	}
}
