package sources

import (
	"context"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
	"github.com/PuerkitoBio/goquery"
)

type retailerParser func(string, string) []domain.Deal

type frenchRetailer struct {
	name, description string
	urls              []string
	http              scraper.Fetcher
	byparr            *scraper.ByparrClient
	useFB             bool
	parse             retailerParser
}

func (s *frenchRetailer) Name() string { return s.name }
func (s *frenchRetailer) Info() SourceInfo {
	return SourceInfo{Name: s.name, Description: s.description, Categories: []string{"marchand-fr"}, Requires: []string{strings.ToUpper(s.name) + "_URLS"}, Version: "1"}
}
func (s *frenchRetailer) Fetch(ctx context.Context) ([]domain.Deal, error) {
	parse := func(html, base string) []domain.Deal {
		deals := s.parse(html, base)
		for i := range deals {
			deals[i].Source = s.name
		}
		return deals
	}
	return fetchWithByparrFallback(ctx, s.name, s.http, s.byparr, s.urls, s.useFB, parse)
}

func init() {
	Register(func(r *Registry) Source {
		return newFrenchRetailer(r, "darty", "Darty France (HDD/SSD)", r.Config().DartyURLs, parseGenericRetailer)
	})
	Register(func(r *Registry) Source {
		return newFrenchRetailer(r, "materiel", "Materiel.net (HDD/SSD)", r.Config().MaterielURLs, parseMateriel)
	})
	Register(func(r *Registry) Source {
		return newFrenchRetailer(r, "cybertek", "Cybertek France (HDD/SSD)", r.Config().CybertekURLs, parseGrosbill)
	})
	Register(func(r *Registry) Source {
		return newFrenchRetailer(r, "corsair", "Corsair France (SSD)", r.Config().CorsairURLs, parseCorsair)
	})
	Register(func(r *Registry) Source {
		return newFrenchRetailer(r, "pccomponentes", "PCComponentes France (HDD/SSD)", r.Config().PCComponentesURLs, parseGenericRetailer)
	})
	Register(func(r *Registry) Source {
		return newFrenchRetailer(r, "topbiz", "Topbiz France (HDD/SSD)", r.Config().TopbizURLs, parseTopbiz)
	})
}

func newFrenchRetailer(r *Registry, name, description string, urls []string, parse retailerParser) Source {
	if len(urls) == 0 {
		return nil
	}
	return &frenchRetailer{name: name, description: description, urls: urls, http: r.HTTP(), byparr: r.Byparr(), useFB: r.Config().HeadlessFallback, parse: parse}
}

func retailerDeal(title, rawURL string, price float64, details string) (domain.Deal, bool) {
	title, rawURL = strings.TrimSpace(title), strings.TrimSpace(rawURL)
	if title == "" || rawURL == "" || price <= 0 {
		return domain.Deal{}, false
	}
	text := strings.TrimSpace(title + " " + details)
	capacity, err := parsing.ParseCapacityTB(text)
	if err != nil || capacity <= 0 {
		return domain.Deal{}, false
	}
	media := normalMedia(text)
	if media == nil {
		return domain.Deal{}, false
	}
	category := parsing.NormalizeDriveCategory(text, media)
	interfaces := parsing.NormalizeInterfaces(text)
	if len(interfaces) == 0 && category != nil {
		interfaces = parsing.DefaultInterfacesForCategory(*category)
	}
	condition := domain.ConditionNew
	return domain.Deal{Title: title, URL: rawURL, PriceEUR: round2(price), PricePerTB: round2(price / capacity), CapacityTB: round2(capacity), Condition: &condition, MediaType: media, DriveCategory: category, Interfaces: interfaces, ObservedAt: domain.UTCNow()}, true
}

func parseMateriel(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var out []domain.Deal
	doc.Find(".c-product-block").Each(func(_ int, card *goquery.Selection) {
		link := card.Find("a.c-product__link").First()
		href, _ := link.Attr("href")
		price, err := parseLDLCPrice(card.Find(".o-product__price").First().Text())
		if err != nil {
			return
		}
		if deal, ok := retailerDeal(card.Find(".c-product__title").First().Text(), absolutizeURL(baseURL, href), price, card.Find(".product-specs").First().Text()); ok {
			out = append(out, deal)
		}
	})
	return out
}

func parseTopbiz(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var out []domain.Deal
	doc.Find("article.product-miniature").Each(func(_ int, card *goquery.Selection) {
		link := card.Find(".product-title-mini a, .product-title a").First()
		href, _ := link.Attr("href")
		priceEl := card.Find(".product-price-and-shipping .price [content]").Last()
		priceText, _ := priceEl.Attr("content")
		if priceText == "" {
			priceText = card.Find(".product-price-and-shipping .price").Last().Text()
		}
		price, err := parseFloatClean(priceText)
		if err != nil {
			return
		}
		if deal, ok := retailerDeal(link.Text(), absolutizeURL(baseURL, href), price, card.Text()); ok {
			out = append(out, deal)
		}
	})
	return out
}

func parseCorsair(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var out []domain.Deal
	doc.Find("[data-product-sku][data-product-price]").Each(func(_ int, card *goquery.Selection) {
		title, _ := card.Attr("data-product-name")
		priceText, _ := card.Attr("data-product-price")
		price, err := parseFloatClean(priceText)
		if err != nil {
			return
		}
		link := card.Find("a[href]").First()
		href, _ := link.Attr("href")
		if deal, ok := retailerDeal(title, absolutizeURL(baseURL, href), price, card.Text()); ok {
			out = append(out, deal)
		}
	})
	return out
}

func parseGenericRetailer(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var out []domain.Deal
	doc.Find("article, li.product, li.product-list__item, [data-product-id]").Each(func(_ int, card *goquery.Selection) {
		link := card.Find("a[href]").First()
		href, _ := link.Attr("href")
		title := strings.TrimSpace(card.Find("[itemprop='name'], h2, h3, .product-name, .product-title").First().Text())
		if title == "" {
			title = strings.TrimSpace(link.Text())
		}
		priceText := ""
		priceEl := card.Find("[itemprop='price'], [data-price], .price, [class*='price']").First()
		for _, attr := range []string{"content", "data-price"} {
			if value, ok := priceEl.Attr(attr); ok && value != "" {
				priceText = value
				break
			}
		}
		if priceText == "" {
			priceText = priceEl.Text()
		}
		price, err := parseFloatClean(priceText)
		if err != nil {
			return
		}
		if deal, ok := retailerDeal(title, absolutizeURL(baseURL, href), price, card.Text()); ok {
			out = append(out, deal)
		}
	})
	return out
}
