package sources

import (
	"context"
	"net/url"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
	"github.com/PuerkitoBio/goquery"
)

func init() {
	Register(func(r *Registry) Source {
		cfg := r.Config()
		if len(cfg.PricePerTBURLs) == 0 {
			return nil
		}
		return &PricePerTB{http: r.HTTP(), byparr: r.Byparr(), urls: cfg.PricePerTBURLs, useFB: cfg.HeadlessFallback}
	})
}

type PricePerTB struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *PricePerTB) Name() string { return "pricepertb" }

func (s *PricePerTB) Info() SourceInfo {
	return SourceInfo{
		Name:        "pricepertb",
		Description: "Tableau HTML pricepertb.com (€/To, multi-pays)",
		Categories:  []string{"scraping"},
		Requires:    []string{"PRICEPERTB_URLS"},
		Version:     "1",
	}
}

func (s *PricePerTB) Fetch(ctx context.Context) ([]domain.Deal, error) {
	res := fetchMultiURL(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parsePTB)
	if err := res.asTransientError(s.Name()); err != nil {
		return nil, err
	}
	return res.deals, nil
}

func parsePTB(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
		cells := row.Find("td")
		if cells.Length() < 3 {
			return
		}
		texts := make([]string, cells.Length())
		cells.Each(func(i int, c *goquery.Selection) { texts[i] = strings.TrimSpace(c.Text()) })
		if strings.Contains(strings.ToLower(texts[0]), "price") {
			return
		}

		priceEUR, err := parseFloatClean(textAt(texts, 2))
		if err != nil || priceEUR <= 0 {
			return
		}
		pricePerTB, err := parseFloatClean(strings.TrimSpace(row.Find("td.price-per-tb").First().Text()))
		if err != nil || pricePerTB <= 0 {
			return
		}
		capText := textAt(texts, 3)
		var tb float64
		if attr, ok := row.Attr("data-capacity"); ok {
			if gb, e := parseFloatClean(attr); e == nil && gb > 0 {
				tb = gb / 1000
			}
		}
		if tb <= 0 {
			if parsed, e := parsing.ParseCapacityTB(capText); e == nil && parsed > 0 {
				tb = parsed
			}
		}
		if tb <= 0 {
			if parts := strings.Fields(capText); len(parts) > 0 {
				if v, e := parseFloatClean(parts[0]); e == nil && v > 0 {
					tb = v
					if len(parts) > 1 && strings.EqualFold(parts[1], "Go") {
						tb /= 1000
					}
				}
			}
		}
		if tb <= 0 {
			return
		}
		link := row.Find("a").First()
		href, _ := link.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if !isAmazonURL(href) {
			return
		}
		title := strings.TrimSpace(link.Text())
		if title == "" && len(texts) > 2 {
			title = strings.TrimSpace(texts[2])
		}
		if title == "" || href == "" {
			return
		}

		formFactor := textAt(texts, 5)
		technology := textAt(texts, 6)
		classText := strings.Join([]string{title, formFactor, technology}, " ")
		media := normalMedia(classText)
		if media == nil {
			return
		}
		dc := parsing.NormalizeDriveCategory(classText, media)
		ifaces := parsing.NormalizeInterfaces(classText)
		if len(ifaces) == 0 && dc != nil {
			ifaces = parsing.DefaultInterfacesForCategory(*dc)
		}

		cond := domain.ConditionNew
		condText := strings.TrimSpace(textAt(texts, 7))
		if attr, ok := row.Attr("data-condition"); ok && strings.TrimSpace(attr) != "" {
			condText = attr
		}
		if strings.Contains(strings.ToLower(condText), "used") || strings.Contains(strings.ToLower(condText), "occasion") {
			cond = domain.ConditionUsed
		}
		deals = append(deals, domain.Deal{
			Source: "pricepertb", Title: title, URL: href,
			PriceEUR: round2(priceEUR), PricePerTB: round2(pricePerTB), CapacityTB: round2(tb),
			Condition: &cond, MediaType: media,
			ExternalID:    parsing.ExtractASIN(href),
			SKU:           amazonSKU("", parsing.ExtractASIN(href)),
			ImageURL:      amazonImageFromASIN(parsing.ExtractASIN(href)),
			DriveCategory: dc, Interfaces: ifaces,
			FormFactor: strPtr(formFactor), Technology: strPtr(technology),
			ObservedAt: domain.UTCNow(),
		})
	})
	return deals
}

func textAt(texts []string, i int) string {
	if i < 0 || i >= len(texts) {
		return ""
	}
	return strings.TrimSpace(texts[i])
}

func absolutizeURL(baseURL, raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return raw
	}
	return base.ResolveReference(parsed).String()
}
