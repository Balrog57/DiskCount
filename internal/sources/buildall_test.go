package sources

import (
	"testing"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/scraper"
)

// TestBuildAllWithNoConfig verifies that BuildAll returns 0 sources when no
// URLs or API keys are configured (the expected startup state before the user
// fills in the .env). This validates that every registered source correctly
// skips itself when its config is empty.
func TestBuildAllWithNoConfig(t *testing.T) {
	cfg := &config.Config{}
	reg := &Registry{
		cfg:    cfg,
		http:   scraper.NewHTTPFetcherWithOptions(scraper.Options{}),
		retry:  scraper.NewRetryingFetcher(scraper.NewHTTPFetcherWithOptions(scraper.Options{}), scraper.RetryConfig{}),
		byparr: scraper.NewByparrClient(""),
	}
	srcs := BuildAll(reg)
	if len(srcs) != 0 {
		var names []string
		for _, s := range srcs {
			names = append(names, s.Name())
		}
		t.Fatalf("expected 0 sources with empty config, got %d: %v", len(srcs), names)
	}
}

// TestBuildAllWithDiskPrices verifies that BuildAll returns at least the
// diskprices source when its URL is configured (the one source with a default
// URL in AppSettings).
func TestBuildAllWithDiskPrices(t *testing.T) {
	cfg := &config.Config{
		DiskPricesURL: "https://diskprices.com/?locale=fr",
	}
	reg := &Registry{
		cfg:    cfg,
		http:   scraper.NewHTTPFetcherWithOptions(scraper.Options{}),
		retry:  scraper.NewRetryingFetcher(scraper.NewHTTPFetcherWithOptions(scraper.Options{}), scraper.RetryConfig{}),
		byparr: scraper.NewByparrClient(""),
	}
	srcs := BuildAll(reg)
	if len(srcs) == 0 {
		t.Fatal("expected at least 1 source (diskprices) with default URL configured")
	}
	found := false
	for _, s := range srcs {
		if s.Name() == "diskprices" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("diskprices source not found in BuildAll output")
	}
}

// TestAllSourcesImplementName verifies that every registered factory produces
// a source with a non-empty Name(). This catches registration bugs where a
// factory returns nil or a source with an empty name.
func TestAllSourcesImplementName(t *testing.T) {
	if len(registeredFactories) == 0 {
		t.Fatal("no factories registered")
	}
	reg := &Registry{
		cfg: &config.Config{
			DiskPricesURL:        "https://diskprices.com/?locale=fr",
			PricePerGigEnabled:   true,
			PricePerGigAPIURL:    "https://api.pricepergig.com/drives",
			PricePerGigMarket:    "amazon.fr",
			PricePerTBURLs:       []string{"https://pricepertb.com/fr"},
			DealabsRSSURLs:       []string{"https://dealabs.com/rss"},
			IdealoFeedURLs:       []string{"https://idealo.fr/rss"},
			LeDenicheurFeedURLs:  []string{"https://ledenicheur.fr/rss"},
			LeBonCoinFeedURLs:    []string{"https://leboncoin.fr/rss"},
			MindfactoryURLs:      []string{"https://example.com/"},
			AlternateURLs:        []string{"https://example.com/"},
			ComputeruniverseURLs: []string{"https://example.com/"},
			ProshopURLs:          []string{"https://example.com/"},
			GeizhalsURLs:         []string{"https://example.com/"},
			LDLCURLs:             []string{"https://example.com/"},
			TopachatURLs:         []string{"https://example.com/"},
			GrosbillURLs:         []string{"https://example.com/"},
			FnacURLs:             []string{"https://example.com/"},
			BoulangerURLs:        []string{"https://example.com/"},
			CdiscountURLs:        []string{"https://example.com/"},
			RakutenURLs:          []string{"https://example.com/"},
			RueDuCommerceURLs:    []string{"https://example.com/"},
			BackmarketURLs:       []string{"https://example.com/"},
			KeepaAPIKey:          "test-key",
			KeepaASINs:           []string{"B08XYZ1234"},
			KeepaDomains:         []int{4},
			EbayClientID:         "test-id",
			EbayClientSecret:     "test-secret",
			EbaySearchQueries:    []string{"disque dur"},
			EbayMarketplaces:     []string{"EBAY_FR"},
		},
		http:   scraper.NewHTTPFetcherWithOptions(scraper.Options{}),
		retry:  scraper.NewRetryingFetcher(scraper.NewHTTPFetcherWithOptions(scraper.Options{}), scraper.RetryConfig{}),
		byparr: scraper.NewByparrClient(""),
	}
	srcs := BuildAll(reg)
	t.Logf("sources built with full config: %d", len(srcs))
	for _, s := range srcs {
		t.Logf("  - %s", s.Name())
	}
	if len(srcs) < 15 {
		t.Fatalf("expected at least 15 sources with full config, got %d", len(srcs))
	}
	for _, s := range srcs {
		if s.Name() == "" {
			t.Error("a source has an empty Name()")
		}
	}
}
