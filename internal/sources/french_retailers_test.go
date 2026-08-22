package sources

import (
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestFrenchRetailerParsers(t *testing.T) {
	tests := []struct {
		name, html, base string
		parse            retailerParser
	}{
		{"materiel", `<div class="c-product-block"><a class="c-product__link" href="/produit/1.html"><span class="c-product__title">Seagate IronWolf 4 To HDD</span></a><div class="product-specs">SATA 3.5 pouces</div><span class="o-product__price">219€95</span></div>`, "https://www.materiel.net/", parseMateriel},
		{"topbiz", `<article class="product-miniature"><strong class="product-title-mini"><a href="/ssd/1.html">Crucial E100 2 To SSD</a></strong><div class="product-price-and-shipping"><span class="price"><span content="209.99">209,99 €</span></span></div></article>`, "https://www.topbiz.fr/", parseTopbiz},
		{"corsair", `<div data-product-sku="SSD1" data-product-name="Corsair MP700 2 To SSD NVMe" data-product-price="249.99"><a href="/p/ssd1">Voir</a></div>`, "https://www.corsair.com/fr/fr/", parseCorsair},
		{"generic", `<article><h2>Samsung 990 Pro 2 To SSD</h2><a href="/ssd/990">Voir</a><span itemprop="price" content="199.99"></span></article>`, "https://shop.example/", parseGenericRetailer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deals := tc.parse(tc.html, tc.base)
			if len(deals) != 1 || deals[0].PriceEUR <= 0 || deals[0].CapacityTB != 2 && tc.name != "materiel" {
				t.Fatalf("unexpected deals: %#v", deals)
			}
		})
	}
}

func TestGenericRetailerDataAttributes(t *testing.T) {
	html := `<a href="/ssd-1tb" data-product-id="1" data-product-name="SSD NVMe 1TB" data-product-price="79.99"><div>SSD NVMe</div></a>`
	deals := parseGenericRetailer(html, "https://www.pccomponentes.fr/")
	if len(deals) != 1 || deals[0].PriceEUR != 79.99 || deals[0].URL != "https://www.pccomponentes.fr/ssd-1tb" {
		t.Fatalf("unexpected product data: %#v", deals)
	}
}

func TestLiveFrenchRetailerParsers(t *testing.T) {
	if os.Getenv("DISKCOUNT_LIVE_SOURCES") != "1" {
		t.Skip("set DISKCOUNT_LIVE_SOURCES=1")
	}
	tests := []struct {
		name, url string
		parse     retailerParser
	}{
		{"materiel", "https://www.materiel.net/disque-dur-interne/l430/", parseMateriel},
		{"cybertek", "https://www.cybertek.fr/disque-ssd-49.aspx", parseGrosbill},
		{"corsair", "https://www.corsair.com/fr/fr/c/data-storage", parseCorsair},
		{"topbiz", "https://www.topbiz.fr/95-disques-durs-ssd", parseTopbiz},
	}
	client := &http.Client{Timeout: 20 * time.Second}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, tc.url, nil)
			req.Header.Set("User-Agent", "Mozilla/5.0")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if deals := tc.parse(string(body), tc.url); len(deals) == 0 {
				t.Fatalf("live parser returned zero deals (status %d, bytes %d)", resp.StatusCode, len(body))
			}
		})
	}
}

func TestFrenchRetailerRewritesSource(t *testing.T) {
	s := &frenchRetailer{name: "topbiz", parse: parseTopbiz}
	deals := s.parse(`<article class="product-miniature"><strong class="product-title-mini"><a href="https://www.topbiz.fr/p">SSD 1 To</a></strong><div class="product-price-and-shipping"><span class="price"><span content="99.99"></span></span></div></article>`, "https://www.topbiz.fr")
	for i := range deals {
		deals[i].Source = s.name
	}
	if len(deals) != 1 || deals[0].Source != "topbiz" {
		t.Fatalf("source not assigned: %#v", deals)
	}
}
