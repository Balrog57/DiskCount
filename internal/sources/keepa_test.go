package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKeepaFetchParsesProduct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("unexpected key: %s", r.URL.Query().Get("key"))
		}
		if r.URL.Query().Get("domain") != "4" {
			t.Errorf("unexpected domain: %s", r.URL.Query().Get("domain"))
		}
		w.Header().Set("Content-Type", "application/json")
		// Keepa prices are in cents. current[0] = Amazon price.
		json.NewEncoder(w).Encode(map[string]any{
			"products": []map[string]any{
				{
					"asin":  "B08XYZ1234",
					"brand": "Seagate",
					"title": "Seagate Exos X20 18TB Enterprise HDD",
					"stats": map[string]any{
						"current": []float64{28999, -1, -1},
					},
				},
			},
		})
	}))
	defer srv.Close()

	fetcher := newTestHTTPFetcher(t)
	s := &Keepa{
		http: fetcher, apiKey: "test-key", asins: []string{"B08XYZ1234"},
		domains: []int{4}, apiBase: srv.URL,
	}

	deals, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(deals) != 1 {
		t.Fatalf("expected 1 deal, got %d", len(deals))
	}
	d := deals[0]
	if d.PriceEUR != 289.99 {
		t.Fatalf("price: got %.2f (28999 cents should be 289.99)", d.PriceEUR)
	}
	if d.CapacityTB != 18 {
		t.Fatalf("capacity: got %.2f", d.CapacityTB)
	}
	if d.ExternalID == nil || *d.ExternalID != "B08XYZ1234" {
		t.Fatalf("external ID: %v", d.ExternalID)
	}
	if d.URL != "https://www.amazon.fr/dp/B08XYZ1234" {
		t.Fatalf("URL: got %q", d.URL)
	}
}

func TestKeepaPriceFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Amazon price is -1 (not tracked), New is available.
		json.NewEncoder(w).Encode(map[string]any{
			"products": []map[string]any{
				{
					"asin":  "B00ABC",
					"brand": "WD",
					"title": "WD Red 8TB NAS Hard Drive",
					"stats": map[string]any{
						"current": []float64{-1, 14999, 12000},
					},
				},
			},
		})
	}))
	defer srv.Close()

	fetcher := newTestHTTPFetcher(t)
	s := &Keepa{http: fetcher, apiKey: "k", asins: []string{"B00ABC"}, domains: []int{4}, apiBase: srv.URL}
	deals, _ := s.Fetch(context.Background())
	if len(deals) != 1 {
		t.Fatalf("expected 1 deal, got %d", len(deals))
	}
	// Should fall back to New marketplace price (14999 cents = 149.99).
	if deals[0].PriceEUR != 149.99 {
		t.Fatalf("price: got %.2f, want 149.99 (New fallback)", deals[0].PriceEUR)
	}
}

func TestKeepaNoPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"products": []map[string]any{
				{
					"asin":  "X",
					"title": "SSD Samsung 2TB",
					"stats": map[string]any{
						"current": []float64{-1, -1, -1},
					},
				},
			},
		})
	}))
	defer srv.Close()

	fetcher := newTestHTTPFetcher(t)
	s := &Keepa{http: fetcher, apiKey: "k", asins: []string{"X"}, domains: []int{4}, apiBase: srv.URL}
	deals, _ := s.Fetch(context.Background())
	if len(deals) != 0 {
		t.Fatalf("expected 0 deals when all prices are -1, got %d", len(deals))
	}
}

func TestKeepaDomainTLD(t *testing.T) {
	cases := []struct{ domain int; want string }{
		{1, "com"}, {2, "co.uk"}, {3, "de"}, {4, "fr"},
		{5, "co.jp"}, {6, "it"}, {7, "es"},
		{99, "fr"}, // unknown defaults to fr
	}
	for _, c := range cases {
		if got := keepaDomainTLD(c.domain); got != c.want {
			t.Errorf("keepaDomainTLD(%d) = %s, want %s", c.domain, got, c.want)
		}
	}
}

func TestKeepaPriceExtraction(t *testing.T) {
	cases := []struct {
		name    string
		current []float64
		want    float64
	}{
		{"amazon only", []float64{28999}, 28999},
		{"amazon preferred", []float64{28999, 25000, 20000}, 28999},
		{"new fallback", []float64{-1, 25000, 20000}, 25000},
		{"used fallback", []float64{-1, -1, 20000}, 20000},
		{"all unavailable", []float64{-1, -1, -1}, 0},
		{"empty", []float64{}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := keepaPrice(c.current); got != c.want {
				t.Errorf("keepaPrice(%v) = %.0f, want %.0f", c.current, got, c.want)
			}
		})
	}
}

// TestKeepaMultiDomain verifies that Fetch queries every configured domain for
// each ASIN and produces one deal per (ASIN × domain), with distinct Amazon
// URLs. The plan (§Phase 2) explicitly requires this dedicated test: the
// generic TestKeepaFetchParsesProduct only exercises a single domain.
func TestKeepaMultiDomain(t *testing.T) {
	var requestCount int
	seenDomains := make(map[string]bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dom := r.URL.Query().Get("domain")
		seenDomains[dom] = true
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		// Title contains the domain so we can tell the deals apart.
		titleByDomain := map[string]string{"3": "Seagate Exos 18TB DE", "4": "Seagate Exos 18TB FR"}
		title := titleByDomain[dom]
		if title == "" {
			t.Errorf("unexpected domain: %s", dom)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"products": []map[string]any{
				{
					"asin":  "B08XYZ1234",
					"brand": "Seagate",
					"title": title,
					"stats": map[string]any{
						"current": []float64{28999, -1, -1},
					},
				},
			},
		})
	}))
	defer srv.Close()

	fetcher := newTestHTTPFetcher(t)
	s := &Keepa{
		http: fetcher, apiKey: "test-key", asins: []string{"B08XYZ1234"},
		domains: []int{3, 4}, // DE + FR
		apiBase: srv.URL,
	}

	deals, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(deals) != 2 {
		t.Fatalf("expected 2 deals (1 ASIN × 2 domains), got %d: %+v", len(deals), deals)
	}
	if requestCount != 2 {
		t.Fatalf("expected 2 API calls (one per domain), got %d", requestCount)
	}
	if !seenDomains["3"] || !seenDomains["4"] {
		t.Fatalf("expected both domains 3 and 4 queried, seen=%v", seenDomains)
	}

	// Collect URLs to verify they target different Amazon TLDs.
	urls := map[string]bool{}
	externalIDs := map[string]bool{}
	for _, d := range deals {
		urls[d.URL] = true
		if d.ExternalID != nil {
			externalIDs[*d.ExternalID] = true
		}
		if d.CapacityTB != 18 {
			t.Errorf("capacity: got %.2f, want 18", d.CapacityTB)
		}
		if d.PriceEUR != 289.99 {
			t.Errorf("price: got %.2f, want 289.99", d.PriceEUR)
		}
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 distinct URLs, got %d: %v", len(urls), urls)
	}
	if !urls["https://www.amazon.de/dp/B08XYZ1234"] || !urls["https://www.amazon.fr/dp/B08XYZ1234"] {
		t.Fatalf("expected amazon.de + amazon.fr URLs, got %v", urls)
	}
	// ExternalID is the same ASIN across domains — ProductID dedup is by source
	// ("keepa"), so both deals share the same ProductID. That is the documented
	// behaviour: the last observation wins. This test only asserts that the two
	// domains are queried, not that they dedup.
	if len(externalIDs) != 1 || !externalIDs["B08XYZ1234"] {
		t.Fatalf("expected ExternalID B08XYZ1234 on both deals, got %v", externalIDs)
	}
}
