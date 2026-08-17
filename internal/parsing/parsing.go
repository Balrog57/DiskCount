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

// conditionUsedWords / conditionNewWords / ssdWords / hddWords and the
// category keyword lists below are the substrings used by the Normalize*
// classifiers.
//
// ⚡ Bolt optimization: hoisted to package-level vars so the classifiers do
// not allocate a fresh slice literal on every call. Each Normalize* function
// runs once per accepted deal in the normalize pipeline (and again inside
// parseDiskPrices per row), so a 200-deal scan previously allocated ~200
// copies of each list. All entries are ASCII constants with no per-call
// state and are matched against asciiFold-stripped text, so they must stay
// accent-free (see bolt.md asciiFold note).
var (
	conditionUsedWords = []string{"used", "occasion", "reconditionne", "reconditionne", "refurbished", "renewed", "seconde main"}
	conditionNewWords  = []string{"new", "neuf", "neuve"}

	ssdWords = []string{"ssd", "nvme", "solid state", "nand", "tlc", "qlc", "mlc", "m2", "m 2", "pcie", "pci-e", "u2", "u 2", "u3", "u 3"}
	hddWords = []string{"hdd", "disque dur", "hard drive", "hard disk", "7200rpm", "5400rpm", "7200 tr", "5400 tr"}

	externalWords = []string{"external", "externe", "portable", "usb", "boitier"}
	internalWords = []string{"internal", "interne", "m2", "m 2", "u2", "u 2", "u3", "u 3", "sata", "sas", "nas", "surveillance"}

	m2NvmeWords = []string{"m2 nvme", "m 2 nvme", "nvme"}
	m2SataWords = []string{"m2 sata", "m 2 sata"}
	u2u3Words   = []string{"u2", "u 2", "u3", "u 3"}
)

func NormalizeCondition(text string) *domain.Condition {
	folded := asciiFold(text)
	for _, w := range conditionUsedWords {
		if strings.Contains(folded, w) {
			c := domain.ConditionUsed
			return &c
		}
	}
	for _, w := range conditionNewWords {
		if strings.Contains(folded, w) {
			c := domain.ConditionNew
			return &c
		}
	}
	return nil
}

func NormalizeMediaType(text string) *domain.MediaType {
	folded := asciiFold(text)
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

	compact := folded
	if strings.ContainsAny(compact, "\".-") {
		// ⚡ Bolt: Use strings.Builder and fast-path check instead of chained strings.ReplaceAll
		// to avoid multiple allocations when stripping punctuation.
		var b strings.Builder
		b.Grow(len(compact))
		for i := 0; i < len(compact); i++ {
			c := compact[i]
			if c == '"' || c == '.' {
				continue
			}
			if c == '-' {
				b.WriteByte(' ')
				continue
			}
			b.WriteByte(c)
		}
		compact = b.String()
	}

	isExternal := false
	for _, w := range externalWords {
		if strings.Contains(compact, w) {
			isExternal = true
			break
		}
	}
	isInternal := false
	for _, w := range internalWords {
		if strings.Contains(compact, w) {
			isInternal = true
			break
		}
	}

	if mediaType != nil && *mediaType == domain.MediaTypeSolidState {
		for _, w := range m2NvmeWords {
			if strings.Contains(compact, w) {
				c := domain.DriveCategoryM2NVMe
				return &c
			}
		}
		for _, w := range m2SataWords {
			if strings.Contains(compact, w) {
				c := domain.DriveCategoryM2SATA
				return &c
			}
		}
		for _, w := range u2u3Words {
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

// DefaultInterfacesForCategory is the single source of truth for the
// "if no explicit interface was detected, infer one from the drive category"
// mapping. It was previously duplicated verbatim in four places
// (normalize.inferInterfaces, sources.defaultInterfacesForCategory, and
// inline in diskprices/pricepertb). Centralising it here means a future
// change to the table only touches one file.
func DefaultInterfacesForCategory(dc domain.DriveCategory) []domain.DriveInterface {
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

// cmrFamilies and smrFamilies list the model-family substrings that imply a
// drive's recording method when no explicit "CMR"/"SMR" label is present.
//
// ⚡ Bolt optimization: hoisted to package-level vars so NormalizeRecordingMethod
// does not allocate two fresh slices on every call. It runs once per accepted
// HDD deal in the normalize pipeline and once per row in parseDiskPrices, so
// a single scan previously allocated ~400 short-lived slices (~23 entries
// each) just for recording-method detection. All entries are ASCII constants
// with no per-call state.
var (
	cmrFamilies = []string{
		"exos", "ironwolf", "wd red plus", "red plus", "ultrastar",
		"wd gold", "wdgold", "seagate skyhawk", "skyhawk",
		"toshiba mg", "toshiba mn", "mg07", "mg08", "mg09", "mg10",
		"hgst", "enterprise capacity",
	}
	smrFamilies = []string{
		"wd red ", " wd red,", " wd red-", // base WD Red (not "Plus")
		"barracuda", "archive", "smrdata",
		"toshiba l200", "toshiba md3004", "l200",
		"seagate enterprise smr",
	}
)

// NormalizeRecordingMethod infers whether a rotational drive uses CMR or SMR.
// This only applies to HDDs; SSDs return nil. Detection priority:
//  1. Explicit "CMR"/"conventional" or "SMR"/"shingled" in the title/tech text.
//  2. Known CMR model families (Exos, IronWolf, WD Red Plus, Ultrastar, Gold,
//     Toshiba MG/MN enterprise series).
//  3. Known SMR model families (WD Red base, Seagate Barracuda/Archive,
//     Toshiba L200/MD3004).
//  4. nil when undetermined (the alert matcher treats nil as "unknown").
func NormalizeRecordingMethod(text string, mediaType *domain.MediaType) *domain.RecordingMethod {
	if mediaType != nil && *mediaType == domain.MediaTypeSolidState {
		return nil
	}
	folded := asciiFold(text)

	compact := folded
	if strings.ContainsAny(compact, "-_") {
		// ⚡ Bolt: Use strings.Builder and fast-path check instead of chained strings.ReplaceAll
		// to avoid multiple allocations when stripping punctuation.
		var b strings.Builder
		b.Grow(len(compact))
		for i := 0; i < len(compact); i++ {
			c := compact[i]
			if c == '-' || c == '_' {
				continue
			}
			b.WriteByte(c)
		}
		compact = b.String()
	}

	// Explicit declaration wins.
	if strings.Contains(folded, "conventional") || strings.Contains(compact, "cmr") {
		c := domain.RecordingMethodCMR
		return &c
	}
	if strings.Contains(folded, "shingled") || strings.Contains(compact, "smr") {
		c := domain.RecordingMethodSMR
		return &c
	}

	// Known CMR families (package-level var; see cmrFamilies comment).
	for _, f := range cmrFamilies {
		if strings.Contains(folded, f) {
			c := domain.RecordingMethodCMR
			return &c
		}
	}

	// Known SMR families (package-level var; see smrFamilies comment).
	for _, f := range smrFamilies {
		if strings.Contains(folded, f) {
			c := domain.RecordingMethodSMR
			return &c
		}
	}

	return nil
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
