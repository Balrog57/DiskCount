package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
)

func init() {
	Register(func(r *Registry) Source {
		cfg := r.Config()
		if cfg.KeepaAPIKey == "" || len(cfg.KeepaASINs) == 0 {
			return nil
		}
		return &Keepa{
			http:    r.HTTP(),
			apiKey:  cfg.KeepaAPIKey,
			asins:   cfg.KeepaASINs,
			domains: cfg.KeepaDomains,
			apiBase: "https://api.keepa.com",
		}
	})
}

// Keepa implements the Keepa product API source. It queries configured ASINs
// on one or more Amazon domains and extracts the current price from the price
// history stats. Keepa stores prices in cents and timestamps as Keepa minutes
// (Unix minutes), so conversions are needed.
type Keepa struct {
	http    scraper.Fetcher
	apiKey  string
	asins   []string
	domains []int
	apiBase string
}

func (s *Keepa) Name() string { return "keepa" }

func (s *Keepa) Info() SourceInfo {
	return SourceInfo{
		Name:        "keepa",
		Description: "API Keepa (historique des prix Amazon multi-domaines)",
		Categories:  []string{"api"},
		Requires:    []string{"KEEPA_API_KEY", "KEEPA_ASINS", "KEEPA_DOMAINS"},
		Version:     "2",
	}
}

func (s *Keepa) Fetch(ctx context.Context) ([]domain.Deal, error) {
	var deals []domain.Deal
	// Keepa rate-limits by token cost (~1 token per request, 100/min pool).
	// We process ASINs one at a time, querying all configured domains for
	// each ASIN before waiting, so the per-ASIN delay stays at ~800ms.
	for i := 0; i < len(s.asins); i += 1 {
		asin := strings.TrimSpace(s.asins[i])
		if asin == "" {
			continue
		}
		for _, domain := range s.domains {
			deal, err := s.fetchProduct(ctx, asin, domain)
			if err != nil {
				slog.Warn("keepa product", "asin", asin, "domain", domain, "err", err)
				continue
			}
			if deal != nil {
				deals = append(deals, *deal)
			}
		}
		// Respect Keepa rate limit: ~1 request per 800ms.
		select {
		case <-ctx.Done():
			return deals, ctx.Err()
		case <-time.After(800 * time.Millisecond):
		}
	}
	slog.Debug("keepa", "deals", len(deals))
	return deals, nil
}

func (s *Keepa) fetchProduct(ctx context.Context, asin string, kd int) (*domain.Deal, error) {
	u := fmt.Sprintf("%s/product?key=%s&domain=%d&asin=%s&stats=180&offers=20",
		s.apiBase, s.apiKey, kd, asin)
	resp, err := s.http.Get(ctx, u)
	if err != nil {
		return nil, err
	}

	var result struct {
		Products []struct {
			ASIN       string `json:"asin"`
			Brand      string `json:"brand"`
			Title      string `json:"title"`
			Stats      struct {
				Current []float64 `json:"current"`
			} `json:"stats"`
			CategoryID int `json:"categoryTypeId"`
		} `json:"products"`
	}
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if len(result.Products) == 0 {
		return nil, nil
	}
	p := result.Products[0]
	if p.Title == "" {
		return nil, nil
	}

	// Keepa's current[] array has price values indexed by CSV type.
	// Index 0 = Amazon, Index 1 = New (3rd party), Index 2 = Used.
	// Prices are in cents. We prefer Amazon (0), then New (1), then Used (2).
	priceCents := keepaPrice(result.Products[0].Stats.Current)
	if priceCents <= 0 {
		return nil, nil
	}
	price := priceCents / 100.0

	tb, err := parsing.ParseCapacityTB(p.Title)
	if err != nil || tb <= 0 {
		return nil, nil
	}
	pt := price / tb
	cond := domain.ConditionNew
	media := normalMedia(p.Title + " " + p.Brand)
	classText := p.Title + " " + p.Brand
	dc := parsing.NormalizeDriveCategory(classText, media)
	ifaces := parsing.NormalizeInterfaces(classText)

	// Build the Amazon product URL from the domain and ASIN.
	amazonTLD := keepaDomainTLD(kd)
	productURL := fmt.Sprintf("https://www.amazon.%s/dp/%s", amazonTLD, asin)

	return &domain.Deal{
		Source: "keepa", Title: p.Title, URL: productURL,
		PriceEUR: round2(price), PricePerTB: round2(pt), CapacityTB: round2(tb),
		Condition: &cond, MediaType: media,
		ExternalID:    &asin,
		DriveCategory: dc, Interfaces: ifaces,
		Brand: strPtr(p.Brand),
		ObservedAt:     domain.UTCNow(),
	}, nil
}

// keepaPrice extracts the best available current price from Keepa's current[]
// array, preferring Amazon, then New marketplace, then Used.
func keepaPrice(current []float64) float64 {
	if len(current) == 0 {
		return 0
	}
	// current[0] = Amazon price, current[1] = New, current[2] = Used.
	// A value of -1 or -2 means "not tracked" or "out of stock".
	if len(current) > 0 && current[0] > 0 {
		return current[0]
	}
	if len(current) > 1 && current[1] > 0 {
		return current[1]
	}
	if len(current) > 2 && current[2] > 0 {
		return current[2]
	}
	return 0
}

// keepaDomainTLD maps a Keepa domain ID to the Amazon TLD.
func keepaDomainTLD(domain int) string {
	switch domain {
	case 1:
		return "com"
	case 2:
		return "co.uk"
	case 3:
		return "de"
	case 4:
		return "fr"
	case 5:
		return "co.jp"
	case 6:
		return "it"
	case 7:
		return "es"
	case 8:
		return "in"
	case 9:
		return "com.mx"
	default:
		return "fr"
	}
}
