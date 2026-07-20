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

func (s *Computeruniverse) Name() string { return "computeruniverse" }

func (s *Computeruniverse) Info() SourceInfo {
	return SourceInfo{
		Name:        "computeruniverse",
		Description: "Computeruniverse (HDD/SSD, EUR) — requiert Byparr",
		Categories:  []string{"scraping"},
		Requires:    []string{"COMPUTERUNIVERSE_URLS", "BYPARR_URL"},
		Version:     "2",
	}
}

func (s *Computeruniverse) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return fetchWithByparrFallback(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseComputeruniverse)
}

func parseComputeruniverse(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("article.c-productTile, div[data-product-id], a[class*='product'], div[class*='productTile']").Each(func(_ int, s *goquery.Selection) {
		var href, title string
		s.Find("a").Each(func(_ int, a *goquery.Selection) {
			if title != "" {
				return
			}
			t := strings.TrimSpace(a.Text())
			if len(t) < 10 {
				return
			}
			h, _ := a.Attr("href")
			if h != "" && !strings.HasPrefix(h, "#") {
				href = absolutizeURL(baseURL, strings.TrimSpace(h))
				title = t
			}
		})
		if href == "" || title == "" {
			return
		}
		priceText := strings.TrimSpace(s.Find("[class*='price'], span.price, .product-price").First().Text())
		if priceText == "" {
			s.Find("span, div").Each(func(_ int, el *goquery.Selection) {
				if priceText != "" {
					return
				}
				t := strings.TrimSpace(el.Text())
				if strings.Contains(t, "€") || strings.Contains(t, "EUR") {
					priceText = t
				}
			})
		}
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
