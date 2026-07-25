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

	needsClean := false
	for _, r := range s {
		if r == '€' || r == '\u00a0' || r == ' ' || r == ',' {
			needsClean = true
			break
		}
	}

	if !needsClean {
		return strconv.ParseFloat(s, 64)
	}

	// ⚡ Bolt: Single-pass iteration to eliminate multiple string allocations
	// from chained strings.ReplaceAll on the price parsing hot path.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '€', '\u00a0', ' ':
			// skip
		case ',':
			b.WriteRune('.')
		default:
			b.WriteRune(r)
		}
	}
	return strconv.ParseFloat(b.String(), 64)
}
