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
// It lists products from many retailers in a Svelte-rendered table. Each
// product is a <tr class="datatable__row"> with the name in a.product-name,
// the price in a.price (format "€ 241,90"), and specs in dl.product-description
// as dd/dt pairs (e.g. dd="2000GB" dt="Kapazität").
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
		Version:     "2",
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
	// Remove thousands separator (dot) before replacing comma with dot.
	// "1.284,00" → "1284,00" → "1284.00"
	if strings.Contains(s, ",") && strings.Contains(s, ".") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	} else if strings.Contains(s, ",") {
		s = strings.Replace(s, ",", ".", 1)
	}
	return parseFloatClean(s)
}

func parseGeizhals(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find("tr.datatable__row").Each(func(_ int, row *goquery.Selection) {
		nameSel := row.Find("a.product-name")
		title := strings.TrimSpace(nameSel.Text())
		if title == "" {
			return
		}
		href, _ := nameSel.Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}

		priceText := strings.TrimSpace(row.Find("a.price").Text())
		priceEUR, err := parseGeizhalsPrice(priceText)
		if err != nil || priceEUR <= 0 {
			return
		}

		// Extract capacity from the product-description <dl>: look for the
		// pair where <dt> = "Kapazität" and read the <dd> value.
		capText := ""
		row.Find("dl.product-description > div").Each(func(_ int, div *goquery.Selection) {
			label := strings.TrimSpace(div.Find("dt").Text())
			if label == "Kapazität" {
				capText = strings.TrimSpace(div.Find("dd").Text())
			}
		})
		// Fallback: extract from the title if the <dl> was empty.
		if capText == "" {
			capText = title
		}
		tb, err := parsing.ParseCapacityTB(capText)
		if err != nil || tb <= 0 {
			return
		}

		media := normalMedia(title + " " + capText)
		if media == nil {
			// Geizhals titles don't always contain "SSD"/"HDD"; check the
			// specs: "PCIe" or "M.2" indicates SSD, "Drehzahl" (rpm) or
			// "SATA 6Gb/s" with "3.5" indicates HDD.
			specsText := ""
			row.Find("dl.product-description > div").Each(func(_ int, div *goquery.Selection) {
				specsText += " " + strings.TrimSpace(div.Find("dd").Text()) + " " + strings.TrimSpace(div.Find("dt").Text())
			})
			media = normalMedia(title + " " + specsText)
			if media == nil {
				return
			}
		}
		dc := parsing.NormalizeDriveCategory(title, media)
		ifaces := parsing.NormalizeInterfaces(title)
		if len(ifaces) == 0 && dc != nil {
			ifaces = parsing.DefaultInterfacesForCategory(*dc)
		}
		cond := domain.ConditionNew

		deals = append(deals, domain.Deal{
			Source:        "geizhals",
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
