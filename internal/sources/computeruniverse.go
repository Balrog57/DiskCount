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
		if len(cfg.ComputeruniverseURLs) == 0 {
			return nil
		}
		return &Computeruniverse{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.ComputeruniverseURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Computeruniverse struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Computeruniverse) Name() string  { return "computeruniverse" }
func (s *Computeruniverse) RateLimit() (int, time.Duration) { return 1, 2 * time.Second }

func (s *Computeruniverse) Info() SourceInfo {
	return SourceInfo{
		Name:        "computeruniverse",
		Description: "Computeruniverse (HDD/SSD, EUR) — requiert Byparr (403 sans UA navigateur)",
		Categories:  []string{"scraping"},
		Requires:    []string{"COMPUTERUNIVERSE_URLS", "BYPARR_URL"},
		Version:     "1",
	}
}

// Fetch first tries HTTP, then falls back to Byparr (403 without browser UA).
func (s *Computeruniverse) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return fetchWithByparrFallback(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseComputeruniverse)
}

func parseComputeruniverse(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("article.c-productTile").Each(func(_ int, s *goquery.Selection) {
		linkEl := s.Find("a.c-productTile__title")
		title := strings.TrimSpace(linkEl.Text())
		if title == "" {
			return
		}
		href, _ := linkEl.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		priceText := strings.TrimSpace(s.Find("span.c-productTile__price").Text())
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
		cond := domain.ConditionNew
		deals = append(deals, domain.Deal{
			Source:        "computeruniverse",
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
