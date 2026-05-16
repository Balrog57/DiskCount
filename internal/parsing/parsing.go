package parsing

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
)

var (
	capacityRE = regexp.MustCompile(`(?i)(?P<value>\d+(?:[,.]\d+)?)\s*(?P<unit>t[bo]|g[bo]|tb|gb)\b`)
	euroRE     = regexp.MustCompile(`(?i)(?:^|[€]\s*)(?P<prefix>\d[\d\s\x{00a0}.,]*(?:[,.]\d{1,3})?)|(?P<suffix>\d[\d\s\x{00a0}.,]*(?:[,.]\d{1,3})?)\s*[€]`)
	asinRE     = regexp.MustCompile(`(?i)(?:/dp/|/gp/product/|/product/)(?P<asin>[A-Z0-9]{10})(?:[/?#]|$)`)
)

func asciiFold(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if r < 128 {
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

func parseDecimal(text string) (float64, error) {
	cleaned := strings.TrimSpace(text)
	cleaned = strings.ReplaceAll(cleaned, "\u00a0", " ")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	if cleaned == "" {
		return 0, nil
	}
	if strings.Contains(cleaned, ",") && strings.Contains(cleaned, ".") {
		cleaned = strings.ReplaceAll(cleaned, ".", "")
		cleaned = strings.ReplaceAll(cleaned, ",", ".")
	} else {
		cleaned = strings.ReplaceAll(cleaned, ",", ".")
	}
	cleaned = regexp.MustCompile(`[^0-9.]`).ReplaceAllString(cleaned, "")
	if cleaned == "" {
		return 0, nil
	}
	return strconv.ParseFloat(cleaned, 64)
}

func ParsePriceEUR(text string) (float64, error) {
	if text == "" {
		return 0, nil
	}
	match := euroRE.FindStringSubmatch(text)
	if match != nil {
		prefixIdx := euroRE.SubexpIndex("prefix")
		suffixIdx := euroRE.SubexpIndex("suffix")
		raw := ""
		if prefixIdx >= 0 && match[prefixIdx] != "" {
			raw = match[prefixIdx]
		} else if suffixIdx >= 0 && match[suffixIdx] != "" {
			raw = match[suffixIdx]
		}
		if raw != "" {
			return parseDecimal(raw)
		}
	}
	return parseDecimal(text)
}

func ParseCapacityTB(text string) (float64, error) {
	if text == "" {
		return 0, nil
	}
	match := capacityRE.FindStringSubmatch(text)
	if match == nil {
		return 0, nil
	}
	valIdx := capacityRE.SubexpIndex("value")
	unitIdx := capacityRE.SubexpIndex("unit")
	if valIdx < 0 || unitIdx < 0 {
		return 0, nil
	}
	valStr := match[valIdx]
	unitStr := asciiFold(match[unitIdx])
	value, err := parseDecimal(valStr)
	if err != nil || value == 0 {
		return 0, err
	}
	if strings.HasPrefix(unitStr, "g") {
		return value / 1000.0, nil
	}
	return value, nil
}

func NormalizeCondition(text string) *domain.Condition {
	folded := asciiFold(text)
	usedWords := []string{"used", "occasion", "reconditionne", "refurbished"}
	newWords := []string{"new", "neuf", "neuve"}
	for _, w := range usedWords {
		if strings.Contains(folded, w) {
			c := domain.ConditionUsed
			return &c
		}
	}
	for _, w := range newWords {
		if strings.Contains(folded, w) {
			c := domain.ConditionNew
			return &c
		}
	}
	return nil
}

func NormalizeMediaType(text string) *domain.MediaType {
	folded := asciiFold(text)
	ssdWords := []string{"ssd", "nvme", "solid state"}
	hddWords := []string{"hdd", "disque dur", "hard drive", "7200rpm", "5400rpm", "3.5", "2.5"}
	for _, w := range ssdWords {
		if strings.Contains(folded, w) {
			m := domain.MediaTypeSolidState
			return &m
		}
	}
	for _, w := range hddWords {
		if strings.Contains(folded, w) {
			m := domain.MediaTypeRotational
			return &m
		}
	}
	return nil
}

func NormalizeDriveCategory(text string, mediaType *domain.MediaType) *domain.DriveCategory {
	folded := asciiFold(text)
	folded = strings.ReplaceAll(folded, "\"", "")
	compact := strings.ReplaceAll(folded, ".", "")
	compact = strings.ReplaceAll(compact, "-", " ")

	isExternal := false
	for _, w := range []string{"external", "externe", "usb"} {
		if strings.Contains(compact, w) {
			isExternal = true
			break
		}
	}
	isInternal := false
	for _, w := range []string{"internal", "interne", "m2", "m 2", "u2", "u 2", "u3", "u 3"} {
		if strings.Contains(compact, w) {
			isInternal = true
			break
		}
	}

	if mediaType != nil && *mediaType == domain.MediaTypeSolidState {
		for _, w := range []string{"m2 nvme", "m 2 nvme", "nvme"} {
			if strings.Contains(compact, w) {
				c := domain.DriveCategoryM2NVMe
				return &c
			}
		}
		for _, w := range []string{"m2 sata", "m 2 sata"} {
			if strings.Contains(compact, w) {
				c := domain.DriveCategoryM2SATA
				return &c
			}
		}
		for _, w := range []string{"u2", "u 2", "u3", "u 3"} {
			if strings.Contains(compact, w) {
				c := domain.DriveCategoryU2U3
				return &c
			}
		}
		if isExternal {
			c := domain.DriveCategoryExternalSSD
			return &c
		}
		if isInternal {
			c := domain.DriveCategoryInternalSSD
			return &c
		}
		return nil
	}

	if strings.Contains(compact, "hybrid") || strings.Contains(compact, "sshd") {
		c := domain.DriveCategoryInternalHybrid
		return &c
	}
	if strings.Contains(compact, "sas") {
		c := domain.DriveCategoryInternalSAS
		return &c
	}
	if isExternal && strings.Contains(compact, "25") {
		c := domain.DriveCategoryExternal2_5
		return &c
	}
	if isExternal {
		c := domain.DriveCategoryExternal3_5
		return &c
	}
	if isInternal && strings.Contains(compact, "25") {
		c := domain.DriveCategoryInternal2_5
		return &c
	}
	if isInternal {
		c := domain.DriveCategoryInternal3_5
		return &c
	}
	return nil
}

func NormalizeInterfaces(text string) []domain.DriveInterface {
	folded := asciiFold(text)
	var ifaces []domain.DriveInterface

	if containsAny(folded, "nvme", "pcie", "pci-e") {
		ifaces = append(ifaces, domain.DriveInterfaceNVMe)
	}
	if strings.Contains(folded, "sata") {
		ifaces = append(ifaces, domain.DriveInterfaceSATA)
	}
	if strings.Contains(folded, "sas") {
		ifaces = append(ifaces, domain.DriveInterfaceSAS)
	}
	if strings.Contains(folded, "usb") {
		ifaces = append(ifaces, domain.DriveInterfaceUSB)
	}
	return ifaces
}

func ExtractASIN(rawURL string) *string {
	if rawURL == "" {
		return nil
	}
	match := asinRE.FindStringSubmatch(rawURL)
	if match == nil {
		return nil
	}
	idx := asinRE.SubexpIndex("asin")
	if idx < 0 {
		return nil
	}
	result := strings.ToUpper(match[idx])
	return &result
}

func containsAny(text string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}
