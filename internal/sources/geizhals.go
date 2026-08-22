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
		if len(cfg.GeizhalsURLs) == 0 {
			return nil
		}
		return &Geizhals{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.GeizhalsURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

// Geizhals is a price aggregator (geizhals.de / geizhals.at / geizhals.eu).
// Deals are emitted only from product pages with per-merchant offer rows; category
// listings that only show aggregate "ab €X" prices are skipped.
type Geizhals struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Geizhals) Name() string { return "geizhals" }

func (s *Geizhals) Info() SourceInfo {
	return SourceInfo{
		Name:        "geizhals",
		Description: "Geizhals.de (agregateur de prix HDD/SSD, EUR)",
		Categories:  []string{"scraping"},
		Requires:    []string{"GEIZHALS_URLS"},
		Version:     "3",
	}
}

func (s *Geizhals) Fetch(ctx context.Context) ([]domain.Deal, error) {
	res := fetchMultiURL(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseGeizhals)
	if err := res.asTransientError(s.Name()); err != nil {
		return nil, err
	}
	return res.deals, nil
}

// parseGeizhalsPrice handles the German price format "€ 241,90" or "€ 1.284,00"
// where the dot is a thousands separator and the comma is the decimal mark.
func parseGeizhalsPrice(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "€", "")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, " ", "")
	if strings.Contains(s, ",") && strings.Contains(s, ".") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	} else if strings.Contains(s, ",") {
		s = strings.Replace(s, ",", ".", 1)
	}
	return parseFloatClean(s)
}

func parseGeizhals(html, baseURL string) []domain.Deal {
	if offers := parseGeizhalsOffers(html, baseURL); len(offers) > 0 {
		return offers
	}
	// Category listings only show Geizhals aggregate prices — skip them until
	// we fetch a product page with per-merchant offer rows.
	return nil
}

func parseGeizhalsOffers(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	rows := doc.Find("#lazy-list--offers .offer, .variant__content__offers-ajax .offer")
	if rows.Length() == 0 {
		return nil
	}

	title := strings.TrimSpace(doc.Find("h1.product__title, .product-name").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").Text())
	}
	capText := title
	mpnText := strings.TrimSpace(doc.Find(".product-mpn").First().Text())
	mpnText = strings.TrimSuffix(mpnText, "|")
	mpnText = strings.TrimSpace(mpnText)
	if mpnText != "" {
		capText += " " + mpnText
	}
	tb, err := parsing.ParseCapacityTB(capText)
	if err != nil || tb <= 0 {
		return nil
	}
	media := normalMedia(capText)
	if media == nil {
		return nil
	}
	dc := parsing.NormalizeDriveCategory(capText, media)
	ifaces := parsing.NormalizeInterfaces(capText)
	if len(ifaces) == 0 && dc != nil {
		ifaces = parsing.DefaultInterfacesForCategory(*dc)
	}
	cond := domain.ConditionNew

	var deals []domain.Deal
	rows.Each(func(_ int, row *goquery.Selection) {
		shopURL := geizhalsShopURL(row, baseURL)
		if shopURL == "" {
			return
		}
		if _, _, ok := domain.MerchantFromURL(shopURL); !ok {
			return
		}
		priceText := strings.TrimSpace(row.Find(".price, .offer__price-link").First().Text())
		if priceText == "" {
			priceText = strings.TrimSpace(row.Find("a.offer__price-link").First().Text())
		}
		priceEUR, err := parseGeizhalsPrice(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}
		dealTitle := title
		if merchant := strings.TrimSpace(row.Find(".offer__merchant-info-links a").First().Text()); merchant != "" {
			dealTitle = title + " — " + merchant
		}
		deal := domain.Deal{
			Title:         dealTitle,
			URL:           shopURL,
			PriceEUR:      round2(priceEUR),
			PricePerTB:    round2(priceEUR / tb),
			CapacityTB:    round2(tb),
			Condition:     &cond,
			MediaType:     media,
			DriveCategory: dc,
			Interfaces:    ifaces,
			ObservedAt:    domain.UTCNow(),
		}
		if ext := parsing.ExtractASIN(shopURL); ext != nil {
			deal.ExternalID = ext
			deal.SKU = amazonSKU("", ext)
			deal.ImageURL = amazonImageFromASIN(ext)
		}
		if sku := partNumberFromText(capText); sku != nil {
			deal.SKU = sku
		} else if mpnText != "" {
			deal.SKU = strPtr(mpnText)
		}
		deals = append(deals, enrichCardDeal(deal, row, baseURL))
	})
	return deals
}

func geizhalsShopURL(row *goquery.Selection, baseURL string) string {
	var best string
	row.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		if strings.Contains(href, "/merchants/") {
			return
		}
		if _, _, ok := domain.MerchantFromURL(href); ok {
			best = href
		}
	})
	return best
}
