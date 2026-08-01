package sources

import (
	"math"
	"strconv"
	"strings"
)

// round2 rounds a float64 to two decimal places. It uses math.Round rather
// than fmt.Sprintf+ParseFloat (the previous implementation) because every
// price field of every deal in every source flows through here on the scan
// hot path — the Sprintf variant allocated a string per call.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func strPtr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return nil
	}
	return &s
}

// parseFloatClean cleans a price string and parses it to a float64.
// It uses a single-pass strings.Builder to minimize allocations on the hot path.
func parseFloatClean(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "€\u00a0 ,") {
		var b strings.Builder
		b.Grow(len(s))
		for _, r := range s {
			switch r {
			case '€', '\u00a0', ' ':
				continue
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
