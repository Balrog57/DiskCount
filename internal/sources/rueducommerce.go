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
		if len(cfg.RueDuCommerceURLs) == 0 {
			return nil
		}
		return &RueDuCommerce{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.RueDuCommerceURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type RueDuCommerce struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *RueDuCommerce) Name() string { return "rueducommerce" }

func (s *RueDuCommerce) Info() SourceInfo {
	return SourceInfo{
		Name:        "rueducommerce",
		Description: "Rue du Commerce (HDD/SSD, EUR)",
		Categories:  []string{"scraping"},
		Requires:    []string{"RUEDUCOMMERCE_URLS"},
		Version:     "2",
	}
}

func (s *RueDuCommerce) Fetch(ctx context.Context) ([]domain.Deal, error) {
	res := fetchMultiURL(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseRueDuCommerce)
	if err := res.asTransientError(s.Name()); err != nil {
		return nil, err
	}
	return res.deals, nil
}

// parseRueDuCommerce scrapes Rue du Commerce listing pages. Verified against
// the real HTML: products are <li class="pdt-item"> with the title in
// h3.title-3, the price in div.price > div.price (format "198,15€"), and the
// product/offer IDs in data-id / data-offer-id attributes.
func parseRueDuCommerce(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("li.pdt-item").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Find(".listing-product__infos a[href^='/p/']").Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		title := strings.TrimSpace(s.Find("h3.title-3").Text())
		if title == "" {
			return
		}
		// Price is in nested divs: <div class="price"><div class="price">198,15€</div></div>
		priceText := strings.TrimSpace(s.Find("div.price > div.price").Text())
		priceEUR, err := parseFloatClean(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}
		// Description contains capacity info.
		desc := strings.TrimSpace(s.Find(".listing-product__desc").Text())
		capText := title + " " + desc
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
		var extID *string
		if id, ok := s.Attr("data-id"); ok && id != "" {
			extID = &id
		}
		deal := domain.Deal{
			Source:        "rueducommerce",
			Title:         title,
			URL:           href,
			PriceEUR:      round2(priceEUR),
			PricePerTB:    round2(priceEUR / tb),
			CapacityTB:    round2(tb),
			Condition:     &cond,
			MediaType:     media,
			ExternalID:    extID,
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
