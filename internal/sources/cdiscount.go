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
		if len(cfg.CdiscountURLs) == 0 {
			return nil
		}
		return &Cdiscount{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.CdiscountURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Cdiscount struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Cdiscount) Name() string { return "cdiscount" }

func (s *Cdiscount) Info() SourceInfo {
	return SourceInfo{
		Name:        "cdiscount",
		Description: "Cdiscount (HDD/SSD, EUR) — requiert Byparr (anti-bot Baleen JS)",
		Categories:  []string{"scraping"},
		Requires:    []string{"CDISCOUNT_URLS", "BYPARR_URL"},
		Version:     "1",
	}
}

// Fetch first tries HTTP, then falls back to Byparr (Baleen JS anti-bot).
func (s *Cdiscount) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return fetchWithByparrFallback(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseCdiscount)
}

func parseCdiscount(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("li.prdtBloc").Each(func(_ int, s *goquery.Selection) {
		linkEl := s.Find("a.prdtBloc-link")
		title := strings.TrimSpace(linkEl.Text())
		if title == "" {
			return
		}
		href, _ := linkEl.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		priceText := strings.TrimSpace(s.Find("span.price").Text())
		priceEUR, err := parseFloatClean(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}
		tb, err := parsing.ParseCapacityTB(title)
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
			Source:        "cdiscount",
			Title:         title, URL: href,
			PriceEUR:      round2(priceEUR),
			PricePerTB:    round2(priceEUR / tb),
			CapacityTB:    round2(tb),
			Condition:     &cond, MediaType: media,
			DriveCategory: dc, Interfaces: ifaces,
			ObservedAt:    domain.UTCNow(),
		})
	})
	return deals
}
