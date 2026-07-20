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
		if len(cfg.CdiscountURLs) == 0 {
			return nil
		}
		return &Cdiscount{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.CdiscountURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Cdiscount struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Cdiscount) Name() string { return "cdiscount" }

func (s *Cdiscount) Info() SourceInfo {
	return SourceInfo{
		Name:        "cdiscount",
		Description: "Cdiscount (HDD/SSD, EUR) — requiert Byparr (anti-bot Baleen JS)",
		Categories:  []string{"scraping"},
		Requires:    []string{"CDISCOUNT_URLS", "BYPARR_URL"},
		Version:     "2",
	}
}

func (s *Cdiscount) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return fetchWithByparrFallback(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseCdiscount)
}

func parseCdiscount(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	// Cdiscount product cards use various class names across redesigns:
	// li.prdtBloc (legacy), li.sc-dmzty (current), div[class*='product'].
	doc.Find("li.prdtBloc, li.sc-dmzty, div[class*='product'], article").Each(func(_ int, s *goquery.Selection) {
		// Find a product link: prefer links with product names as text,
		// skip short/no-text links (icons, buttons).
		var href, title string
		s.Find("a").Each(func(_ int, a *goquery.Selection) {
			if title != "" {
				return
			}
			t := strings.TrimSpace(a.Text())
			if len(t) < 10 || strings.HasPrefix(t, "Ajouter") || strings.HasPrefix(t, "acheter") {
				return
			}
			h, _ := a.Attr("href")
			if h != "" && !strings.HasPrefix(h, "#") && !strings.Contains(h, "javascript") {
				href = absolutizeURL(baseURL, strings.TrimSpace(h))
				title = t
			}
		})
		if href == "" || title == "" {
			return
		}
		// Try multiple price extraction strategies.
		var priceEUR float64
		// 1. Data attributes (most reliable if present).
		s.Find("[data-price], [data-prix], [itemprop='price']").Each(func(_ int, el *goquery.Selection) {
			if priceEUR > 0 {
				return
			}
			for _, attr := range []string{"data-price", "data-prix", "content"} {
				if v, ok := el.Attr(attr); ok && v != "" {
					if p, err := parseFloatClean(v); err == nil && p > 0 {
						priceEUR = p
					}
				}
			}
		})
		// 2. JSON-LD (common on Cdiscount).
		if priceEUR <= 0 {
			doc.Find("script[type='application/ld+json']").Each(func(_ int, el *goquery.Selection) {
				if priceEUR > 0 {
					return
				}
				jsonText := el.Text()
				if strings.Contains(jsonText, title[:min(10, len(title))]) {
					if p, err := parsing.ParsePriceEUR(jsonText); err == nil && p > 0 {
						priceEUR = p
					}
				}
			})
		}
		// 3. Text price elements.
		if priceEUR <= 0 {
			s.Find("[class*='price'], span.price, .prdtPrice, .product-price").Each(func(_ int, el *goquery.Selection) {
				if priceEUR > 0 {
					return
				}
				if p, err := parseFloatClean(el.Text()); err == nil && p > 0 {
					priceEUR = p
				}
			})
		}
		if priceEUR <= 0 {
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
			Source:        "cdiscount",
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
