package domain

import (
	"net/url"
	"strings"
)

// AggregatorFetcherNames are scanner identifiers that must not persist as Deal.Source.
var AggregatorFetcherNames = map[string]bool{
	"diskprices":  true,
	"pricepertb":  true,
	"pricepergig": true,
	"keepa":       true,
	"geizhals":    true,
	"dealabs":     true,
	"idealo":      true,
	"ledenicheur": true,
	"leboncoin":   true,
	"ebay":        true,
}

var aggregatorHosts = map[string]bool{
	"geizhals.de":    true,
	"geizhals.eu":    true,
	"geizhals.at":    true,
	"dealabs.com":    true,
	"idealo.de":      true,
	"idealo.fr":      true,
	"idealo.es":      true,
	"idealo.it":      true,
	"ledenicheur.fr": true,
	"leboncoin.fr":   true,
}

type hostMerchant struct {
	hosts         []string
	slug, display string
}

var hostMerchants = []hostMerchant{
	{[]string{"ldlc.com", "ldlc-pro.com"}, "ldlc", "LDLC"},
	{[]string{"fnac.com"}, "fnac", "Fnac"},
	{[]string{"darty.com"}, "darty", "Darty"},
	{[]string{"boulanger.com"}, "boulanger", "Boulanger"},
	{[]string{"cdiscount.com"}, "cdiscount", "Cdiscount"},
	{[]string{"alternate.fr"}, "alternate", "Alternate"},
	{[]string{"grosbill.com"}, "grosbill", "Grosbill"},
	{[]string{"topachat.com"}, "topachat", "TopAchat"},
	{[]string{"topbiz.fr"}, "topbiz", "Topbiz"},
	{[]string{"rueducommerce.com"}, "rueducommerce", "Rue du Commerce"},
	{[]string{"materiel.net"}, "materiel", "Materiel.net"},
	{[]string{"pccomponentes.com", "pccomponentes.fr"}, "pccomponentes", "PCComponentes"},
	{[]string{"cybertek.fr"}, "cybertek", "Cybertek"},
	{[]string{"corsair.com"}, "corsair", "Corsair"},
	{[]string{"mindfactory.de"}, "mindfactory", "Mindfactory"},
	{[]string{"proshop.de"}, "proshop", "Proshop"},
	{[]string{"computeruniverse.net"}, "computeruniverse", "Computeruniverse"},
	{[]string{"backmarket.fr", "backmarket.com"}, "backmarket", "Back Market"},
	{[]string{"rakuten.fr", "rakuten.com"}, "rakuten", "Rakuten"},
}

var slugDisplay = map[string]string{
	"alternate":       "Alternate",
	"boulanger":       "Boulanger",
	"cdiscount":       "Cdiscount",
	"corsair":         "Corsair",
	"cybertek":        "Cybertek",
	"darty":           "Darty",
	"fnac":            "Fnac",
	"grosbill":        "Grosbill",
	"ldlc":            "LDLC",
	"materiel":        "Materiel.net",
	"pccomponentes":   "PCComponentes",
	"rueducommerce":   "Rue du Commerce",
	"topachat":        "TopAchat",
	"topbiz":          "Topbiz",
	"mindfactory":     "Mindfactory",
	"proshop":         "Proshop",
	"computeruniverse": "Computeruniverse",
	"backmarket":      "Back Market",
	"rakuten":         "Rakuten",
}

// NormalizeHost strips www. and trailing dots from a hostname.
func NormalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	return strings.TrimPrefix(host, "www.")
}

// MerchantFromURL maps a product URL to a canonical merchant slug and display label.
func MerchantFromURL(rawURL string) (slug, display string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return "", "", false
	}
	host := NormalizeHost(u.Host)
	if aggregatorHosts[host] {
		return "", "", false
	}
	if strings.HasPrefix(host, "amazon.") {
		return host, amazonDisplay(host), true
	}
	if strings.HasPrefix(host, "ebay.") {
		return host, ebayDisplay(host), true
	}
	for _, hm := range hostMerchants {
		for _, h := range hm.hosts {
			if host == h || strings.HasSuffix(host, "."+h) {
				return hm.slug, hm.display, true
			}
		}
	}
	return "", "", false
}

// DisplayForSlug returns the human label for a merchant slug (e.g. fnac, amazon.fr).
func DisplayForSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if d, ok := slugDisplay[slug]; ok {
		return d
	}
	if strings.HasPrefix(slug, "amazon.") {
		return amazonDisplay(slug)
	}
	if strings.HasPrefix(slug, "ebay.") {
		return ebayDisplay(slug)
	}
	return ""
}

// ResolveMerchant sets Deal.Source and Deal.Merchant from the product URL when possible.
func ResolveMerchant(d *Deal) bool {
	if slug, display, ok := MerchantFromURL(d.URL); ok {
		d.Source = slug
		if d.Merchant == nil || isHostLikeMerchant(*d.Merchant) {
			d.Merchant = &display
		}
		return true
	}
	if display := DisplayForSlug(d.Source); display != "" && !IsAggregatorFetcher(d.Source) {
		if d.Merchant == nil {
			d.Merchant = &display
		}
		return true
	}
	return false
}

func isHostLikeMerchant(m string) bool {
	m = strings.ToLower(strings.TrimSpace(m))
	return strings.Contains(m, ".") || m == "amazon"
}

// IsAggregatorFetcher reports whether source is a fetcher name, not a merchant slug.
func IsAggregatorFetcher(source string) bool {
	return AggregatorFetcherNames[strings.TrimSpace(strings.ToLower(source))]
}

// IsAggregatorHost reports whether the URL host is a pure price-comparison site.
func IsAggregatorHost(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return false
	}
	return aggregatorHosts[NormalizeHost(u.Host)]
}

// NeedsConcreteMerchant reports whether the deal still points at an aggregator instead of a shop.
func NeedsConcreteMerchant(d Deal) bool {
	return IsAggregatorFetcher(d.Source) || IsAggregatorHost(d.URL)
}

func amazonDisplay(hostOrTLD string) string {
	tld := strings.TrimPrefix(hostOrTLD, "amazon.")
	switch tld {
	case "fr":
		return "Amazon France"
	case "de":
		return "Amazon Germany"
	case "es":
		return "Amazon España"
	case "it":
		return "Amazon Italia"
	case "nl":
		return "Amazon Nederland"
	case "pl":
		return "Amazon Polska"
	case "se":
		return "Amazon Sverige"
	case "co.uk":
		return "Amazon UK"
	case "com":
		return "Amazon"
	case "ca":
		return "Amazon Canada"
	case "com.au":
		return "Amazon Australia"
	case "co.jp":
		return "Amazon Japan"
	case "com.mx":
		return "Amazon Mexico"
	case "in":
		return "Amazon India"
	default:
		return "Amazon"
	}
}

func ebayDisplay(hostOrTLD string) string {
	tld := strings.TrimPrefix(hostOrTLD, "ebay.")
	switch tld {
	case "fr":
		return "eBay France"
	case "de":
		return "eBay Germany"
	case "es":
		return "eBay España"
	case "it":
		return "eBay Italia"
	case "co.uk":
		return "eBay UK"
	case "com":
		return "eBay"
	default:
		return "eBay"
	}
}
