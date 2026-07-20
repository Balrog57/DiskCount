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

	// ⚡ Bolt: Single-pass iteration replacing strings.ReplaceAll chain.
	// We iterate over runes to properly handle multibyte characters (like € and \u00a0)
	// and preserve invalid characters so strconv.ParseFloat can fail naturally,
	// maintaining original strict parsing semantics.
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		if r == '€' || r == '\u00a0' || r == ' ' {
			continue
		} else if r == ',' {
			b.WriteRune('.')
		} else {
			b.WriteRune(r)
		}
	}

	return strconv.ParseFloat(b.String(), 64)
}
