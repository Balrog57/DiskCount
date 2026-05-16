package domain

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Condition string

const (
	ConditionNew  Condition = "new"
	ConditionUsed Condition = "used"
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

type Deal struct {
	Source        string
	Title         string
	URL           string
	PriceEUR      float64
	PricePerTB    float64
	CapacityTB    float64
	Condition     *Condition
	MediaType     *MediaType
	ExternalID    *string
	FormFactor    *string
	Technology    *string
	DriveCategory *DriveCategory
	Interfaces    []DriveInterface
	ObservedAt    time.Time
	Raw           map[string]interface{}
}

type NotificationDecision struct {
	ShouldNotify       bool
	Reason             string
	DiscountPct        *float64
	BaselinePricePerTB *float64
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
	identity := canonicalURL(d.URL)
	digest := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("%s:url:%x", d.Source, digest[:12])
}
