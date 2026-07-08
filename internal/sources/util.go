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

	// ⚡ Bolt: Fast-path check and single-pass iteration with strings.Builder
	// avoids 4 separate string allocations from multiple strings.ReplaceAll calls.
	var hasTarget bool
	for _, r := range s {
		if r == '€' || r == '\u00a0' || r == ' ' || r == ',' {
			hasTarget = true
			break
		}
	}

	if hasTarget {
		var b strings.Builder
		b.Grow(len(s))
		for _, r := range s {
			switch r {
			case '€', '\u00a0', ' ':
				continue
			case ',':
				b.WriteByte('.')
			default:
				b.WriteRune(r)
			}
		}
		s = b.String()
	}

	return strconv.ParseFloat(s, 64)
}
