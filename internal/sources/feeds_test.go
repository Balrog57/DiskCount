package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/mmcdole/gofeed"
)

func item(title, desc, link string) *gofeed.Item {
	return &gofeed.Item{Title: title, Description: desc, Link: link, GUID: link}
}

func TestParseItemDealabsStandard(t *testing.T) {
	it := item(
		"Seagate Exos X20 18TB à 289,99 € chez Amazon",
		"Disque dur HDD 18 To CMR. Bon plan limité dans le temps.",
		"https://dealabs.com/deal/123",
	)
	d, ok := parseItem(it, "dealabs", domain.ConditionNew)
	if !ok {
		t.Fatal("expected deal to be parsed")
	}
	if d.PriceEUR != 289.99 {
		t.Fatalf("price: got %.2f, want 289.99", d.PriceEUR)
	}
	if d.CapacityTB != 18 {
		t.Fatalf("capacity: got %.2f, want 18", d.CapacityTB)
	}
	wantPT := 289.99 / 18
	wantPT = float64(int(wantPT*100)) / 100 // round2 equivalent
	if d.PricePerTB != wantPT {
		t.Fatalf("price/tb: got %.4f, want %.4f", d.PricePerTB, wantPT)
	}
	if d.URL == "" {
		t.Fatal("URL should not be empty")
	}
}

func TestParseItemEURPrefix(t *testing.T) {
	it := item("SSD Samsung 980 2 To", "Prix: €199.99 seulement", "https://l.example/1")
	d, ok := parseItem(it, "dealabs", domain.ConditionNew)
	if !ok {
		t.Fatal("expected deal")
	}
	if d.PriceEUR != 199.99 {
		t.Fatalf("price: got %.2f, want 199.99", d.PriceEUR)
	}
	if d.CapacityTB != 2 {
		t.Fatalf("capacity: got %.2f, want 2", d.CapacityTB)
	}
}

func TestParseItemGoUnit(t *testing.T) {
	it := item("Crucial MX500 512 Go SSD", "À 49,99 €", "https://l.example/2")
	d, ok := parseItem(it, "dealabs", domain.ConditionNew)
	if !ok {
		t.Fatal("expected deal")
	}
	// 512 Go = 0.512 TB, but round2 truncates to 2 decimals = 0.51.
	if d.CapacityTB != 0.51 {
		t.Fatalf("capacity: got %.4f, want 0.51 (512 Go, round2)", d.CapacityTB)
	}
}

func TestParseItemIgnoresDeliveryPrice(t *testing.T) {
	// The entry mentions a delivery fee and a promo code, but the product
	// price should still be extracted correctly.
	it := item(
		"WD Red Plus 12TB 299,99 €",
		"Disponible à 299,99€. Livraison 4,99€. Code promo -10€ pour les nouveaux.",
		"https://l.example/3",
	)
	d, ok := parseItem(it, "dealabs", domain.ConditionNew)
	if !ok {
		t.Fatal("expected deal")
	}
	// The product price (299.99) should win, not delivery (4.99).
	if d.PriceEUR != 299.99 {
		t.Fatalf("price: got %.2f, want 299.99 (not the delivery fee)", d.PriceEUR)
	}
}

func TestParseItemPackMultiplier(t *testing.T) {
	it := item(
		"Lot de 2 disques WD Red 4TB",
		"Pack de 2 disques durs à 240,00 €",
		"https://l.example/4",
	)
	d, ok := parseItem(it, "dealabs", domain.ConditionNew)
	if !ok {
		t.Fatal("expected deal")
	}
	if d.CapacityTB != 4 {
		t.Fatalf("per-unit capacity: got %.2f, want 4", d.CapacityTB)
	}
	// Total capacity is 8 TB, so price-per-TB = 240/8 = 30.
	wantPT := 240.0 / 8.0
	if d.PricePerTB != wantPT {
		t.Fatalf("price/tb (with pack): got %.2f, want %.2f", d.PricePerTB, wantPT)
	}
}

func TestParseItemRejectsNonDisk(t *testing.T) {
	it := item("Clavier mécanique RGB à 49,99 €", "Touche 4 To SSD inclus", "https://l.example/5")
	if _, ok := parseItem(it, "dealabs", domain.ConditionNew); ok {
		t.Fatal("keyboard entry should be rejected")
	}
}

func TestParseItemNoPrice(t *testing.T) {
	it := item("Seagate Exos 18TB", "Juste un titre sans prix", "https://l.example/6")
	if _, ok := parseItem(it, "dealabs", domain.ConditionNew); ok {
		t.Fatal("entry without price should be rejected")
	}
}

func TestParseItemNoCapacity(t *testing.T) {
	it := item("Disque dur Seagate à 99,99 €", "Bon produit", "https://l.example/7")
	if _, ok := parseItem(it, "dealabs", domain.ConditionNew); ok {
		t.Fatal("entry without capacity should be rejected")
	}
}

func TestParseItemLeboncoinUsed(t *testing.T) {
	it := item(
		"Disque dur 16 To occasion",
		"Seagate Exos 16TB utilisé. Prix: 150€",
		"https://leboncoin.fr/123",
	)
	d, ok := parseItem(it, "leboncoin", domain.ConditionUsed)
	if !ok {
		t.Fatal("expected deal")
	}
	if d.PriceEUR != 150 {
		t.Fatalf("price: got %.2f, want 150", d.PriceEUR)
	}
	if d.CapacityTB != 16 {
		t.Fatalf("capacity: got %.2f, want 16", d.CapacityTB)
	}
}

func TestExtractProductPriceMultiple(t *testing.T) {
	// Multiple prices: should pick the product price, not the smaller noise.
	text := "Prix normal 350€, maintenant 289,99 €"
	v := extractProductPrice(text)
	if v != 289.99 {
		t.Fatalf("got %.2f, want 289.99", v)
	}
}

func TestExtractProductPriceThousandsSeparator(t *testing.T) {
	v := extractProductPrice("1.299,00 €")
	if v != 1299.0 {
		t.Fatalf("got %.2f, want 1299.00", v)
	}
}

func TestParseFrenchDecimal(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"289,99", 289.99},
		{"289.99", 289.99},
		{"1.299,00", 1299.0},
		{"1,299.00", 1299.0},
		{"1 299,99", 1299.99},
		{"50", 50},
	}
	for _, c := range cases {
		got := parseFrenchDecimal(c.in)
		if got != c.want {
			t.Errorf("parseFrenchDecimal(%q) = %.4f, want %.4f", c.in, got, c.want)
		}
	}
}

// TestFeedSourceAllURLsFailReturnsTransient verifies that when every feed URL
// is unreachable, FeedSource.Fetch bubbles up a typed Transient error so the
// per-source circuit breaker can count the outage. Previously the loop
// swallowed all errors and returned (nil, nil), which left the breaker closed
// and only the slower zero-streak health monitor could flag the dead source.
func TestFeedSourceAllURLsFailReturnsTransient(t *testing.T) {
	srv := new500Server(t)
	defer srv.Close()

	fetcher := newTestHTTPFetcher(t)
	s := &FeedSource{
		name: "dealabs",
		urls: []string{srv.URL, srv.URL + "/x"},
		def:  domain.ConditionNew,
		http: fetcher,
	}
	deals, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected Transient error when all feed URLs fail, got nil")
	}
	if len(deals) != 0 {
		t.Fatalf("expected 0 deals, got %d", len(deals))
	}
	if Classify(err) != SeverityTransient {
		t.Fatalf("expected SeverityTransient, got %v (err=%v)", Classify(err), err)
	}
}

// TestFeedSourcePartialFailureNoError verifies that a single failing URL among
// several does NOT bubble up as an error — only a total outage trips the
// breaker. The successful URL must still produce deals.
func TestFeedSourcePartialFailureNoError(t *testing.T) {
	bad := new500Server(t)
	defer bad.Close()
	good := newFeedServer(t, "Seagate Exos 18TB à 289,99 €")
	defer good.Close()

	fetcher := newTestHTTPFetcher(t)
	s := &FeedSource{
		name: "dealabs",
		urls: []string{bad.URL, good.URL},
		def:  domain.ConditionNew,
		http: fetcher,
	}
	deals, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("partial failure should not return error, got %v", err)
	}
	if len(deals) != 1 {
		t.Fatalf("expected 1 deal from the good URL, got %d", len(deals))
	}
}

// new500Server returns a test server that always responds 500.
func new500Server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

// newFeedServer returns a test server that serves a minimal RSS 2.0 feed
// containing a single item with the given title.
func newFeedServer(t *testing.T, itemTitle string) *httptest.Server {
	t.Helper()
	body := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
<title>Test feed</title><link>http://test</link><description>test</description>
<item><title>` + itemTitle + `</title><link>http://test/1</link><description>p</description></item>
</channel></rss>`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Write([]byte(body))
	}))
}
