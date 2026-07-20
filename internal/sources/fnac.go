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
		Description: "Fnac/Darty (HDD/SSD, EUR) — requiert Byparr (anti-bot 403/404)",
		Categories:  []string{"scraping"},
		Requires:    []string{"FNAC_URLS", "BYPARR_URL"},
		Version:     "2",
	}
}

func (s *Fnac) Fetch(ctx context.Context) ([]domain.Deal, error) {
	res := fetchMultiURL(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseFnac)
	if err := res.asTransientError(s.Name()); err != nil {
		return nil, err
	}
	return res.deals, nil
}

// parseFnac scrapes Fnac listing pages. Fnac blocks non-browser requests
// (403/404 on all tested URLs), so Byparr is required. The selectors are
// placeholders based on common Fnac patterns — to be adjusted after the first
// dry-run with Byparr returns the real HTML.
func parseFnac(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	// Fnac uses various product card selectors across redesigns. We try the
	// most common patterns and fall back to links containing "/p/" or "/a/".
	doc.Find("article.Article-itemGroup, div.product, li.product").Each(func(_ int, s *goquery.Selection) {
		linkEl := s.Find("a.Article-titleLink, a.product-name, a[href*='/p/'], a[href*='/a/']").First()
		title := strings.TrimSpace(linkEl.Text())
		if title == "" {
			return
		}
		href, _ := linkEl.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		priceText := strings.TrimSpace(s.Find("span.userPrice, span.f-priceBox-price, span.price").Text())
		priceEUR, err := parseFloatClean(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}
		capText := title + " " + strings.TrimSpace(s.Find("p, .description, .specs").Text())
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
		deals = append(deals, domain.Deal{
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
		})
	})
	return deals
}