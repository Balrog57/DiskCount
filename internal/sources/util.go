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

	// ⚡ Bolt: Use IndexAny as a fast-path check and a single-pass builder
	// for multibyte replacement to prevent intermediate string allocations.
	idx := strings.IndexAny(s, "€\u00a0 ,")
	if idx == -1 {
		return strconv.ParseFloat(s, 64)
	}

	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:idx])

	for _, r := range s[idx:] {
		switch r {
		case '€', '\u00a0', ' ':
			// skip
		case ',':
			b.WriteByte('.')
		default:
			b.WriteRune(r)
		}
	}
	return strconv.ParseFloat(b.String(), 64)
}
