package normalize

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
)

const (
	MinCapacityTB      = 0.08
	MaxCapacityTB      = 200.0
	MinPriceEUR        = 1.0
	MaxPriceEUR        = 100000.0
	MaxPricePerTB      = 5000.0
	MinQualityForAlert = 70
)

var packRE = regexp.MustCompile(`(?i)(?:lot\s+de|pack\s+of|pack\s+de)\s*(\d{1,2})`)

type Result struct {
	Deal   domain.Deal
	Reject *Reject
}

type Reject struct {
	Reason string
	Detail string
}

func Deal(raw domain.Deal) Result {
	raw.RawTitle = first(raw.RawTitle, raw.Title)
	raw.Title = cleanTitle(raw.Title)
	raw.URL = strings.TrimSpace(raw.URL)
	raw.Source = strings.TrimSpace(raw.Source)

	if raw.Source == "" {
		return rejected(raw, "missing_source", "source is empty")
	}
	if raw.Title == "" {
		return rejected(raw, "missing_title", "title is empty")
	}
	if raw.URL == "" {
		return rejected(raw, "missing_url", "url is empty")
	}
	parsed, err := url.Parse(raw.URL)
	if err != nil {
		return rejected(raw, "invalid_url", err.Error())
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return rejected(raw, "invalid_url", "missing scheme or host")
	}
	if raw.CapacityTB < MinCapacityTB || raw.CapacityTB > MaxCapacityTB {
		return rejected(raw, "invalid_capacity", fmt.Sprintf("%.3f TB", raw.CapacityTB))
	}
	if raw.PriceEUR < MinPriceEUR || raw.PriceEUR > MaxPriceEUR {
		return rejected(raw, "invalid_price", fmt.Sprintf("%.2f EUR", raw.PriceEUR))
	}
	if multiplier := packMultiplier(raw.Title); multiplier > 1 {
		raw.CapacityTB *= float64(multiplier)
		raw.PricePerTB = raw.PriceEUR / raw.CapacityTB
	}
	if raw.PricePerTB <= 0 {
		raw.PricePerTB = raw.PriceEUR / raw.CapacityTB
	}
	if raw.PricePerTB <= 0 || raw.PricePerTB > MaxPricePerTB {
		return rejected(raw, "invalid_price_per_tb", fmt.Sprintf("%.2f EUR/TB", raw.PricePerTB))
	}

	raw.CanonicalURL = domain.CanonicalURL(raw.URL)
	raw = enrich(raw)
	if unsupportedProduct(raw.Title) {
		return rejected(raw, "unsupported_product", "item is not a standalone HDD or SSD")
	}
	if raw.MediaType == nil {
		return rejected(raw, "unknown_media", "media type could not be classified as HDD or SSD")
	}
	raw.QualityScore = qualityScore(raw)
	if raw.QualityScore < 50 {
		return rejected(raw, "low_quality", fmt.Sprintf("score=%d", raw.QualityScore))
	}
	return Result{Deal: raw}
}

func IsAlertQuality(d domain.Deal) bool {
	return d.QualityScore >= MinQualityForAlert
}

func enrich(d domain.Deal) domain.Deal {
	classText := strings.Join([]string{d.Title, str(d.FormFactor), str(d.Technology)}, " ")
	if d.MediaType == nil {
		d.MediaType = parsing.NormalizeMediaType(classText)
	}
	if d.DriveCategory == nil {
		d.DriveCategory = parsing.NormalizeDriveCategory(classText, d.MediaType)
	}
	if len(d.Interfaces) == 0 {
		d.Interfaces = parsing.NormalizeInterfaces(classText)
	}
	if len(d.Interfaces) == 0 && d.DriveCategory != nil {
		d.Interfaces = parsing.DefaultInterfacesForCategory(*d.DriveCategory)
	}
	if d.RecordingMethod == nil {
		d.RecordingMethod = parsing.NormalizeRecordingMethod(classText, d.MediaType)
	}
	if d.ClassificationSource == "" {
		if d.MediaType != nil || d.DriveCategory != nil || len(d.Interfaces) > 0 {
			d.ClassificationSource = "heuristic"
		} else {
			d.ClassificationSource = "unknown"
		}
	}
	if d.Merchant == nil {
		if host := host(d.URL); host != "" {
			d.Merchant = &host
		}
	}
	if d.Brand == nil {
		if b := inferBrand(d.Title); b != "" {
			d.Brand = &b
		}
	}
	return d
}

func qualityScore(d domain.Deal) int {
	score := 0
	if d.Source != "" {
		score += 10
	}
	if d.Title != "" {
		score += 20
	}
	if d.URL != "" && d.CanonicalURL != "" {
		score += 15
	}
	if d.ExternalID != nil && *d.ExternalID != "" {
		score += 10
	}
	if d.CapacityTB >= MinCapacityTB && d.CapacityTB <= MaxCapacityTB {
		score += 15
	}
	if d.PriceEUR >= MinPriceEUR && d.PriceEUR <= MaxPriceEUR && d.PricePerTB > 0 && d.PricePerTB <= MaxPricePerTB {
		score += 15
	}
	if d.MediaType != nil {
		score += 10
	}
	if d.DriveCategory != nil {
		score += 10
	}
	if len(d.Interfaces) > 0 {
		score += 5
	}
	if d.RecordingMethod != nil {
		score += 5
	}
	if score > 100 {
		score = 100
	}
	return score
}

func rejected(d domain.Deal, reason, detail string) Result {
	return Result{Deal: d, Reject: &Reject{Reason: reason, Detail: detail}}
}

func cleanTitle(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	s = strings.Trim(s, "-|")
	return strings.TrimSpace(s)
}

func host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// knownBrands is the canonical list of brands we recognise. It is matched
// case-insensitively against the full title so brands appear even when the
// title does not start with them (e.g. "2 To Seagate IronWolf" → "Seagate").
// The list mirrors the brand picker exposed in the Telegram bot so an alert
// configured by brand matches the same spelling the normaliser produces.
var knownBrands = []string{
	"Seagate", "Western Digital", "WD", "Toshiba", "Samsung",
	"Crucial", "Kingston", "HGST", "Micron", "SanDisk", "Lexar",
	"ADATA", "Corsair", "PNY", "Intel", "Kioxia", "LaCie",
	"Maxtor", "Fujitsu", "G-Technology", "OWC",
}

// inferBrand returns the brand of a drive by scanning the title for any
// known brand, case-insensitively. The previous implementation took the
// first whitespace token of the title, which mis-classified titles that
// begin with a capacity (e.g. "2 To Seagate ..." → brand "2") and so both
// polluted the products.brand column and silently broke brand-based alert
// filtering. Matching the whole title against a curated list is far more
// accurate and avoids storing junk brands for unrecognised titles.
func inferBrand(title string) string {
	if title == "" {
		return ""
	}
	lower := strings.ToLower(title)
	// Check "Western Digital" before "WD" — longer match wins so we don't
	// label a "Western Digital Red" drive as "WD".
	for _, b := range knownBrands {
		if strings.Contains(lower, strings.ToLower(b)) {
			return b
		}
	}
	return ""
}

// unsupportedTerms lists the substrings that mark a listing as NOT a
// standalone HDD/SSD (RAM, laptops, GPUs, flash cards, USB sticks, ...).
//
// ⚡ Bolt optimization: hoisted to a package-level var so normalize.Deal
// does not allocate a fresh ~30-element slice on every call. With ~200
// deals per scan this avoids ~6000 short-lived slice headers per scan,
// which is the dominant allocation in the normalize hot path. All terms
// are lowercase; unsupportedProduct lowercases the title before matching.
var unsupportedTerms = []string{
	"ddr", "sodimm", "so-dimm", "udimm", "mémoire ram", "memoire ram", "ram ",
	"ordinateur portable", "pc portable", "notebook", "laptop", "mini pc", "desktop pc",
	"intel core", "ryzen", "rtx", "geforce", "radeon", "clavier",
	"compactflash", "cfexpress", "cf card", "carte cf", "carte cfe", "sd card",
	"carte mémoire", "carte memoire", "usb flash drive", "clé usb", "cle usb",
}

func unsupportedProduct(title string) bool {
	t := strings.ToLower(title)
	for _, term := range unsupportedTerms {
		if strings.Contains(t, term) {
			return true
		}
	}
	return false
}

func packMultiplier(title string) int {
	match := packRE.FindStringSubmatch(title)
	if match == nil {
		return 1
	}
	n, err := strconv.Atoi(match[1])
	if err != nil || n < 2 || n > 24 {
		return 1
	}
	return n
}

func first(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
