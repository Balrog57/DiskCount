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
		if len(cfg.FnacURLs) == 0 {
			return nil
		}
		return &Fnac{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.FnacURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Fnac struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Fnac) Name() string { return "fnac" }

func (s *Fnac) Info() SourceInfo {
	return SourceInfo{
		Name:        "fnac",
		Description: "Fnac France (HDD/SSD, EUR) — requiert Byparr (anti-bot 403/404)",
		Categories:  []string{"scraping"},
		Requires:    []string{"FNAC_URLS", "BYPARR_URL"},
		Version:     "3",
	}
}

func (s *Fnac) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return fetchWithByparrFallback(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseFnac)
}

// parseFnac scrapes Fnac listing pages. Fnac blocks non-browser requests
// (403/404), so Byparr is required in production. Selectors cover classic
// Article-itemGroup cards and the newer f-product / Article-item layouts.
func parseFnac(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	seen := map[string]bool{}
	doc.Find("article.Article-itemGroup, article.Article-item, div.f-productCard, li.Article-item, div.product, li.product").Each(func(_ int, s *goquery.Selection) {
		linkEl := s.Find("a.Article-titleLink, a.f-productCard-link, a.product-name, a[href*='/a/'], a[href*='/p/']").First()
		title := strings.TrimSpace(linkEl.Text())
		if title == "" {
			title = strings.TrimSpace(s.Find(".f-productCard-title, .Article-desc, h3").First().Text())
		}
		if title == "" {
			return
		}
		href, _ := linkEl.Attr("href")
		if href == "" {
			href, _ = s.Find("a[href*='/a/'], a[href*='/p/']").First().Attr("href")
		}
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" || seen[href] {
			return
		}
		priceText := strings.TrimSpace(s.Find("span.userPrice, span.f-priceBox-price, strong.f-priceBox-price, span.f-priceBox__price, span.price").First().Text())
		if priceText == "" {
			priceText = strings.TrimSpace(s.Find("[data-price], .f-priceBox").First().Text())
		}
		priceEUR, err := parseFloatClean(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}
		capText := title + " " + strings.TrimSpace(s.Find("p, .description, .specs, .f-productCard-summary").Text())
		tb, err := parsing.ParseCapacityTB(capText)
		if err != nil || tb <= 0 {
			return
		}
		media := normalMedia(capText)
		if media == nil {
			return
		}
		dc := parsing.NormalizeDriveCategory(capText, media)
		ifaces := parsing.NormalizeInterfaces(capText)
		if len(ifaces) == 0 && dc != nil {
			ifaces = parsing.DefaultInterfacesForCategory(*dc)
		}
		cond := domain.ConditionNew
		deal := domain.Deal{
			Source:        "fnac",
			Title:         title,
			URL:           href,
			PriceEUR:      round2(priceEUR),
			PricePerTB:    round2(priceEUR / tb),
			CapacityTB:    round2(tb),
			Condition:     &cond,
			MediaType:     media,
			DriveCategory: dc,
			Interfaces:    ifaces,
			ObservedAt:    domain.UTCNow(),
		}
		if ext := externalIDFromHref(href, "/p/a", "/a/"); ext != nil {
			deal.ExternalID = ext
		}
		deal = enrichCardDeal(deal, s, baseURL)
		seen[href] = true
		deals = append(deals, deal)
	})
	return deals
}
