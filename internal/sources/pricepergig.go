package sources

import (
	"context"
	"encoding/json"
	"fmt"
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

func (s *PricePerGig) Info() SourceInfo {
	return SourceInfo{
		Name:        "pricepergig",
		Description: "API JSON pricepergig.com (Amazon marketplace)",
		Categories:  []string{"api"},
		Requires:    []string{"PRICEPERGIG_API_URL", "PRICEPERGIG_MARKET"},
		Version:     "1",
	}
}

func (s *PricePerGig) Fetch(ctx context.Context) ([]domain.Deal, error) {
	html, err := s.http.Get(ctx, s.apiURL+"?marketplace=eq."+s.market+"&technology=in.(HDD,SSD)&limit=50")
	if err != nil {
		return nil, Transient(s.Name(), err)
	}

	var drives []struct {
		ID         json.Number `json:"id"`
		Name       string      `json:"name"`
		Title      string      `json:"title"`
		URL        string      `json:"url"`
		Affiliate  string      `json:"affiliate_url"`
		Price      float64     `json:"price"`
		PricePerTB float64     `json:"price_per_tb"`
		CapacityGB float64     `json:"capacity_gb"`
		Technology string      `json:"technology"`
		Interface  string      `json:"interface"`
		FormFactor string      `json:"form_factor"`
		ASIN       string      `json:"asin"`
		Brand      string      `json:"brand"`
		Model      string      `json:"model"`
		Condition  string      `json:"condition"`
	}
	if err := json.Unmarshal([]byte(html), &drives); err != nil {
		return nil, Schema(s.Name(), err, "response is not valid JSON — pricepergig.com API may have changed")
	}

	var deals []domain.Deal
	for _, d := range drives {
		tb := d.CapacityGB / 1000
		if tb <= 0 || d.Price <= 0 {
			continue
		}
		pt := d.Price / tb
		if d.PricePerTB > 0 {
			pt = d.PricePerTB
		}
		title := strings.TrimSpace(firstNonEmpty(d.Name, d.Title))
		if title == "" {
			title = strings.TrimSpace(strings.Join([]string{d.Technology, d.FormFactor, fmt.Sprintf("%.0f Go", d.CapacityGB)}, " "))
		}
		url := strings.TrimSpace(firstNonEmpty(d.URL, d.Affiliate))
		if url == "" && d.ASIN != "" {
			url = "https://www.amazon.fr/dp/" + d.ASIN
		}
		// Normalise through the shared pipeline so the matcher, the
		// notifier and the DB all see the same fields. pricepergig is
		// the first source migrated; diskprices and the RSS feeds are
		// still using their inline code path.
		price := d.Price
		capacityGB := d.CapacityGB
		deal, rej := Normalize(RawDeal{
			Source:     s.Name(),
			Title:      title,
			URL:        url,
			PriceEUR:   &price,
			CapacityGB: &capacityGB,
			Condition:  d.Condition,
			MediaHint:  d.Technology,
			FormFactor: d.FormFactor,
			Interface:  d.Interface,
			Brand:      d.Brand,
			Model:      d.Model,
			ExternalID: d.ASIN,
		})
		_ = pt // already used inside Normalize; kept for clarity above
		if rej != nil {
			// pricepergig has loose fields; silently skip instead of
			// failing the whole batch.
			slog.Debug("pricepergig skipped", "title", title, "reason", rej.Reason)
			continue
		}
		deals = append(deals, deal)
	}
	slog.Debug("pricepergig", "deals", len(deals))
	return deals, nil
}

// Schema is a small wrapper used by sources that receive structured
// responses (JSON or JSON-LD) and need to flag "the API changed" in a
// way the admin can recognise at a glance.
func Schema(source string, cause error, hint string) error {
	return Wrap(SeveritySchema, "parse", source, cause, hint)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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
