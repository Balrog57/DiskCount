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
		if len(cfg.GrosbillURLs) == 0 {
			return nil
		}
		return &Grosbill{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			urls:   cfg.GrosbillURLs,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type Grosbill struct {
	http   scraper.Fetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *Grosbill) Name() string { return "grosbill" }

func (s *Grosbill) Info() SourceInfo {
	return SourceInfo{
		Name:        "grosbill",
		Description: "Grosbill (HDD/SSD, EUR)",
		Categories:  []string{"scraping"},
		Requires:    []string{"GROSBILL_URLS"},
		Version:     "2",
	}
}

func (s *Grosbill) Fetch(ctx context.Context) ([]domain.Deal, error) {
	res := fetchMultiURL(ctx, s.Name(), s.http, s.byparr, s.urls, s.useFB, parseGrosbill)
	if err := res.asTransientError(s.Name()); err != nil {
		return nil, err
	}
	return res.deals, nil
}

// parseGrosbill scrapes Grosbill listing pages. Verified against the real HTML:
// products are div.grb__liste-produit__liste__produit with the title in
// .grb__liste-produit__liste__produit__information__libelle, the price in
// span.content_prix_produit (format "139,99", comma decimal, no currency
// symbol — more reliable than the displayed "139€99" which has broken HTML),
// and the capacity in ul.caracteristiques__liste li containing "Capacité".
func parseGrosbill(html, baseURL string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var deals []domain.Deal
	doc.Find(".grb__liste-produit__liste__produit").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Find("a.prod_txt_left, a.grb__liste-produit__liste__produit__link").First().Attr("href")
		href = absolutizeURL(baseURL, strings.TrimSpace(href))
		if href == "" {
			return
		}
		// Title = constructeur + libellé produit
		constructeur := strings.TrimSpace(s.Find(".grb__liste-produit__liste__produit__information__constructeur").Text())
		libelle := strings.TrimSpace(s.Find(".grb__liste-produit__liste__produit__information__libelle__libelle_produit").Text())
		title := strings.TrimSpace(constructeur + " " + libelle)
		if title == "" {
			return
		}
		// Price: prefer the machine-friendly content_prix_produit ("139,99")
		// over the displayed price which has broken HTML.
		priceText := strings.TrimSpace(s.Find("span.grb__liste-produit__liste__produit__reference-container__content_prix_produit").Text())
		var priceEUR float64
		var perr error
		if priceText != "" {
			// Grosbill uses comma decimal: "139,99" → "139.99"
			priceText = strings.ReplaceAll(priceText, ",", ".")
			priceEUR, perr = strconv.ParseFloat(priceText, 64)
		} else {
			// Fallback: parse the displayed price "139€99" via LDLC-style parser
			displayedPrice := strings.TrimSpace(s.Find("div.grb__liste-produit__liste__produit__achat__prix span").First().Text())
			priceEUR, perr = parseLDLCPrice(displayedPrice)
		}
		if perr != nil || priceEUR <= 0 {
			return
		}
		// Capacity from caracteristiques list
		capText := ""
		s.Find("ul.grb__liste-produit__liste__produit__information__caracteristiques__liste li").Each(func(_ int, li *goquery.Selection) {
			t := strings.TrimSpace(li.Text())
			if strings.Contains(t, "Capacité") {
				capText = t
			}
		})
		if capText == "" {
			capText = title
		}
		tb, err := parsing.ParseCapacityTB(capText)
		if err != nil || tb <= 0 {
			return
		}
		media := normalMedia(title + " " + capText)
		if media == nil {
			// Grosbill exposes the product category in a hidden span; use it
			// as a fallback to classify SSD vs HDD when the title/specs don't
			// contain a recognisable keyword (e.g. "Samsung 870 EVO SATA").
			catText := strings.TrimSpace(s.Find("span.cyb__liste-produit__title-cat").Text())
			if catText != "" {
				media = normalMedia(catText)
			}
			if media == nil {
				return
			}
		}
		classText := title + " " + capText
		dc := parsing.NormalizeDriveCategory(classText, media)
		ifaces := parsing.NormalizeInterfaces(classText)
		if len(ifaces) == 0 && dc != nil {
			ifaces = parsing.DefaultInterfacesForCategory(*dc)
		}
		cond := domain.ConditionNew
		deal := domain.Deal{
			Source:        "grosbill",
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
