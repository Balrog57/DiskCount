package domain

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Condition string

const (
	ConditionNew  Condition = "new"
	ConditionUsed Condition = "used"
)

type Availability string

const (
	AvailabilityAvailable   Availability = "available"
	AvailabilityUnavailable Availability = "unavailable"
)

type MediaType string

const (
	MediaTypeRotational MediaType = "rotational"
	MediaTypeSolidState MediaType = "solid_state"
)

type DriveCategory string

const (
	DriveCategoryExternal3_5    DriveCategory = "external_3_5"
	DriveCategoryExternal2_5    DriveCategory = "external_2_5"
	DriveCategoryInternal3_5    DriveCategory = "internal_3_5"
	DriveCategoryInternal2_5    DriveCategory = "internal_2_5"
	DriveCategoryInternalHybrid DriveCategory = "internal_hybrid"
	DriveCategoryInternalSAS    DriveCategory = "internal_sas"
	DriveCategoryExternalSSD    DriveCategory = "external_ssd"
	DriveCategoryInternalSSD    DriveCategory = "internal_ssd"
	DriveCategoryM2SATA         DriveCategory = "m2_sata"
	DriveCategoryM2NVMe         DriveCategory = "m2_nvme"
	DriveCategoryU2U3           DriveCategory = "u2_u3"
)

type DriveInterface string

const (
	DriveInterfaceSATA DriveInterface = "sata"
	DriveInterfaceSAS  DriveInterface = "sas"
	DriveInterfaceNVMe DriveInterface = "nvme"
	DriveInterfaceUSB  DriveInterface = "usb"
)

// RecordingMethod describes the magnetic recording technology of a rotational
// drive. It matters for NAS/server use: CMR (Conventional Magnetic Recording)
// handles sustained random writes well, while SMR (Shingled Magnetic Recording)
// can suffer severe performance degradation in such workloads. Users who build
// NAS arrays often want to filter out SMR drives entirely.
type RecordingMethod string

const (
	// RecordingMethodCMR is conventional (perpendicular) recording — the safe
	// choice for RAID/NAS/ZFS.
	RecordingMethodCMR RecordingMethod = "cmr"
	// RecordingMethodSMR is shingled recording (drive-managed unless noted).
	RecordingMethodSMR RecordingMethod = "smr"
)

type Deal struct {
	Source               string
	Title                string
	URL                  string
	CanonicalURL         string
	PriceEUR             float64
	PricePerTB           float64
	CapacityTB           float64
	Condition            *Condition
	MediaType            *MediaType
	ExternalID           *string
	FormFactor           *string
	Technology           *string
	DriveCategory        *DriveCategory
	RecordingMethod      *RecordingMethod
	Interfaces           []DriveInterface
	ObservedAt           time.Time
	Raw                  map[string]interface{}
	QualityScore         int
	ClassificationSource string
	Merchant             *string
	Brand                *string
	Model                *string
	RawTitle             string
}

type NotificationDecision struct {
	ShouldNotify       bool
	Reason             string
	DiscountPct        *float64
	BaselinePricePerTB *float64
	// BackInStockHours is set when this notification was triggered
	// because the product returned after an absence longer than the
	// configured threshold. Zero means "normal price drop" notification.
	BackInStockHours float64
}

func UTCNow() time.Time {
	return time.Now().UTC()
}

func canonicalURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := parsed.Query()
	for key := range q {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "tag") || strings.HasPrefix(lower, "utm_") || strings.HasPrefix(lower, "ascsubtag") {
			q.Del(key)
		}
	}
	parsed.RawQuery = q.Encode()
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String()
}

func (d Deal) ProductID() string {
	if d.ExternalID != nil && *d.ExternalID != "" {
		return strings.ToLower(fmt.Sprintf("%s:%s", d.Source, *d.ExternalID))
	}
	identity := d.CanonicalURL
	if identity == "" {
		identity = canonicalURL(d.URL)
	}
	if identity == "" {
		identity = strings.ToLower(fmt.Sprintf("%s:%s:%.3f:%.2f", d.Source, strings.TrimSpace(d.Title), d.CapacityTB, d.PriceEUR))
	}
	digest := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("%s:url:%x", d.Source, digest[:12])
}

// CanonicalProductKey identifies equivalent offers only when the three stable
// product attributes are known. An empty key is intentional: title text is
// too seller-specific to be a safe grouping fallback.
func (d Deal) CanonicalProductKey() string {
	return CanonicalProductKey(d.Brand, d.Model, d.CapacityTB)
}

func CanonicalProductKey(brand, model *string, capacityTB float64) string {
	if brand == nil || model == nil || capacityTB <= 0 {
		return ""
	}
	b, m := canonicalPart(*brand), canonicalPart(*model)
	if b == "wd" || b == "westerndigital" {
		b = "westerndigital"
	}
	if b == "" || m == "" {
		return ""
	}
	return b + "|" + m + "|" + strconv.FormatFloat(capacityTB, 'f', 3, 64)
}

func canonicalPart(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func CanonicalURL(rawURL string) string { return canonicalURL(rawURL) }
