package sources

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
	"github.com/PuerkitoBio/goquery"
)

func init() {
	Register(func(r *Registry) Source {
		return &DiskPrices{http: r.HTTP(), url: r.Config().DiskPricesURL}
	})
}

type DiskPrices struct {
	http *scraper.HTTPFetcher
	url  string
}

func (s *DiskPrices) Name() string { return "diskprices" }

func (s *DiskPrices) Fetch(ctx context.Context) ([]domain.Deal, error) {
	html, err := s.http.Get(ctx, s.url)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	var deals []domain.Deal
	doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
		cells := row.Find("td,th")
		if cells.Length() < 9 {
			return
		}
		texts := make([]string, cells.Length())
		cells.Each(func(i int, c *goquery.Selection) { texts[i] = strings.TrimSpace(c.Text()) })
		if strings.Contains(strings.ToLower(texts[0]), "price") {
			return
		}

		priceEUR, err := parsing.ParsePriceEUR(texts[2])
		if err != nil || priceEUR <= 0 {
			return
		}
		capacityTB, err := parsing.ParseCapacityTB(texts[3])
		if err != nil || capacityTB <= 0 {
			return
		}

		tech := texts[6]
		media := normalMedia(tech + " " + texts[3])
		if media == nil {
			return
		}

		link := cells.Eq(cells.Length() - 1).Find("a")
		href, _ := link.Attr("href")
		title := strings.TrimSpace(link.Text())
		if title == "" {
			title = texts[cells.Length()-1]
		}

		categoryText := strings.Join([]string{texts[5], tech, title}, " ")
		dc := parsing.NormalizeDriveCategory(categoryText, media)
		ifaces := parsing.NormalizeInterfaces(categoryText)
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

		cond := parsing.NormalizeCondition(texts[7])
		if cond == nil {
			c := domain.ConditionNew
			cond = &c
		}
		pricePT := priceEUR / capacityTB
		deals = append(deals, domain.Deal{
			Source: "diskprices", Title: title, URL: href,
			PriceEUR: round2(priceEUR), PricePerTB: round2(pricePT), CapacityTB: round2(capacityTB),
			Condition: cond, MediaType: media,
			ExternalID: parsing.ExtractASIN(href),
			FormFactor: strPtr(texts[5]), Technology: strPtr(tech),
			DriveCategory: dc, Interfaces: ifaces,
			ObservedAt: domain.UTCNow(),
		})
	})
	slog.Debug("diskprices", "deals", len(deals))
	return deals, nil
}
