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
		Description: "LDLC.com (HDD/SSD, EUR)",
		Categories:  []string{"scraping"},
		Requires:    []string{"LDLC_URLS"},
		Version:     "2",
	}
}

func (s *LDLC) Fetch(ctx context.Context) ([]domain.Deal, error) {
	res := fetchMultiURL(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseLDLC)
	if err := res.asTransientError(s.Name()); err != nil {
		return nil, err
	}
	return res.deals, nil
}

// parseLDLCPrice handles the LDLC price format "219€95" where the euro sign
// separates euros from centimes (no decimal comma). Examples:
//   "219€95"    → 219.95
//   "1 199€95"  → 1199.95
//   "59€99"     → 59.99
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
	seen := make(map[string]bool)
	// LDLC search results render products as blocks containing a link to
	// /fiche/PBxxxxxx.html. We select by that link pattern which is stable
	// across their redesigns.
	doc.Find("a[href*='/fiche/PB']").Each(func(_ int, linkEl *goquery.Selection) {
		href, _ := linkEl.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" || !strings.Contains(href, "/fiche/PB") {
			return
		}
		// Strip fragment to deduplicate (image, title, and rating links
		// all point to the same /fiche/PBxxxxx.html with different #fragments).
		href = strings.SplitN(href, "#", 2)[0]
		if seen[href] {
			return
		}
		title := strings.TrimSpace(linkEl.Text())
		// Skip image links (empty text, just an <img>) and rating links
		// ("N avis"). Only the title link has the real product name.
		if title == "" || strings.Contains(title, "avis") {
			return
		}
		seen[href] = true
		// Find the enclosing product block by climbing to the closest
		// container that also holds the price. The exact class varies
		// across LDLC redesigns, so we search up to 5 levels.
		block := linkEl
		priceText := ""
		desc := ""
		for i := 0; i < 5; i++ {
			// Prefer the specific .newprice element, then fall back to
			// generic price selectors.
			priceEl := block.Find(".newprice")
			if priceEl.Length() == 0 {
				priceEl = block.Find("[class*='price-amount'], .price__amount")
			}
			if priceEl.Length() > 0 {
				priceText = strings.TrimSpace(priceEl.First().Text())
				if priceText != "" {
					break
				}
			}
			// Also grab the description if we find it at this level.
			if desc == "" {
				descEl := block.Find("p.desc, p.description, .desc, .description")
				if descEl.Length() > 0 {
					desc = strings.TrimSpace(descEl.First().Text())
				}
			}
			block = block.Parent()
			if block.Length() == 0 {
				break
			}
		}
		priceEUR, err := parseLDLCPrice(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}
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
		deals = append(deals, domain.Deal{
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
		})
	})
	return deals
}