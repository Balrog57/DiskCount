package web

import (
	"testing"

	"github.com/Balrog57/DiskCount/internal/db"
)

func TestRegionalizeUsesReliableHostOrMerchant(t *testing.T) {
	prices := []db.CurrentPrice{
		{Source: "pricepergig", URL: "https://www.amazon.de/dp/A", PricePerTB: 12},
		{Source: "ldlc", URL: "https://www.ldlc.com/fiche/B", PricePerTB: 15},
		{Source: "unknown", URL: "https://example.com/C", PricePerTB: 1},
	}
	offers, summaries := regionalize(prices, "", 10)
	if len(offers) != 2 || len(summaries) != 2 || offers[0].Country.Code != "de" || offers[1].Country.Code != "fr" {
		t.Fatalf("unexpected regional catalogue: offers=%#v summaries=%#v", offers, summaries)
	}
	filtered, _ := regionalize(prices, "fr", 10)
	if len(filtered) != 1 || filtered[0].Source != "ldlc" {
		t.Fatalf("country filter failed: %#v", filtered)
	}
}
