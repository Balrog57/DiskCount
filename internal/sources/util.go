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

// parseFloatClean normalizes price strings to floats.
// ⚡ Bolt: Single-pass iteration to minimize allocations instead of chaining ReplaceAll.
func parseFloatClean(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if !strings.ContainsAny(s, "€\u00a0 ,") {
		return strconv.ParseFloat(s, 64)
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == ' ' {
			i++
			continue
		}
		if s[i] == ',' {
			b.WriteByte('.')
			i++
			continue
		}
		// € is 3 bytes: \xe2\x82\xac
		if i+2 < len(s) && s[i] == '\xe2' && s[i+1] == '\x82' && s[i+2] == '\xac' {
			i += 3
			continue
		}
		// \u00a0 is 2 bytes: \xc2\xa0
		if i+1 < len(s) && s[i] == '\xc2' && s[i+1] == '\xa0' {
			i += 2
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return strconv.ParseFloat(b.String(), 64)
}
