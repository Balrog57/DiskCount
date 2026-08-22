package sources

import (
	"math"
	"net/url"
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
	s = strings.ReplaceAll(s, "€", "")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	return strconv.ParseFloat(s, 64)
}

func isAmazonURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	host = strings.TrimPrefix(host, "www.")
	switch host {
	case "amazon.fr", "amazon.de", "amazon.es", "amazon.it", "amazon.nl", "amazon.pl", "amazon.se", "amazon.com", "amazon.ca", "amazon.co.uk", "amazon.com.au", "amazon.co.jp":
		return true
	default:
		return false
	}
}

func amazonImageURL(asin string) string {
	asin = strings.ToUpper(strings.TrimSpace(asin))
	if len(asin) != 10 {
		return ""
	}
	return "https://m.media-amazon.com/images/P/" + asin + ".jpg"
}

func amazonSKU(model string, asin *string) *string {
	if model = strings.TrimSpace(model); model != "" {
		return &model
	}
	if asin != nil {
		return strPtr(*asin)
	}
	return nil
}

func amazonImageFromASIN(asin *string) *string {
	if asin == nil {
		return nil
	}
	return strPtr(amazonImageURL(*asin))
}
