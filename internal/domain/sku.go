package domain

import (
	"regexp"
	"strings"
	"unicode"
)

var eanDigitsRE = regexp.MustCompile(`^\d{8,14}$`)

// partNumberRE matches manufacturer part numbers used for cross-merchant grouping.
var partNumberRE = regexp.MustCompile(`(?i)\b(?:ST\d{3,}[A-Z0-9-]{2,}|WD[A-Z0-9][A-Z0-9-]{3,}|WDS[A-Z0-9-]{4,}|CT\d{3,}[A-Z0-9-]{3,}|MZ[A-Z0-9-]{4,}|SK[A-Z0-9-]{4,}|HUS\d{3,}[A-Z0-9-]*|MG\d{3,}[A-Z0-9-]*|SSDSC[A-Z0-9-]{3,}|D3-S\d{4,}[A-Z0-9-]*|900[A-Z0-9-]{4,})\b`)

var asinRE = regexp.MustCompile(`^[a-z0-9]{10}$`)

// NormalizeEAN returns a canonical GTIN/EAN (digits only, 8–14 chars).
func NormalizeEAN(ean *string) string {
	if ean == nil {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.TrimSpace(*ean) {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) < 8 || len(s) > 14 || !eanDigitsRE.MatchString(s) {
		return ""
	}
	return s
}

// HasProductIdentifier reports whether at least one stable product id is known.
func HasProductIdentifier(ean, sku *string) bool {
	return NormalizeEAN(ean) != "" || NormalizeProductSKU(sku) != ""
}

// NormalizeProductSKU returns a lowercase alphanumeric token for merchant or MPN sku values.
func NormalizeProductSKU(sku *string) string {
	if sku == nil {
		return ""
	}
	s := strings.TrimSpace(*sku)
	if s == "" || s == "-" {
		return ""
	}
	return canonicalPart(s)
}

// CanonicalProductKey groups offers by EAN (preferred) or manufacturer/merchant SKU.
func CanonicalProductKey(ean, sku *string) string {
	if e := NormalizeEAN(ean); e != "" {
		return "ean:" + e
	}
	if sku == nil {
		return ""
	}
	raw := strings.TrimSpace(*sku)
	if raw == "" {
		return ""
	}
	if IsManufacturerPartNumber(raw) {
		return "mpn:" + NormalizePartNumber(raw)
	}
	norm := NormalizePartNumber(raw)
	if IsASIN(norm) {
		return "asin:" + norm
	}
	if id := NormalizeProductSKU(sku); id != "" {
		return "sku:" + id
	}
	return ""
}


// NormalizePartNumber extracts and normalizes a manufacturer part number token.
func NormalizePartNumber(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if m := partNumberRE.FindString(s); m != "" {
		return canonicalPart(m)
	}
	return canonicalPart(s)
}

// IsManufacturerPartNumber reports whether s contains or equals a known drive part number.
func IsManufacturerPartNumber(s string) bool {
	return partNumberRE.MatchString(s)
}

// IsASIN reports whether s is a normalized Amazon ASIN (10 alphanumeric chars).
func IsASIN(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return len(s) == 10 && asinRE.MatchString(s)
}

// CanonicalBrandKey normalizes brand names for catalog facets.
func CanonicalBrandKey(brand *string) string {
	if brand == nil {
		return ""
	}
	b := canonicalPart(*brand)
	if b == "wd" || b == "westerndigital" {
		return "westerndigital"
	}
	return b
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
