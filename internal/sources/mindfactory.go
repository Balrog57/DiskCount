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
		if len(cfg.MindfactoryURLs) == 0 {
			return nil
		}
		return &Mindfactory{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.MindfactoryURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Mindfactory struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Mindfactory) Name() string  { return "mindfactory" }
func (s *Mindfactory) RateLimit() (int, time.Duration) { return 1, 2 * time.Second }

func (s *Mindfactory) Info() SourceInfo {
	return SourceInfo{
		Name:        "mindfactory",
		Description: "Mindfactory.de (HDD/SSD, EUR) — requiert Byparr (Cloudflare Turnstile)",
		Categories:  []string{"scraping"},
		Requires:    []string{"MINDFACTORY_URLS", "BYPARR_URL"},
		Version:     "1",
	}
}

func (s *Mindfactory) Fetch(ctx context.Context) ([]domain.Deal, error) {
	res := fetchMultiURL(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseMindfactory)
	if err := res.asTransientError(s.Name()); err != nil {
		return nil, err
	}
	return res.deals, nil
}

func parseMindfactory(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("div.pcontent").Each(func(_ int, s *goquery.Selection) {
		nameEl := s.Find("div.pname a")
		title := strings.TrimSpace(nameEl.Text())
		if title == "" {
			return
		}
		href, _ := nameEl.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}

		priceText := strings.TrimSpace(s.Find("span.pprice").Text())
		priceEUR, err := parseFloatClean(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}

		capText := title + " " + strings.TrimSpace(s.Find("div.pspec").Text())
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

		deals = append(deals, domain.Deal{
			Source:          "mindfactory",
			Title:           title,
			URL:             href,
			PriceEUR:        round2(priceEUR),
			PricePerTB:      round2(priceEUR / tb),
			CapacityTB:      round2(tb),
			Condition:       &cond,
			MediaType:       media,
			DriveCategory:   dc,
			Interfaces:      ifaces,
			ObservedAt:      domain.UTCNow(),
		})
	})
	return deals
}
