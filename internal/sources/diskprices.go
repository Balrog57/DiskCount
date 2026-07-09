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
		cfg := r.Config()
		return &DiskPrices{
			http:   r.HTTP(),
			byparr: r.Byparr(),
			url:    cfg.DiskPricesURL,
			useFB:  cfg.HeadlessFallback,
		}
	})
}

type DiskPrices struct {
	http   *scraper.HTTPFetcher
	byparr *scraper.ByparrClient
	url    string
	useFB  bool
}

func (s *DiskPrices) Name() string { return "diskprices" }

func (s *DiskPrices) Info() SourceInfo {
	return SourceInfo{
		Name:        "diskprices",
		Description: "Tableau HTML diskprices.com (HDD/SSD agreges)",
		Categories:  []string{"scraping"},
		Requires:    []string{"DISKPRICES_URL"},
		Version:     "1",
	}
}

func (s *DiskPrices) Fetch(ctx context.Context) ([]domain.Deal, error) {
	html, err := s.http.Get(ctx, s.url)
	if err != nil && s.useFB && s.byparr != nil {
		if ses, e2 := s.byparr.GetPage(ctx, s.url); e2 == nil {
			html = ses.HTML
			err = nil
		} else {
			// The byparr fallback also failed: keep the original
			// transient error so the breaker can count it, but log
			// the headless attempt for the admin.
			slog.Warn("diskprices byparr fallback failed", "err", e2)
		}
	}
	if err != nil {
		// Wrap the network failure as a typed transient error so
		// the scanner / health endpoint can distinguish a real
		// upstream outage from a broken selector.
		return nil, Transient(s.Name(), err)
	}
	deals, err := parseDiskPrices(html)
	if err != nil {
		return nil, err
	}
	return deals, nil
}

func parseDiskPrices(html string) ([]domain.Deal, error) {
	if strings.TrimSpace(html) == "" {
		return nil, Selector("diskprices", errEmptyHTML, "page returned empty body — possible WAF block or layout change")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, Transient("diskprices", err)
	}
	// Quick sanity: we expect at least one <tr> with a price cell.
	// If not, the page is probably not a deal table any more.
	rowCount := doc.Find("tr").Length()
	if rowCount == 0 {
		return nil, Selector("diskprices", errEmptyTable, "no <tr> rows found — diskprices.com may have changed its layout")
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
		recording := parsing.NormalizeRecordingMethod(strings.Join([]string{title, tech, texts[5]}, " "), media)
		pricePT := priceEUR / capacityTB
		deals = append(deals, domain.Deal{
			Source: "diskprices", Title: title, URL: href,
			PriceEUR: round2(priceEUR), PricePerTB: round2(pricePT), CapacityTB: round2(capacityTB),
			Condition: cond, MediaType: media,
			ExternalID: parsing.ExtractASIN(href),
			FormFactor: strPtr(texts[5]), Technology: strPtr(tech),
			DriveCategory: dc, Interfaces: ifaces,
			RecordingMethod: recording,
			ObservedAt: domain.UTCNow(),
		})
	})
	slog.Debug("diskprices", "deals", len(deals))
	return deals, nil
}

// Internal sentinel errors so parseDiskPrices can wrap a meaningful
// message without re-importing errors just to call errors.New.
var (
	errEmptyHTML   = stringError("empty HTML body")
	errEmptyTable  = stringError("no <tr> rows in diskprices.com table")
)

type stringError string

func (s stringError) Error() string { return string(s) }
