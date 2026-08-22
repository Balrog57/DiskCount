package scraper

import "testing"

func TestParseJSONLDSingleProduct(t *testing.T) {
	html := `<html><head>
<script type="application/ld+json">
{
  "@type": "Product",
  "name": "Seagate Exos X20 18TB",
  "sku": "ST18000NM000J",
  "brand": {"@type": "Brand", "name": "Seagate"},
  "offers": {
    "@type": "Offer",
    "price": "289.99",
    "priceCurrency": "EUR",
    "availability": "https://schema.org/InStock",
    "url": "https://example.com/exos"
  }
}</script>
</head><body></body></html>`

	pd, ok := ParseJSONLD(html)
	if !ok {
		t.Fatal("expected product to be found")
	}
	if pd.Name != "Seagate Exos X20 18TB" {
		t.Fatalf("name: got %q", pd.Name)
	}
	if pd.Brand != "Seagate" {
		t.Fatalf("brand: got %q", pd.Brand)
	}
	if pd.SKU != "ST18000NM000J" {
		t.Fatalf("sku: got %q", pd.SKU)
	}
	if pd.Price != "289.99" {
		t.Fatalf("price: got %q", pd.Price)
	}
	if pd.Currency != "EUR" {
		t.Fatalf("currency: got %q", pd.Currency)
	}
	if pd.Availability != "https://schema.org/InStock" {
		t.Fatalf("availability: got %q", pd.Availability)
	}
}

func TestParseJSONLDImageString(t *testing.T) {
	pd, ok := ParseJSONLD(`<script type="application/ld+json">{"@type":"Product","image":"https://example.com/disk.jpg"}</script>`)
	if !ok || pd.Image != "https://example.com/disk.jpg" {
		t.Fatalf("image: got %q, found=%v", pd.Image, ok)
	}
}

func TestParseJSONLDImageArray(t *testing.T) {
	pd, ok := ParseJSONLD(`<script type="application/ld+json">{"@type":"Product","image":[{"url":"https://example.com/one.jpg"},"https://example.com/two.jpg"]}</script>`)
	if !ok || pd.Image != "https://example.com/one.jpg" {
		t.Fatalf("image: got %q, found=%v", pd.Image, ok)
	}
}

func TestParseJSONLDGraphWrapper(t *testing.T) {
	html := `<html><head>
<script type="application/ld+json">
{
  "@context": "https://schema.org",
  "@graph": [
    {"@type": "WebSite", "name": "LDLC"},
    {"@type": "Product", "name": "IronWolf Pro 16TB", "brand": "Seagate", "offers": {"price": "320", "priceCurrency": "EUR"}}
  ]
}
</script>
</head></html>`

	pd, ok := ParseJSONLD(html)
	if !ok {
		t.Fatal("expected product inside @graph")
	}
	if pd.Name != "IronWolf Pro 16TB" {
		t.Fatalf("name: got %q", pd.Name)
	}
	if pd.Brand != "Seagate" {
		t.Fatalf("brand: got %q", pd.Brand)
	}
	if pd.Price != "320" {
		t.Fatalf("price: got %q", pd.Price)
	}
}

func TestParseJSONLDArrayOffers(t *testing.T) {
	html := `<html><head>
<script type="application/ld+json">
{"@type": "Product", "name": "WD Red 8TB", "offers": [
  {"price": "150", "priceCurrency": "EUR"},
  {"price": "140", "priceCurrency": "EUR"}
]}
</script>
</head></html>`

	pd, ok := ParseJSONLD(html)
	if !ok {
		t.Fatal("expected product with array offers")
	}
	if pd.Price != "150" {
		t.Fatalf("expected first offer price 150, got %q", pd.Price)
	}
}

func TestParseJSONLDBrandAsString(t *testing.T) {
	html := `<script type="application/ld+json">
{"@type": "Product", "name": "SSD", "brand": "Crucial"}
</script>`
	pd, ok := ParseJSONLD(html)
	if !ok {
		t.Fatal("expected product")
	}
	if pd.Brand != "Crucial" {
		t.Fatalf("brand: got %q", pd.Brand)
	}
}

func TestParseJSONLDMultipleBlocks(t *testing.T) {
	html := `<html><head>
<script type="application/ld+json">{"@type": "BreadcrumbList", "name": "x"}</script>
<script type="application/ld+json">{"@type": "Product", "name": "MX500 2TB", "brand": "Crucial", "offers": {"price":"99","priceCurrency":"EUR"}}</script>
</head></html>`

	pd, ok := ParseJSONLD(html)
	if !ok {
		t.Fatal("expected product from second block")
	}
	if pd.Name != "MX500 2TB" {
		t.Fatalf("name: got %q", pd.Name)
	}
}

func TestParseJSONLDNoProduct(t *testing.T) {
	html := `<script type="application/ld+json">{"@type": "WebPage", "name": "Home"}</script>`
	_, ok := ParseJSONLD(html)
	if ok {
		t.Fatal("expected no product found")
	}
}

func TestParseJSONLDEmptyHTML(t *testing.T) {
	_, ok := ParseJSONLD("")
	if ok {
		t.Fatal("expected no product from empty HTML")
	}
}

func TestParseJSONLDMPN(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"Product","name":"Exos","mpn":"ST18000NM000J","sku":"shop-123"}</script>`
	pd, ok := ParseJSONLD(html)
	if !ok || pd.MPN != "ST18000NM000J" {
		t.Fatalf("mpn: got %q found=%v", pd.MPN, ok)
	}
}

func TestParseJSONLDArrayTypeField(t *testing.T) {
	// Some sites put @type as an array including Product.
	html := `<script type="application/ld+json">
{"@type": ["Product", "IndividualProduct"], "name": "Test", "offers": {"price":"10"}}
</script>`
	pd, ok := ParseJSONLD(html)
	if !ok {
		t.Fatal("expected product with array @type")
	}
	if pd.Name != "Test" {
		t.Fatalf("name: got %q", pd.Name)
	}
}
