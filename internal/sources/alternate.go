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
		if len(cfg.AlternateURLs) == 0 {
			return nil
		}
		return &Alternate{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.AlternateURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Alternate struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Alternate) Name() string                    { return "alternate" }
func (s *Alternate) RateLimit() (int, time.Duration) { return 1, 2 * time.Second }

func (s *Alternate) Info() SourceInfo {
	return SourceInfo{
		Name:        "alternate",
		Description: "Alternate France (HDD/SSD, EUR)",
		Categories:  []string{"scraping"},
		Requires:    []string{"ALTERNATE_URLS"},
		Version:     "2",
	}
}

func (s *Alternate) Fetch(ctx context.Context) ([]domain.Deal, error) {
	res := fetchMultiURL(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseAlternate)
	if err := res.asTransientError(s.Name()); err != nil {
		return nil, err
	}
	return res.deals, nil
}

// parseAlternate scrapes Alternate listing pages. Verified against the real
// HTML structure: each product is an <a class="productBox"> wrapping the whole
// card. Title lives in .product-name, price in span.price (format "€ 129,90"),
// and the capacity is the <li> starting with "Kapazität:" inside the bullet list.
func parseAlternate(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("div.grid-container.listing > a.productBox").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		title := strings.TrimSpace(s.Find(".product-name").Text())
		if title == "" {
			return
		}
		priceText := strings.TrimSpace(s.Find("span.price").Text())
		priceEUR, err := parseFloatClean(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}
		// Capacity lives in the bullet list: <li>Kapazität: 2 TB</li>
		capText := ""
		s.Find("ul.product-bullet-list li").Each(func(_ int, li *goquery.Selection) {
			t := strings.TrimSpace(li.Text())
			if strings.HasPrefix(t, "Kapazität:") {
				capText = strings.TrimSpace(strings.TrimPrefix(t, "Kapazität:"))
			}
		})
		// Fallback: extract from the title if the bullet list was empty.
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
		classText := title + " " + strings.TrimSpace(s.Find(".product-name-sub").Text())
		dc := parsing.NormalizeDriveCategory(classText, media)
		ifaces := parsing.NormalizeInterfaces(classText)
		if len(ifaces) == 0 && dc != nil {
			ifaces = parsing.DefaultInterfacesForCategory(*dc)
		}
		cond := domain.ConditionNew
		deal := domain.Deal{
			Source:        "alternate",
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
