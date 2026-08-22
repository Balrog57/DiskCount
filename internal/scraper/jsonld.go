package scraper

import (
	"encoding/json"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ProductData holds the structured fields extractable from schema.org/Product
// JSON-LD markup. Any field may be empty/nil when the page does not declare it.
type ProductData struct {
	Name         string
	Brand        string
	SKU          string
	MPN          string
	GTIN         string
	Image        string
	Price        string
	Currency     string
	Availability string
	URL          string
}

// ParseJSONLD extracts schema.org/Product (and nested Offer) data from the
// JSON-LD <script type="application/ld+json"> blocks embedded in an HTML page.
// Most e-commerce sites (Amazon, LDLC, Alternate, etc.) embed this structured
// data, which is far more reliable than scraping HTML tables or CSS selectors.
//
// It handles:
//   - A single JSON object or a JSON array of blocks.
//   - @graph wrappers (common in WordPress/Shopify sites).
//   - Offers as a single object or an array (the first is used).
//   - Brand as a string or as a {"@type":"Brand","name":"..."} object.
//
// Returns the first Product found, or false if none.
func ParseJSONLD(html string) (ProductData, bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ProductData{}, false
	}
	var result ProductData
	found := false

	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return true // continue
		}
		candidates := extractObjects(raw)
		for _, obj := range candidates {
			if pd, ok := productFromObject(obj); ok {
				result = pd
				found = true
				return false // stop iterating blocks
			}
		}
		return true
	})

	return result, found
}

// extractObjects normalises a raw JSON-LD blob into a slice of generic maps.
// Handles single objects, arrays, and @graph wrappers.
func extractObjects(raw string) []map[string]interface{} {
	// First, try as an array.
	var arr []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return flattenGraphs(arr)
	}
	// Fall back to a single object.
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil
	}
	return flattenGraphs([]map[string]interface{}{obj})
}

// flattenGraphs recurses into @graph entries, which are themselves arrays or
// single objects nested inside a parent node.
func flattenGraphs(objs []map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	for _, o := range objs {
		if g, ok := o["@graph"]; ok {
			switch v := g.(type) {
			case []interface{}:
				for _, item := range v {
					if m, ok := item.(map[string]interface{}); ok {
						out = append(out, flattenGraphs([]map[string]interface{}{m})...)
					}
				}
				continue
			case map[string]interface{}:
				out = append(out, flattenGraphs([]map[string]interface{}{v})...)
				continue
			}
		}
		out = append(out, o)
	}
	return out
}

// productFromObject checks whether a JSON object is a Product (or contains one
// via @type arrays) and extracts its fields plus the first Offer.
func productFromObject(obj map[string]interface{}) (ProductData, bool) {
	if !isProductType(obj) {
		return ProductData{}, false
	}
	pd := ProductData{
		Name:  stringField(obj, "name"),
		SKU:   stringField(obj, "sku"),
		MPN:   stringField(obj, "mpn"),
		GTIN:  firstNonEmpty(stringField(obj, "gtin13"), stringField(obj, "gtin14"), stringField(obj, "gtin")),
		Image: imageField(obj["image"]),
		URL:   stringField(obj, "url"),
	}
	if brand := obj["brand"]; brand != nil {
		pd.Brand = brandString(brand)
	}
	if offers := obj["offers"]; offers != nil {
		firstOffer(offers, &pd)
	}
	return pd, true
}

func imageField(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		for _, item := range v {
			if image := imageField(item); image != "" {
				return image
			}
		}
	case map[string]interface{}:
		return stringField(v, "url")
	}
	return ""
}

func isProductType(obj map[string]interface{}) bool {
	t := obj["@type"]
	if t == nil {
		return false
	}
	switch v := t.(type) {
	case string:
		return strings.EqualFold(v, "Product") || strings.EqualFold(v, "IndividualProduct")
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if strings.EqualFold(s, "Product") || strings.EqualFold(s, "IndividualProduct") {
					return true
				}
			}
		}
	}
	return false
}

func brandString(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return v
	case map[string]interface{}:
		return stringField(v, "name")
	}
	return ""
}

func firstOffer(raw interface{}, pd *ProductData) {
	var offer map[string]interface{}
	switch v := raw.(type) {
	case map[string]interface{}:
		offer = v
	case []interface{}:
		if len(v) == 0 {
			return
		}
		if m, ok := v[0].(map[string]interface{}); ok {
			offer = m
		}
	}
	if offer == nil {
		return
	}
	pd.Price = stringField(offer, "price")
	pd.Currency = stringField(offer, "priceCurrency")
	pd.Availability = stringField(offer, "availability")
	pd.URL = firstNonEmpty(pd.URL, stringField(offer, "url"))
}

func stringField(obj map[string]interface{}, key string) string {
	v, ok := obj[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case float64:
		// JSON numbers come through as float64; format without trailing noise.
		return formatNumber(s)
	default:
		b, _ := json.Marshal(v)
		return strings.Trim(string(b), `"`)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func formatNumber(f float64) string {
	s := strings.TrimRight(strings.TrimRight(
		// Use enough precision for price values.
		jsonNumber(f), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

// jsonNumber formats a float64 as a clean decimal string without scientific
// notation or unnecessary trailing zeros.
func jsonNumber(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}
