package parsing

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
)

var (
	capacityRE    = regexp.MustCompile(`(?i)(?P<value>\d+(?:[,.]\d+)?)\s*(?P<unit>t[bo]|g[bo]|m[bo]|tb|gb|mb)\b`)
	euroRE        = regexp.MustCompile(`(?i)(?:^|[€]\s*)(?P<prefix>\d[\d\s\x{00a0}.,]*(?:[,.]\d{1,3})?)|(?P<suffix>\d[\d\s\x{00a0}.,]*(?:[,.]\d{1,3})?)\s*[€]`)
	asinRE        = regexp.MustCompile(`(?i)(?:/dp/|/gp/product/|/product/)(?P<asin>[A-Z0-9]{10})(?:[/?#]|$)`)
	nonDigitDotRE = regexp.MustCompile(`[^0-9.]`)
)

func asciiFold(s string) string {
	if s == "" {
		return ""
	}
	// ⚡ Bolt: Use byte-level iteration and inline case folding to minimize allocations
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 128 {
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			b.WriteByte(c)
		}
	}
	return b.String()
}

func parseDecimal(text string) (float64, error) {
	// ⚡ Bolt: Single-pass byte iteration to extract digits and decimal separator
	// avoiding multiple string allocations from strings.ReplaceAll and regex.
	hasComma := false
	hasDot := false
	for i := 0; i < len(text); i++ {
		if text[i] == ',' {
			hasComma = true
		} else if text[i] == '.' {
			hasDot = true
		}
	}

	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= '0' && c <= '9' {
			b.WriteByte(c)
		} else if c == ',' {
			if hasDot {
				// If both present, comma acts as decimal point in original logic
				b.WriteByte('.')
			} else {
				// If only comma, it acts as decimal point
				b.WriteByte('.')
			}
		} else if c == '.' {
			if hasComma {
				// If both present, dot is ignored (thousands separator)
				continue
			} else {
				b.WriteByte('.')
			}
		}
	}

	if b.Len() == 0 {
		return 0, nil
	}
	return strconv.ParseFloat(b.String(), 64)
}

func ParsePriceEUR(text string) (float64, error) {
	if text == "" {
		return 0, nil
	}
	folded := asciiFold(text)
	if containsAny(folded, "/mois", "mensuel", "par mois", "month", "monthly") {
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
	if strings.HasPrefix(unitStr, "m") {
		return value / 1000_000.0, nil
	}
	if strings.HasPrefix(unitStr, "g") {
		return value / 1000.0, nil
	}
	return value, nil
}

func NormalizeCondition(text string) *domain.Condition {
	folded := asciiFold(text)
	usedWords := []string{"used", "occasion", "reconditionne", "reconditionne", "refurbished", "renewed", "seconde main"}
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
	ssdWords := []string{"ssd", "nvme", "solid state", "nand", "tlc", "qlc", "mlc", "m2", "m 2", "pcie", "pci-e", "u2", "u 2", "u3", "u 3"}
	hddWords := []string{"hdd", "disque dur", "hard drive", "hard disk", "7200rpm", "5400rpm", "7200 tr", "5400 tr"}
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
	// ⚡ Bolt: Single-pass byte iteration for ascii-folding and character replacements.
	// Replaces multiple sequential strings.ReplaceAll allocations.
	// Note: We replicate the exact behavior of the asciiFold function, which
	// explicitly strips non-ASCII characters (bytes >= 128) by design.
	var sb strings.Builder
	sb.Grow(len(text))
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c < 128 {
			if c == '"' || c == '.' {
				continue
			}
			if c == '-' {
				sb.WriteByte(' ')
				continue
			}
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			sb.WriteByte(c)
		}
	}
	compact := sb.String()

	isExternal := false
	for _, w := range []string{"external", "externe", "portable", "usb", "boitier"} {
		if strings.Contains(compact, w) {
			isExternal = true
			break
		}
	}
	isInternal := false
	for _, w := range []string{"internal", "interne", "m2", "m 2", "u2", "u 2", "u3", "u 3", "sata", "sas", "nas", "surveillance"} {
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
		if containsAny(compact, "25", "2 5", "2,5", "25 inch", "25 pouces") && !containsAny(compact, "35", "3 5", "3,5") {
			c := domain.DriveCategoryInternalSSD
			if isExternal {
				c = domain.DriveCategoryExternalSSD
			}
			return &c
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
	is25 := containsAny(compact, "25", "2 5", "2,5", "25 inch", "25 pouces")
	is35 := containsAny(compact, "35", "3 5", "3,5", "35 inch", "35 pouces")
	if isExternal && is25 && !is35 {
		c := domain.DriveCategoryExternal2_5
		return &c
	}
	if isExternal && (is35 || !is25) {
		c := domain.DriveCategoryExternal3_5
		return &c
	}
	if isInternal && is25 && !is35 {
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
