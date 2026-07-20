package sources

import (
	"context"
	"strings"
	"time"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
	"github.com/PuerkitoBio/goquery"
)

func init() {
	Register(func(r *Registry) Source {
		cfg := r.Config()
		if len(cfg.BackmarketURLs) == 0 {
			return nil
		}
		return &Backmarket{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.BackmarketURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

// Backmarket scrapes Back Market category pages for refurbished HDD/SSD
// deals. All products are assumed used/refurbished. The site uses aggressive
// anti-bot (Agari/SquareLine captcha), so a headless fallback via Byparr is
// strongly recommended. Implements RateLimitable (1 req / 2s) to reduce
// triggering.
type Backmarket struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Backmarket) Name() string  { return "backmarket" }
func (s *Backmarket) RateLimit() (int, time.Duration) { return 1, 2 * time.Second }

func (s *Backmarket) Info() SourceInfo {
	return SourceInfo{
		Name:        "backmarket",
		Description: "Back Market (reconditionne HDD/SSD, EUR) — requiert Byparr (bot detection)",
		Categories:  []string{"scraping", "refurbished"},
		Requires:    []string{"BACKMARKET_URLS", "BYPARR_URL"},
		Version:     "1",
	}
}

// Fetch first tries HTTP, then falls back to Byparr (bot detection).
func (s *Backmarket) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return fetchWithByparrFallback(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseBackmarket)
}

func parseBackmarket(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("div[data-cy='product-card']").Each(func(_ int, s *goquery.Selection) {
		linkEl := s.Find("a[data-cy='product-card-link']")
		title := strings.TrimSpace(linkEl.Text())
		if title == "" {
			return
		}
		href, _ := linkEl.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		priceText := strings.TrimSpace(s.Find("span[data-cy='product-card-price']").Text())
		priceEUR, err := parseFloatClean(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}
		tb, err := parsing.ParseCapacityTB(title)
		if err != nil || tb <= 0 {
			return
		}
		media := normalMedia(title)
		if media == nil {
			return
		}
		dc := parsing.NormalizeDriveCategory(title, media)
		ifaces := parsing.NormalizeInterfaces(title)
		if len(ifaces) == 0 && dc != nil {
			ifaces = parsing.DefaultInterfacesForCategory(*dc)
		}
		cond := domain.ConditionUsed
		deals = append(deals, domain.Deal{
			Source:        "backmarket",
			Title:         title, URL: href,
			PriceEUR:      round2(priceEUR),
			PricePerTB:    round2(priceEUR / tb),
			CapacityTB:    round2(tb),
			Condition:     &cond, MediaType: media,
			DriveCategory: dc, Interfaces: ifaces,
			ObservedAt:    domain.UTCNow(),
		})
	})
	return deals
}
