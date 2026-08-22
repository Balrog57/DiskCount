package sources

import (
	"context"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
	"github.com/PuerkitoBio/goquery"
)

func init() {
	Register(func(r *Registry) Source {
		cfg := r.Config()
		if len(cfg.RakutenURLs) == 0 {
			return nil
		}
		return &Rakuten{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.RakutenURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Rakuten struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Rakuten) Name() string { return "rakuten" }

func (s *Rakuten) Info() SourceInfo {
	return SourceInfo{
		Name:        "rakuten",
		Description: "Rakuten France (HDD/SSD, EUR) — requiert Byparr (SPA Next.js)",
		Categories:  []string{"scraping"},
		Requires:    []string{"RAKUTEN_URLS", "BYPARR_URL"},
		Version:     "1",
	}
}

// Fetch first tries HTTP, then falls back to Byparr (Next.js SPA).
func (s *Rakuten) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return fetchWithByparrFallback(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseRakuten)
}

func parseRakuten(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("div.search-results-item").Each(func(_ int, s *goquery.Selection) {
		linkEl := s.Find("a.item-title")
		title := strings.TrimSpace(linkEl.Text())
		if title == "" {
			return
		}
		href, _ := linkEl.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		priceText := strings.TrimSpace(s.Find("span.item-price").Text())
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
		deal := domain.Deal{
			Source: "rakuten",
			Title:  title, URL: href,
			PriceEUR:   round2(priceEUR),
			PricePerTB: round2(priceEUR / tb),
			CapacityTB: round2(tb),
			Condition:  &cond, MediaType: media,
			DriveCategory: dc, Interfaces: ifaces,
			ObservedAt: domain.UTCNow(),
		}
		if ext := externalIDFromHref(href, "/p/", "/product/"); ext != nil {
			deal.ExternalID = ext
		}
		deals = append(deals, enrichCardDeal(deal, s, baseURL))
	})
	return deals
}
