package sources

import (
	"context"
	"strconv"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
	"github.com/PuerkitoBio/goquery"
)

func init() {
	Register(func(r *Registry) Source {
		cfg := r.Config()
		if len(cfg.LDLCURLs) == 0 {
			return nil
		}
		return &LDLC{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.LDLCURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type LDLC struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *LDLC) Name() string { return "ldlc" }

func (s *LDLC) Info() SourceInfo {
	return SourceInfo{
		Name:        "ldlc",
		Description: "LDLC.com (HDD/SSD, EUR) — SPA, Byparr recommande",
		Categories:  []string{"scraping"},
		Requires:    []string{"LDLC_URLS", "BYPARR_URL"},
		Version:     "2",
	}
}

// Fetch first tries HTTP, then falls back to Byparr if the page returns no
// products (LDLC search results are injected via JS).
func (s *LDLC) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return fetchWithByparrFallback(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseLDLC)
}

// parseLDLCPrice handles the LDLC price format "219€95" where the euro sign
// separates euros from centimes (no decimal comma). Examples:
//
//	"219€95"    → 219.95
//	"1 199€95"  → 1199.95
//	"59€99"     → 59.99
//
// Falls back to parseFloatClean for standard formats.
func parseLDLCPrice(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\u00a0", "")
	s = strings.ReplaceAll(s, " ", "")
	// Split on the euro sign: "219€95" → ["219", "95"]
	if idx := strings.Index(s, "€"); idx > 0 {
		euros := s[:idx]
		cents := s[idx+len("€"):]
		// If cents is exactly 2 digits, it's the centimes format.
		if len(cents) == 2 {
			ev, err := strconv.ParseFloat(euros, 64)
			if err != nil {
				return 0, err
			}
			cv, err := strconv.ParseFloat(cents, 64)
			if err != nil {
				return 0, err
			}
			return ev + cv/100, nil
		}
		// If no cents after €, just parse the euros part.
		if cents == "" {
			return strconv.ParseFloat(euros, 64)
		}
	}
	// Fallback to the standard parser for "€ 129,90" or "129.90" formats.
	return parseFloatClean(s)
}

func parseLDLC(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	// LDLC search results render each product as li.pdt-item containing
	// a /fiche/PBxxxxx.html link and a price span.
	doc.Find("li.pdt-item").Each(func(_ int, s *goquery.Selection) {
		// Find the product link — there may be several /fiche/PB links
		// (image, title, rating). Pick the first one with a title.
		var href, title string
		s.Find("a[href*='/fiche/PB']").Each(func(_ int, linkEl *goquery.Selection) {
			if title != "" {
				return // already found a valid link
			}
			t := strings.TrimSpace(linkEl.Text())
			if t == "" || strings.Contains(t, "avis") {
				return
			}
			h, _ := linkEl.Attr("href")
			h = absolutizeURL(baseURL, strings.TrimSpace(h))
			if h != "" && strings.Contains(h, "/fiche/PB") {
				href = h
				title = t
			}
		})
		if href == "" || title == "" {
			return
		}
		href = strings.SplitN(href, "#", 2)[0]
		// LDLC prices: the price element contains text like "219€95"
		// (euros + centimes joined by the € sign) or "219€" (no centimes).
		priceText := ""
		s.Find("[class*='price']").Each(func(_ int, el *goquery.Selection) {
			t := strings.TrimSpace(el.Text())
			if t != "" && strings.Contains(t, "€") {
				priceText = t
			}
		})
		priceEUR, err := parseLDLCPrice(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}
		desc := strings.TrimSpace(s.Find("p.desc, .desc, .description").First().Text())
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
		deal := domain.Deal{
			Source:        "ldlc",
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
