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
		if len(cfg.TopachatURLs) == 0 {
			return nil
		}
		return &Topachat{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.TopachatURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Topachat struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Topachat) Name() string { return "topachat" }

func (s *Topachat) Info() SourceInfo {
	return SourceInfo{
		Name:        "topachat",
		Description: "TopAchat (HDD/SSD, EUR)",
		Categories:  []string{"scraping"},
		Requires:    []string{"TOPACHAT_URLS"},
		Version:     "2",
	}
}

func (s *Topachat) Fetch(ctx context.Context) ([]domain.Deal, error) {
	res := fetchMultiURL(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseTopachat)
	if err := res.asTransientError(s.Name()); err != nil {
		return nil, err
	}
	return res.deals, nil
}

// parseTopachat scrapes TopAchat listing pages (Svelte SSR). Verified against
// the real HTML: each product is <article class="product"> wrapped in an <a
// class="product-list__product">. Title in h2.product__label, price in
// span.product__price (format "269.99 €", point decimal), description in
// span.product__sublabel.
func parseTopachat(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("a.product-list__product").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		article := a.Find("article.product")
		if article.Length() == 0 {
			return
		}
		title := strings.TrimSpace(article.Find("h2.product__label").Text())
		if title == "" {
			return
		}
		priceText := strings.TrimSpace(article.Find("span.product__price").Text())
		priceEUR, err := parseFloatClean(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}
		sublabel := strings.TrimSpace(article.Find("span.product__sublabel").Text())
		capText := title + " " + sublabel
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
			Source:        "topachat",
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
		deals = append(deals, enrichCardDeal(deal, article, baseURL))
	})
	return deals
}
