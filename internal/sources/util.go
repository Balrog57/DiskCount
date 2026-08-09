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

func parseFloatClean(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if !strings.ContainsAny(s, "€\u00a0 ,") {
		return strconv.ParseFloat(s, 64)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		switch c {
		case '€', '\u00a0', ' ':
			continue
		case ',':
			b.WriteRune('.')
		default:
			b.WriteRune(c)
		}
	}
	return strconv.ParseFloat(b.String(), 64)
}
