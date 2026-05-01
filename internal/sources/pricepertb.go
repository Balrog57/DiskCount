package sources

import (
	"context"
	"log/slog"
	"strings"

	"github.com/MarcPartensky/DiskCount/internal/domain"
	"github.com/MarcPartensky/DiskCount/internal/parsing"
	"github.com/MarcPartensky/DiskCount/internal/scraper"
	"github.com/PuerkitoBio/goquery"
)

func init() {
	Register(func(r *Registry) Source {
		cfg := r.Config()
		if len(cfg.PricePerTBURLs) == 0 { return nil }
		return &PricePerTB{http: r.HTTP(), byparr: r.Byparr(), urls: cfg.PricePerTBURLs, useFB: cfg.HeadlessFallback}
	})
}

type PricePerTB struct {
	http   *scraper.HTTPFetcher
	byparr *scraper.ByparrClient
	urls   []string
	useFB  bool
}

func (s *PricePerTB) Name() string { return "pricepertb" }

func (s *PricePerTB) Fetch(ctx context.Context) ([]domain.Deal, error) {
	var all []domain.Deal
	for _, u := range s.urls {
		html, err := s.http.Get(ctx, u)
		if err != nil && s.useFB && s.byparr != nil {
			if ses, e2 := s.byparr.GetPage(ctx, u); e2 == nil { html = ses.HTML; err = nil }
		}
		if err != nil { slog.Warn("pricepertb", "url", u, "err", err); continue }
		all = append(all, parsePTB(html)...)
	}
	slog.Debug("pricepertb", "deals", len(all))
	return all, nil
}

func parsePTB(html string) []domain.Deal {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil { return nil }
	var deals []domain.Deal
	doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
		cells := row.Find("td")
		if cells.Length() < 3 { return }
		texts := make([]string, cells.Length())
		cells.Each(func(i int, c *goquery.Selection) { texts[i] = strings.TrimSpace(c.Text()) })
		if strings.Contains(strings.ToLower(texts[0]), "price") { return }

		priceEUR, err := parseFloatClean(texts[0])
		if err != nil || priceEUR <= 0 { return }
		capText := texts[1]
		var tb float64
		if parts := strings.Fields(capText); len(parts) > 0 {
			if v, e := parseFloatClean(parts[0]); e == nil && v > 0 {
				tb = v
				if len(parts) > 1 && strings.EqualFold(parts[1], "Go") { tb /= 1000 }
			}
		}
		if tb <= 0 { return }
		link := cells.Eq(2).Find("a")
		href, _ := link.Attr("href")
		title := strings.TrimSpace(link.Text())

		media := normalMedia(title)
		dc := parsing.NormalizeDriveCategory(title, media)
		ifaces := parsing.NormalizeInterfaces(title)
		if len(ifaces) == 0 && dc != nil {
			switch *dc {
			case domain.DriveCategoryInternal3_5, domain.DriveCategoryInternal2_5, domain.DriveCategoryInternalHybrid, domain.DriveCategoryInternalSSD, domain.DriveCategoryM2SATA:
				ifaces = append(ifaces, domain.DriveInterfaceSATA)
			case domain.DriveCategoryExternal3_5, domain.DriveCategoryExternal2_5, domain.DriveCategoryExternalSSD:
				ifaces = append(ifaces, domain.DriveInterfaceUSB)
			case domain.DriveCategoryM2NVMe, domain.DriveCategoryU2U3:
				ifaces = append(ifaces, domain.DriveInterfaceNVMe)
			case domain.DriveCategoryInternalSAS:
				ifaces = append(ifaces, domain.DriveInterfaceSAS)
			}
		}

		cond := domain.ConditionNew; pt := priceEUR / tb
		deals = append(deals, domain.Deal{
			Source: "pricepertb", Title: title, URL: href,
			PriceEUR: round2(priceEUR), PricePerTB: round2(pt), CapacityTB: round2(tb),
			Condition: &cond, MediaType: media,
			DriveCategory: dc, Interfaces: ifaces,
			ObservedAt: domain.UTCNow(),
		})
	})
	return deals
}
