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

// parseFloatClean cleans European price formats before parsing.
// ⚡ Bolt optimization: using a fast byte scan to avoid allocations when
// the string needs no modification, and a single-pass strings.Builder
// blacklist approach to replace multiple chained strings.ReplaceAll
// calls. This avoids string allocations on the hot path for scraper parsing.
func parseFloatClean(s string) (float64, error) {
	needsModification := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == ',' || c == '\xe2' || c == '\xc2' {
			needsModification = true
			break
		}
	}

	if !needsModification {
		return strconv.ParseFloat(s, 64)
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			continue
		}
		if c == ',' {
			b.WriteByte('.')
			continue
		}
		if c == '\xe2' && i+2 < len(s) && s[i+1] == '\x82' && s[i+2] == '\xac' { // €
			i += 2
			continue
		}
		if c == '\xc2' && i+1 < len(s) && s[i+1] == '\xa0' { // \u00a0
			i += 1
			continue
		}
		b.WriteByte(c)
	}
	return strconv.ParseFloat(b.String(), 64)
}
