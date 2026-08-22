package web

import (
	"net/url"
	"strings"

	"github.com/Balrog57/DiskCount/internal/db"
)

type countryInfo struct{ Code, Name, Flag string }

type regionalOffer struct {
	db.CurrentPrice
	Country countryInfo
}

type regionSummary struct {
	Country        countryInfo
	Offers         int
	BestPricePerTB float64
}

var europeanCountries = []struct {
	suffix string
	info   countryInfo
}{
	{".fr", countryInfo{"fr", "France", "🇫🇷"}},
	{".de", countryInfo{"de", "Allemagne", "🇩🇪"}},
	{".es", countryInfo{"es", "Espagne", "🇪🇸"}},
	{".it", countryInfo{"it", "Italie", "🇮🇹"}},
	{".nl", countryInfo{"nl", "Pays-Bas", "🇳🇱"}},
	{".be", countryInfo{"be", "Belgique", "🇧🇪"}},
	{".at", countryInfo{"at", "Autriche", "🇦🇹"}},
	{".co.uk", countryInfo{"uk", "Royaume-Uni", "🇬🇧"}},
}

func countryForOffer(p db.CurrentPrice) (countryInfo, bool) {
	u, err := url.Parse(p.URL)
	if err == nil {
		host := strings.ToLower(u.Hostname())
		for _, candidate := range europeanCountries {
			if strings.HasSuffix(host, candidate.suffix) {
				return candidate.info, true
			}
		}
	}
	// These .com merchants are French storefronts; unknown .com hosts stay
	// excluded because guessing a country would make the comparison deceptive.
	switch strings.ToLower(p.Source) {
	case "dealabs", "grosbill", "ldlc", "topachat":
		return countryInfo{"fr", "France", "🇫🇷"}, true
	}
	return countryInfo{}, false
}

func regionalize(prices []db.CurrentPrice, selected string, limit int) ([]regionalOffer, []regionSummary) {
	if limit <= 0 {
		limit = 200
	}
	var offers []regionalOffer
	byCode := map[string]int{}
	var summaries []regionSummary
	for _, price := range prices {
		if string(price.Availability) == "unavailable" {
			continue
		}
		country, ok := countryForOffer(price)
		if !ok {
			continue
		}
		i, exists := byCode[country.Code]
		if !exists {
			i = len(summaries)
			byCode[country.Code] = i
			summaries = append(summaries, regionSummary{Country: country, BestPricePerTB: price.PricePerTB})
		}
		summaries[i].Offers++
		if (selected == "" || selected == country.Code) && len(offers) < limit {
			offers = append(offers, regionalOffer{CurrentPrice: price, Country: country})
		}
	}
	return offers, summaries
}
