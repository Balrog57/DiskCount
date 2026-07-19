package sources

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
	"github.com/mmcdole/gofeed"
)

func init() {
	Register(func(r *Registry) Source {
		urls := r.Config().DealabsRSSURLs
		if len(urls) == 0 {
			return nil
		}
		return &FeedSource{name: "dealabs", urls: urls, def: domain.ConditionNew, http: r.HTTP()}
	})
	Register(func(r *Registry) Source {
		urls := r.Config().IdealoFeedURLs
		if len(urls) == 0 {
			return nil
		}
		return &FeedSource{name: "idealo", urls: urls, def: domain.ConditionNew, http: r.HTTP()}
	})
	Register(func(r *Registry) Source {
		urls := r.Config().LeDenicheurFeedURLs
		if len(urls) == 0 {
			return nil
		}
		return &FeedSource{name: "ledenicheur", urls: urls, def: domain.ConditionNew, http: r.HTTP()}
	})
	Register(func(r *Registry) Source {
		urls := r.Config().LeBonCoinFeedURLs
		if len(urls) == 0 {
			return nil
		}
		return &FeedSource{name: "leboncoin", urls: urls, def: domain.ConditionUsed, http: r.HTTP()}
	})
}

type FeedSource struct {
	name string
	urls []string
	def  domain.Condition
	http scraper.Fetcher
}

func (s *FeedSource) Name() string { return s.name }

func (s *FeedSource) Info() SourceInfo {
	desc := "Flux RSS/Atom"
	cats := []string{"rss"}
	switch s.name {
	case "dealabs":
		desc = "Flux RSS Dealabs (deals chauds)"
	case "idealo":
		desc = "Flux RSS Idealo (prix compares)"
	case "ledenicheur":
		desc = "Flux RSS leDenicheur"
	case "leboncoin":
		desc = "Flux RSS leboncoin (occasion)"
	}
	return SourceInfo{
		Name:        s.name,
		Description: desc,
		Categories:  cats,
		Requires:    []string{s.nameUpper() + "_FEED_URLS"},
		Version:     "1",
	}
}

func (s *FeedSource) nameUpper() string {
	out := make([]byte, 0, len(s.name))
	for i := 0; i < len(s.name); i++ {
		c := s.name[i]
		if c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

func (s *FeedSource) Fetch(ctx context.Context) ([]domain.Deal, error) {
	fp := gofeed.NewParser()
	var deals []domain.Deal
	for _, u := range s.urls {
		body, err := s.http.Get(ctx, u)
		if err != nil {
			slog.Warn("feed", "src", s.name, "url", u, "err", err)
			continue
		}
		feed, err := fp.ParseString(body)
		if err != nil {
			slog.Warn("feed parse", "src", s.name, "err", err)
			continue
		}
		for _, it := range feed.Items {
			if d, ok := parseItem(it, s.name, s.def); ok {
				deals = append(deals, d)
			}
		}
	}
	slog.Debug(s.name, "deals", len(deals))
	return deals, nil
}

// feedPriceRE matches a price like "289,99 €", "289.99€", "€289.99", or "289€".
// It requires at least one digit before the currency symbol to avoid matching
// bare "%" or "€" signs without a value.
var feedPriceRE = regexp.MustCompile(`(?i)(?:€\s*(\d{1,3}(?:[\s.,]?\d{3})*(?:[.,]\d{1,2})?)|(\d{1,3}(?:[\s.,]?\d{3})*(?:[.,]\d{1,2})?)\s*€)`)

// feedCapacityRE matches a capacity like "18 To", "18TB", "2 Go", "512GB".
var feedCapacityRE = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(To|TB|Go|GB)\b`)

// feedPackRE detects multi-pack items: "lot de 2", "pack de 3", "x2".
var feedPackRE = regexp.MustCompile(`(?i)(?:lot\s+de|pack\s+de)\s+(\d{1,2})`)

// parseItem extracts a Deal from an RSS/Atom feed entry. It uses regex-based
// extraction for price and capacity to avoid the false positives that the old
// token-scanning approach produced (delivery prices, promo codes, percentages).
func parseItem(it *gofeed.Item, src string, def domain.Condition) (domain.Deal, bool) {
	if it.Title == "" {
		return domain.Deal{}, false
	}
	full := it.Title + " " + it.Description
	lowerFull := strings.ToLower(full)

	// Reject entries that are clearly not disk deals.
	if isNonDiskEntry(lowerFull) {
		return domain.Deal{}, false
	}

	// Detect and strip pack multiplier from price-per-TB computation.
	packMult := 1
	if m := feedPackRE.FindStringSubmatch(lowerFull); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 2 && n <= 24 {
			packMult = n
		}
	}

	priceEUR := extractProductPrice(full)
	if priceEUR <= 0 {
		return domain.Deal{}, false
	}

	tb, ok := extractCapacityTB(full)
	if !ok || tb <= 0 {
		return domain.Deal{}, false
	}

	// For packs, the total capacity is per-unit × count, and price-per-TB
	// should be computed against the total capacity.
	totalTB := tb * float64(packMult)
	pt := priceEUR / totalTB

	lnk := it.Link
	if lnk == "" && it.GUID != "" {
		lnk = it.GUID
	}
	cond := &def
	if c := parsing.NormalizeCondition(full); c != nil {
		cond = c
	}
	deal := domain.Deal{
		Source: src, Title: it.Title, URL: lnk,
		PriceEUR: round2(priceEUR), PricePerTB: round2(pt), CapacityTB: round2(tb),
		Condition: cond, MediaType: normalMedia(full),
		ObservedAt: domain.UTCNow(),
	}
	return deal, true
}

// extractProductPrice finds the most likely product price in the feed text,
// filtering out delivery fees, promo code discounts, and percentage values.
// It returns the first plausible price, preferring exact-euro matches.
func extractProductPrice(text string) float64 {
	matches := feedPriceRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return 0
	}
	// Collect candidate prices, filtering noise.
	var candidates []float64
	for _, m := range matches {
		raw := m[1]
		if raw == "" {
			raw = m[2]
		}
		v := parseFrenchDecimal(raw)
		if v < 0.5 || v > 100000 {
			continue
		}
		// Check the text *before* the price for non-product indicators.
		// Delivery fees ("Livraison 4,99€") have their label before the number,
		// so left-context is the right discriminator.
		idx := strings.Index(text, m[0])
		leftContext := ""
		if idx >= 0 {
			start := idx - 30
			if start < 0 {
				start = 0
			}
			leftContext = strings.ToLower(text[start:idx])
		}
		if isNonProductPrice(leftContext) {
			continue
		}
		candidates = append(candidates, v)
	}
	if len(candidates) == 0 {
		return 0
	}
	// Return the smallest plausible price (dealers usually highlight the
	// product price, but a stray delivery fee can be lower — the context
	// filter above removes those).
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c < best {
			best = c
		}
	}
	return best
}

// extractCapacityTB finds the first capacity value in the text and converts it
// to terabytes.
func extractCapacityTB(text string) (float64, bool) {
	m := feedCapacityRE.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	val := parseFrenchDecimal(m[1])
	if val <= 0 {
		return 0, false
	}
	unit := strings.ToLower(m[2])
	if unit == "go" || unit == "gb" {
		val /= 1000
	}
	// Range-check in TB after conversion, so 512 Go (0.512 TB) passes.
	if val > 200 {
		return 0, false
	}
	return val, true
}

// parseFrenchDecimal converts a number string that may use a comma or dot as
// the decimal separator (French vs English) into a float64.
func parseFrenchDecimal(s string) float64 {
	s = strings.ReplaceAll(s, "\u00a0", "") // non-breaking space
	s = strings.ReplaceAll(s, " ", "")
	hasComma := strings.Contains(s, ",")
	hasDot := strings.Contains(s, ".")
	if hasComma && hasDot {
		// Both present: the rightmost is the decimal separator; the other
		// is a thousands separator.
		if strings.LastIndex(s, ",") > strings.LastIndex(s, ".") {
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else {
			s = strings.ReplaceAll(s, ",", "")
		}
	} else if hasComma {
		s = strings.Replace(s, ",", ".", 1)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// isNonProductPrice returns true if the text around a price match contains
// indicators that the price is NOT the product price (delivery, shipping, etc.).
func isNonProductPrice(context string) bool {
	nonProductIndicators := []string{
		"livraison", "shipping", "delivery", "port",
		"frais", "frais de", "code promo", "code:",
		"remise", "économie", "economie", "économisez",
		"-100%", "crédit", "mensualité", "par mois", "/mois",
	}
	for _, ind := range nonProductIndicators {
		if strings.Contains(context, ind) {
			return true
		}
	}
	return false
}

// isNonDiskEntry returns true if the feed entry clearly does not relate to a
// disk drive (RAM, laptops, accessories, etc.). This reduces wasted parsing.
func isNonDiskEntry(lower string) bool {
	nonDiskTerms := []string{
		"clavier", "souris", "moniteur", "écran", "ecran",
		"carte mère", "carte mere", "alimentation", "boîtier", "boitier",
		"câble hdmi", "cable hdmi", "tapis de souris",
	}
	for _, term := range nonDiskTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}
