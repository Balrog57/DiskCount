package sources

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/scraper"
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
		return strPtr(strings.ToUpper(strings.TrimSpace(*asin)))
	}
	return nil
}

func externalIDFromHref(href string, patterns ...string) *string {
	href = strings.TrimSpace(href)
	if href == "" {
		return nil
	}
	lower := strings.ToLower(href)
	for _, pattern := range patterns {
		idx := strings.Index(lower, strings.ToLower(pattern))
		if idx < 0 {
			continue
		}
		rest := href[idx+len(pattern):]
		for _, sep := range []string{".", "/", "?", "#"} {
			if p := strings.Index(rest, sep); p >= 0 {
				rest = rest[:p]
			}
		}
		rest = strings.TrimSpace(rest)
		if rest != "" {
			return strPtr(rest)
		}
	}
	return nil
}

func partNumberFromText(text string) *string {
	if m := partNumberRE.FindString(text); m != "" {
		return strPtr(m)
	}
	return nil
}

// partNumberRE mirrors domain part numbers for title extraction in scrapers.
var partNumberRE = regexp.MustCompile(`(?i)\b(?:ST\d{3,}[A-Z0-9-]{2,}|WD[A-Z0-9][A-Z0-9-]{3,}|WDS[A-Z0-9-]{4,}|CT\d{3,}[A-Z0-9-]{3,}|MZ[A-Z0-9-]{4,}|SK[A-Z0-9-]{4,}|HUS\d{3,}[A-Z0-9-]*|MG\d{3,}[A-Z0-9-]*|SSDSC[A-Z0-9-]{3,}|D3-S\d{4,}[A-Z0-9-]*|900[A-Z0-9-]{4,})\b`)

// applyListingJSONLD fills missing EAN/SKU/brand on listing cards from page-level JSON-LD.
func applyListingJSONLD(html string, deals []domain.Deal) []domain.Deal {
	if len(deals) == 0 {
		return deals
	}
	pd, ok := scraper.ParseJSONLD(html)
	if !ok {
		return deals
	}
	for i := range deals {
		if deals[i].EAN == nil && pd.GTIN != "" {
			deals[i].EAN = strPtr(pd.GTIN)
		}
		if deals[i].SKU == nil {
			if pd.MPN != "" {
				deals[i].SKU = strPtr(pd.MPN)
			} else if pd.SKU != "" {
				deals[i].SKU = strPtr(pd.SKU)
			}
		}
		if deals[i].Brand == nil && pd.Brand != "" {
			deals[i].Brand = strPtr(pd.Brand)
		}
		if deals[i].ImageURL == nil && pd.Image != "" {
			deals[i].ImageURL = strPtr(pd.Image)
		}
		if deals[i].ClassificationSource == "" && (pd.GTIN != "" || pd.MPN != "" || pd.SKU != "") {
			deals[i].ClassificationSource = "jsonld"
		}
	}
	return deals
}

func amazonImageFromASIN(asin *string) *string {
	if asin == nil {
		return nil
	}
	return strPtr(amazonImageURL(*asin))
}
