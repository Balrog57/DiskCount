package sources

import (
	"context"
	"log/slog"
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
		if len(cfg.ProshopURLs) == 0 {
			return nil
		}
		// EUR strict: reject .dk URLs (DKK) that would corrupt PricePerTB.
		var urls []string
		for _, u := range cfg.ProshopURLs {
			if strings.Contains(u, "proshop.dk") {
				slog.Warn("proshop: URL .dk ignoree (DKK != EUR)", "url", u)
				continue
			}
			urls = append(urls, u)
		}
		if len(urls) == 0 {
			return nil
		}
		return &Proshop{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   urls,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Proshop struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Proshop) Name() string                    { return "proshop" }
func (s *Proshop) RateLimit() (int, time.Duration) { return 1, 2 * time.Second }

func (s *Proshop) Info() SourceInfo {
	return SourceInfo{
		Name:        "proshop",
		Description: "ProShop (HDD/SSD, EUR) — requiert Byparr (403 sans UA navigateur)",
		Categories:  []string{"scraping"},
		Requires:    []string{"PROSHOP_URLS", "BYPARR_URL"},
		Version:     "1",
	}
}

// Fetch first tries HTTP, then falls back to Byparr (403 without browser UA).
func (s *Proshop) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return fetchWithByparrFallback(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseProshop)
}

func parseProshop(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("div[data-product-id]").Each(func(_ int, s *goquery.Selection) {
		linkEl := s.Find("a.site-product-link")
		title := strings.TrimSpace(linkEl.Text())
		if title == "" {
			return
		}
		href, _ := linkEl.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		priceText := strings.TrimSpace(s.Find("span.site-currency-lg").Text())
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
		deal := domain.Deal{
			Source: "proshop",
			Title:  title, URL: href,
			PriceEUR:   round2(priceEUR),
			PricePerTB: round2(priceEUR / tb),
			CapacityTB: round2(tb),
			Condition:  &cond, MediaType: media,
			DriveCategory: dc, Interfaces: ifaces,
			ObservedAt: domain.UTCNow(),
		}
		deals = append(deals, enrichCardDeal(deal, s, baseURL))
	})
	return deals
}
