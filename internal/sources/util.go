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
	// ⚡ Bolt: Single-pass iteration to minimize string allocations
	idx := strings.IndexAny(s, "€\u00a0 ,")
	if idx != -1 {
		var b strings.Builder
		b.Grow(len(s))
		b.WriteString(s[:idx])
		for _, r := range s[idx:] {
			if r == '€' || r == '\u00a0' || r == ' ' {
				continue
			} else if r == ',' {
				b.WriteByte('.')
			} else {
				b.WriteRune(r)
			}
		}
		s = b.String()
	}
	return strconv.ParseFloat(s, 64)
}
