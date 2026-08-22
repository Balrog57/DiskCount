package sources

import (
	"context"
	"strconv"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
	"github.com/PuerkitoBio/goquery"
)

func init() {
	Register(func(r *Registry) Source {
		cfg := r.Config()
		if len(cfg.BoulangerURLs) == 0 {
			return nil
		}
		return &Boulanger{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.BoulangerURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Boulanger struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Boulanger) Name() string { return "boulanger" }

func (s *Boulanger) Info() SourceInfo {
	return SourceInfo{
		Name:        "boulanger",
		Description: "Boulanger.com (HDD/SSD, EUR) — SPA, Byparr recommande",
		Categories:  []string{"scraping"},
		Requires:    []string{"BOULANGER_URLS", "BYPARR_URL"},
		Version:     "2",
	}
}

// Fetch first tries HTTP, then falls back to Byparr if the page returns no
// products (Boulanger is a SPA — the server HTML is an empty shell without JS).
func (s *Boulanger) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return fetchWithByparrFallback(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseBoulanger)
}

// parseBoulanger scrapes Boulanger listing pages. Verified against the real
// HTML: products are <li class="product-list__item"> with the title in
// h3#productLabel, the price in data-analytics_product_unitprice_ati (point
// decimal, more reliable than the text ".price__amount" which uses a comma),
// and the capacity in .keypoints__item.
func parseBoulanger(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("li.product-list__item").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Find("a.product-list__product-image-link").Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		title := strings.TrimSpace(s.Find("h3#productLabel").Text())
		if title == "" {
			return
		}
		// Prefer the data attribute (point decimal, e.g. "329.99") over the
		// text ".price__amount" (comma decimal, e.g. "329,99€").
		priceStr, _ := s.Find("a.product-list__product-image-link").Attr("data-analytics_product_unitprice_ati")
		var priceEUR float64
		var perr error
		if priceStr != "" {
			priceEUR, perr = strconv.ParseFloat(priceStr, 64)
		} else {
			priceEUR, perr = parseFloatClean(s.Find(".price__amount").Text())
		}
		if perr != nil || priceEUR <= 0 {
			return
		}
		// Capacity from keypoints: "Capacité de stockage : 4 To"
		capText := ""
		s.Find(".keypoints__item").Each(func(_ int, li *goquery.Selection) {
			t := strings.TrimSpace(li.Text())
			if strings.Contains(t, "Capacité") {
				capText = t
			}
		})
		if capText == "" {
			capText = title
		}
		tb, err := parsing.ParseCapacityTB(capText)
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
			Source:        "boulanger",
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
		deal = withCardImage(deal, s, baseURL)
		deal.SKU = cardSKU(s)
		deals = append(deals, deal)
	})
	return deals
}
