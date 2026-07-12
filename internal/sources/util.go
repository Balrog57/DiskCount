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
	// Optimization: Use IndexAny as a fast-path check to avoid allocating a builder
	// if the string doesn't contain any target characters.
	// If it does, we iterate from that first index and use a single-pass strings.Builder
	// to eliminate the intermediate allocations caused by chaining strings.ReplaceAll.
	firstIdx := strings.IndexAny(s, "€\u00a0 ,")
	if firstIdx != -1 {
		var b strings.Builder
		b.Grow(len(s))
		b.WriteString(s[:firstIdx])
		for _, r := range s[firstIdx:] {
			switch r {
			case '€', '\u00a0', ' ':
				// Skip these characters
			case ',':
				b.WriteRune('.')
			default:
				b.WriteRune(r)
			}
		}
		s = b.String()
	}
	return strconv.ParseFloat(s, 64)
}
