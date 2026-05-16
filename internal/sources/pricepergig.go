package sources

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
)

func init() {
	Register(func(r *Registry) Source {
		cfg := r.Config()
		if !cfg.PricePerGigEnabled {
			return nil
		}
		return &PricePerGig{http: r.HTTP(), apiURL: cfg.PricePerGigAPIURL, market: cfg.PricePerGigMarket}
	})
}

type PricePerGig struct {
	http   *scraper.HTTPFetcher
	apiURL string
	market string
}

func (s *PricePerGig) Name() string { return "pricepergig" }

func (s *PricePerGig) Fetch(ctx context.Context) ([]domain.Deal, error) {
	html, err := s.http.Get(ctx, s.apiURL+"?marketplace=eq."+s.market+"&technology=in.(HDD,SSD)&limit=50")
	if err != nil {
		return nil, err
	}

	var drives []struct {
		ID         json.Number `json:"id"`
		Title      string      `json:"title"`
		URL        string      `json:"affiliate_url"`
		Price      float64     `json:"price"`
		CapacityGB float64     `json:"capacity_gb"`
		Technology string      `json:"technology"`
		FormFactor string      `json:"form_factor"`
		ASIN       string      `json:"asin"`
		Condition  string      `json:"condition"`
	}
	if err := json.Unmarshal([]byte(html), &drives); err != nil {
		return nil, err
	}

	var deals []domain.Deal
	for _, d := range drives {
		tb := d.CapacityGB / 1000
		if tb <= 0 || d.Price <= 0 {
			continue
		}
		pt := d.Price / tb
		cond := domain.ConditionNew
		if strings.Contains(strings.ToLower(d.Condition), "used") {
			cond = domain.ConditionUsed
		}
		var extID *string
		if d.ASIN != "" {
			extID = &d.ASIN
		} else if id := d.ID.String(); id != "" {
			extID = &id
		}
		deals = append(deals, domain.Deal{
			Source: "pricepergig", Title: d.Title, URL: d.URL,
			PriceEUR: round2(d.Price), PricePerTB: round2(pt), CapacityTB: round2(tb),
			Condition:  &cond,
			MediaType:  normalMedia(d.Technology),
			ExternalID: extID,
			FormFactor: strPtr(d.FormFactor), Technology: strPtr(d.Technology),
			ObservedAt: domain.UTCNow(),
		})
	}
	slog.Debug("pricepergig", "deals", len(deals))
	return deals, nil
}

func normalMedia(text string) *domain.MediaType {
	t := strings.ToLower(text)
	if strings.Contains(t, "ssd") || strings.Contains(t, "nvme") || strings.Contains(t, "solid") {
		m := domain.MediaTypeSolidState
		return &m
	}
	if strings.Contains(t, "hdd") || strings.Contains(t, "disque dur") || strings.Contains(t, "hard drive") || strings.Contains(t, "rpm") {
		m := domain.MediaTypeRotational
		return &m
	}
	return nil
}

var _ = parsing.ParsePriceEUR
