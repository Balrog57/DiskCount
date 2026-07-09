package sources

import (
	"fmt"
	"strconv"
	"strings"
)

func round2(v float64) float64 {
	r, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", v), 64)
	return r
}

func strPtr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return nil
	}
	return &s
}

func parseFloatClean(s string) (float64, error) {
	s = strings.TrimSpace(s)
	// ⚡ Bolt: Single-pass rune iteration to eliminate intermediate string allocations.
	// Uses IndexAny for a fast path to avoid allocating when no replacements are needed.
	idx := strings.IndexAny(s, "€\u00a0 ,")
	var clean string
	if idx < 0 {
		clean = s
	} else {
		var b strings.Builder
		b.Grow(len(s))
		b.WriteString(s[:idx])
		for _, r := range s[idx:] {
			switch r {
			case '€', ' ', '\u00a0':
				// skip
			case ',':
				b.WriteByte('.')
			default:
				b.WriteRune(r)
			}
		}
		clean = b.String()
	}
	return strconv.ParseFloat(clean, 64)
}
