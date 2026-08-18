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

// parseFloatClean parses price texts.
//
// ⚡ Bolt optimization: Refactored multiple strings.ReplaceAll calls into a
// single-pass string builder with a fast-path scan. Previously, chaining
// four ReplaceAll calls triggered multiple allocations and iterations per call.
// The fast-path scan checks if formatting is even needed. If no symbols/spaces
// are present, it avoids allocations entirely. Otherwise, a strings.Builder
// performs all substitutions in a single O(N) pass.
func parseFloatClean(s string) (float64, error) {
	s = strings.TrimSpace(s)

	hasToReplace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == ',' || c == 0xe2 || c == 0xc2 {
			hasToReplace = true
			break
		}
	}

	if !hasToReplace {
		return strconv.ParseFloat(s, 64)
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ' ':
			continue
		case ',':
			b.WriteByte('.')
		case 0xe2:
			// € is e2 82 ac
			if i+2 < len(s) && s[i+1] == 0x82 && s[i+2] == 0xac {
				i += 2
				continue
			}
			b.WriteByte(c)
		case 0xc2:
			// \u00a0 (NBSP) is c2 a0
			if i+1 < len(s) && s[i+1] == 0xa0 {
				i++
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return strconv.ParseFloat(b.String(), 64)
}
