package rules

import (
	"time"
	"github.com/MarcPartensky/DiskCount/internal/db"
	"github.com/MarcPartensky/DiskCount/internal/domain"
)

type Preset struct {
	Label     string
	MinTB     *float64
	MaxTB     *float64
	MediaType string
}

var CapacityPresets = map[string]Preset{
	"all":        {"Toute capacite", nil, nil, "all"},
	"ssd_lt_256": {"SSD <256 Go", nil, pf(0.256), "solid_state"},
	"ssd_256":    {"SSD ~256 Go", pf(0.24), pf(0.30), "solid_state"},
	"ssd_512":    {"SSD ~512 Go", pf(0.48), pf(0.60), "solid_state"},
	"ssd_1":      {"SSD ~1 To", pf(0.9), pf(1.2), "solid_state"},
	"ssd_2":      {"SSD ~2 To", pf(1.8), pf(2.4), "solid_state"},
	"ssd_4":      {"SSD ~4 To", pf(3.6), pf(4.8), "solid_state"},
	"ssd_gt_4":   {"SSD >4 To", pf(4.0), nil, "solid_state"},
	"hdd_lt_4":   {"HDD <4 To", nil, pf(4.0), "rotational"},
	"hdd_4_8":    {"HDD 4-8 To", pf(4.0), pf(8.0), "rotational"},
	"hdd_8_12":   {"HDD 8-12 To", pf(8.0), pf(12.0), "rotational"},
	"hdd_12_16":  {"HDD 12-16 To", pf(12.0), pf(16.0), "rotational"},
	"hdd_16_20":  {"HDD 16-20 To", pf(16.0), pf(20.0), "rotational"},
	"hdd_20_24":  {"HDD 20-24 To", pf(20.0), pf(24.0), "rotational"},
	"hdd_24_30":  {"HDD 24-30 To", pf(24.0), pf(30.0), "rotational"},
	"hdd_gt_30":  {"HDD >30 To", pf(30.0), nil, "rotational"},
}

func pf(v float64) *float64 { return &v }

func AlertMatches(alert *db.Alert, deal domain.Deal) bool {
	if len(alert.Sources) > 0 && !cont(alert.Sources, deal.Source) { return false }
	if len(alert.Conditions) > 0 && (deal.Condition == nil || !cont(alert.Conditions, string(*deal.Condition))) { return false }
	if len(alert.MediaTypes) > 0 && (deal.MediaType == nil || !cont(alert.MediaTypes, string(*deal.MediaType))) { return false }
	if len(alert.DriveCategories) > 0 && (deal.DriveCategory == nil || !cont(alert.DriveCategories, string(*deal.DriveCategory))) { return false }
	if len(alert.Interfaces) > 0 {
		m := false; for _, di := range deal.Interfaces { if cont(alert.Interfaces, string(di)) { m = true; break } }; if !m { return false }
	}
	if !capMatch(alert, deal.CapacityTB) { return false }
	if alert.MaxPricePerTB != nil && deal.PricePerTB > *alert.MaxPricePerTB { return false }
	return true
}

func capMatch(a *db.Alert, tb float64) bool {
	if len(a.CapacityPresets) > 0 {
		for _, k := range a.CapacityPresets {
			p, ok := CapacityPresets[k]; if !ok { continue }
			if p.MinTB != nil && tb < *p.MinTB { continue }
			if p.MaxTB != nil && tb > *p.MaxTB { continue }
			return true
		}
		return false
	}
	if a.MinCapacityTB != nil && tb < *a.MinCapacityTB { return false }
	if a.MaxCapacityTB != nil && tb > *a.MaxCapacityTB { return false }
	return true
}

func ShouldNotify(alert *db.Alert, deal domain.Deal, baseline *float64, last *db.Notification, now time.Time, sigDrop float64) domain.NotificationDecision {
	var disc *float64; dHit := false
	tHit := alert.MaxPricePerTB != nil && deal.PricePerTB <= *alert.MaxPricePerTB
	if baseline != nil && *baseline > 0 {
		f := 1.0 - alert.MinDiscountPct/100; dHit = deal.PricePerTB <= (*baseline * f)
		p := ((*baseline - deal.PricePerTB) / *baseline) * 100; disc = &p
	}
	if !tHit && !dHit { return domain.NotificationDecision{ShouldNotify: false, Reason: "no_threshold", DiscountPct: disc, BaselinePricePerTB: baseline} }
	if last != nil {
		cd := last.SentAt.Add(time.Duration(alert.CooldownHours) * time.Hour)
		df := 1.0 - sigDrop/100; drop := deal.PricePerTB <= (last.PricePerTB * df)
		if now.Before(cd) && !drop { return domain.NotificationDecision{ShouldNotify: false, Reason: "cooldown", DiscountPct: disc, BaselinePricePerTB: baseline} }
	}
	r := "max_price_per_tb"; if !tHit { r = "rolling_discount" }
	return domain.NotificationDecision{ShouldNotify: true, Reason: r, DiscountPct: disc, BaselinePricePerTB: baseline}
}

func cont[T comparable](s []T, v T) bool { for _, x := range s { if x == v { return true } }; return false }
