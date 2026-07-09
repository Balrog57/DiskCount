package sources

import (
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
)

// RawDeal is the per-source shape that all collectors eventually
// normalise into a domain.Deal. Each source defines its own private
// struct that maps onto RawDeal, then hands it to Normalize() so the
// downstream pipeline (matcher, notifier, database) only deals with
// one canonical model.
//
// The fields are pointers because every source fills a different
// subset: pricepergig gives us a brand, a model and an ASIN;
// diskprices gives us a form factor and a technology; RSS feeds give
// us a title and a URL. Normalize does its best to fill the gaps.
type RawDeal struct {
	Source        string
	Title         string
	URL           string
	PriceEUR      *float64
	Currency      string // defaults to EUR
	CapacityTB    *float64
	CapacityGB    *float64 // alternative when the source reports GB
	Condition     string   // "new" / "used" / ""
	MediaHint     string   // "SSD" / "HDD" / "NVMe" …
	FormFactor    string
	Technology    string
	Category      string
	Interface     string
	Brand         string
	Model         string
	Merchant      string
	ExternalID    string
	RecordingHint string // "CMR" / "SMR" / "NAS" …
	RawTitle      string
}

// Normalize turns a RawDeal into a validated domain.Deal. The
// returned deal has the ProductID computed (Source + canonicalised
// title or external ID), the recording method inferred from the
// model family, the media type from the hint, and the drive category
// and interfaces from the available text fields. The source name is
// propagated unchanged so the matcher can filter on it.
//
// The function never panics on missing fields; partial input yields
// a deal with empty optional fields, and validation failures are
// returned as a *Rejection that the scanner can record for quality
// stats.
func Normalize(raw RawDeal) (domain.Deal, *Rejection) {
	if strings.TrimSpace(raw.Title) == "" {
		return domain.Deal{}, &Rejection{Reason: "missing_title", Detail: "raw deal had no title"}
	}
	if strings.TrimSpace(raw.URL) == "" {
		return domain.Deal{}, &Rejection{Reason: "missing_url", Detail: "raw deal had no URL: " + raw.Title}
	}

	priceEUR := 0.0
	if raw.PriceEUR != nil {
		priceEUR = *raw.PriceEUR
	}
	if priceEUR <= 0 {
		return domain.Deal{}, &Rejection{Reason: "missing_price", Detail: raw.Title}
	}

	capacityTB := 0.0
	switch {
	case raw.CapacityTB != nil:
		capacityTB = *raw.CapacityTB
	case raw.CapacityGB != nil:
		capacityTB = *raw.CapacityGB / 1000.0
	}
	if capacityTB <= 0 {
		return domain.Deal{}, &Rejection{Reason: "missing_capacity", Detail: raw.Title}
	}

	// Build a text blob for the categorical classifiers; cheaper than
	// threading every field separately and keeps the per-source code
	// in charge of what to feed in.
	classText := strings.Join([]string{
		raw.Title, raw.MediaHint, raw.FormFactor, raw.Technology,
		raw.Category, raw.Interface, raw.RecordingHint,
	}, " ")
	media := classifyMedia(classText)
	dc := parsing.NormalizeDriveCategory(classText, media)
	ifaces := parsing.NormalizeInterfaces(classText)
	if len(ifaces) == 0 && dc != nil {
		ifaces = defaultInterfacesForCategory(*dc)
	}

	cond := parsing.NormalizeCondition(raw.Condition)
	if cond == nil {
		c := domain.ConditionNew
		cond = &c
	}

	rec := parsing.NormalizeRecordingMethod(classText, media)
	brand := strings.TrimSpace(raw.Brand)
	model := strings.TrimSpace(raw.Model)
	formFactor := strings.TrimSpace(raw.FormFactor)
	technology := strings.TrimSpace(raw.Technology)
	merchant := strings.TrimSpace(raw.Merchant)
	externalID := strings.TrimSpace(raw.ExternalID)
	rawTitle := raw.RawTitle
	if rawTitle == "" {
		rawTitle = raw.Title
	}

	pt := priceEUR / capacityTB
	return domain.Deal{
		Source:        raw.Source,
		Title:         strings.TrimSpace(raw.Title),
		URL:           raw.URL,
		PriceEUR:      round2(priceEUR),
		PricePerTB:    round2(pt),
		CapacityTB:    round2(capacityTB),
		Condition:     cond,
		MediaType:     media,
		FormFactor:    strPtrOrNil(formFactor),
		Technology:    strPtrOrNil(technology),
		DriveCategory: dc,
		Interfaces:    ifaces,
		ExternalID:    strPtrOrNil(externalID),
		Merchant:      strPtrOrNil(merchant),
		Brand:         strPtrOrNil(brand),
		Model:         strPtrOrNil(model),
		RawTitle:      rawTitle,
		RecordingMethod: rec,
		ObservedAt:    domain.UTCNow(),
	}, nil
}

// Rejection describes why a raw deal was dropped before it ever
// reached the matcher. The scanner feeds it into the quality stats
// so the admin can see at a glance which fields are systematically
// missing.
type Rejection struct {
	Reason string
	Detail string
}

// classifyMedia decides between HDD and SSD using the text blob
// the source produced. Kept in this package (not parsing) because
// every source ends up calling it through Normalize, and we want a
// single implementation that is easy to tweak centrally.
func classifyMedia(text string) *domain.MediaType {
	t := strings.ToLower(text)
	switch {
	case containsAny(t, "ssd", "nvme", "solid state", "m.2"):
		m := domain.MediaTypeSolidState
		return &m
	case containsAny(t, "hdd", "disque dur", "hard drive", "rpm", "5400", "7200"):
		m := domain.MediaTypeRotational
		return &m
	}
	return nil
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func defaultInterfacesForCategory(dc domain.DriveCategory) []domain.DriveInterface {
	switch dc {
	case domain.DriveCategoryInternal3_5, domain.DriveCategoryInternal2_5,
		domain.DriveCategoryInternalHybrid, domain.DriveCategoryInternalSSD,
		domain.DriveCategoryM2SATA:
		return []domain.DriveInterface{domain.DriveInterfaceSATA}
	case domain.DriveCategoryExternal3_5, domain.DriveCategoryExternal2_5,
		domain.DriveCategoryExternalSSD:
		return []domain.DriveInterface{domain.DriveInterfaceUSB}
	case domain.DriveCategoryM2NVMe, domain.DriveCategoryU2U3:
		return []domain.DriveInterface{domain.DriveInterfaceNVMe}
	case domain.DriveCategoryInternalSAS:
		return []domain.DriveInterface{domain.DriveInterfaceSAS}
	}
	return nil
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
