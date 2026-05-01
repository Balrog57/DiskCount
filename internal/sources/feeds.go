package sources

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/MarcPartensky/DiskCount/internal/domain"
	"github.com/MarcPartensky/DiskCount/internal/scraper"
	"github.com/mmcdole/gofeed"
)

func init() {
	Register(func(r *Registry) Source {
		urls := r.Config().DealabsRSSURLs; if len(urls) == 0 { return nil }
		return &FeedSource{name: "dealabs", urls: urls, def: domain.ConditionNew, http: r.HTTP()}
	})
	Register(func(r *Registry) Source {
		urls := r.Config().IdealoFeedURLs; if len(urls) == 0 { return nil }
		return &FeedSource{name: "idealo", urls: urls, def: domain.ConditionNew, http: r.HTTP()}
	})
	Register(func(r *Registry) Source {
		urls := r.Config().LeDenicheurFeedURLs; if len(urls) == 0 { return nil }
		return &FeedSource{name: "ledenicheur", urls: urls, def: domain.ConditionNew, http: r.HTTP()}
	})
	Register(func(r *Registry) Source {
		urls := r.Config().LeBonCoinFeedURLs; if len(urls) == 0 { return nil }
		return &FeedSource{name: "leboncoin", urls: urls, def: domain.ConditionUsed, http: r.HTTP()}
	})
}

type FeedSource struct {
	name string; urls []string; def domain.Condition; http *scraper.HTTPFetcher
}

func (s *FeedSource) Name() string { return s.name }

func (s *FeedSource) Fetch(ctx context.Context) ([]domain.Deal, error) {
	fp := gofeed.NewParser(); var deals []domain.Deal
	for _, u := range s.urls {
		body, err := s.http.Get(ctx, u)
		if err != nil { slog.Warn("feed", "src", s.name, "url", u, "err", err); continue }
		feed, err := fp.ParseString(body)
		if err != nil { slog.Warn("feed parse", "src", s.name, "err", err); continue }
		for _, it := range feed.Items {
			if d, ok := parseItem(it, s.name, s.def); ok { deals = append(deals, d) }
		}
	}
	slog.Debug(s.name, "deals", len(deals))
	return deals, nil
}

func parseItem(it *gofeed.Item, src string, def domain.Condition) (domain.Deal, bool) {
	if it.Title == "" { return domain.Deal{}, false }
	full := it.Title + " " + it.Description; t := strings.ToLower(full)
	var priceEUR float64
	for _, w := range strings.Fields(it.Title + " " + it.Description) {
		w = strings.Trim(w, "€() "); w = strings.ReplaceAll(w, ",", "."); w = strings.ReplaceAll(w, "\u00a0", "")
		if v, err := strconv.ParseFloat(w, 64); err == nil && v > 0.5 && v < 100000 {
			if priceEUR == 0 || (v > 0.5 && v < priceEUR) { priceEUR = v }
		}
	}
	if priceEUR <= 0 { return domain.Deal{}, false }
	var tb float64
	for _, p := range []string{"to", "tb", "go", "gb"} {
		if i := strings.LastIndex(t, p); i > 0 {
			before := t[:i]; ws := strings.Fields(before)
			for j := len(ws) - 1; j >= 0; j-- {
				w := strings.ReplaceAll(ws[j], ",", ".")
				if v, err := strconv.ParseFloat(w, 64); err == nil && v > 0.001 && v < 100 {
					tb = v; if p == "go" || p == "gb" { tb /= 1000 }; break
				}
			}
			if tb > 0 { break }
		}
	}
	if tb <= 0 { return domain.Deal{}, false }
	lnk := it.Link; if lnk == "" && it.GUID != "" { lnk = it.GUID }
	pt := priceEUR / tb
	return domain.Deal{
		Source: src, Title: it.Title, URL: lnk,
		PriceEUR: round2(priceEUR), PricePerTB: round2(pt), CapacityTB: round2(tb),
		Condition: &def, MediaType: normalMedia(full),
		ObservedAt: domain.UTCNow(),
	}, true
}
