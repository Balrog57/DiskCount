package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestParsePricePerTBRequiresTitleAndURL(t *testing.T) {
	html := `<table>
<tr class="disk" data-product-type="internal_hdd" data-condition="new" data-capacity="10000.0"><td class="price-per-gb hidden">€0,015</td><td class="price-per-tb">€15,00</td><td>€150</td><td>10 TB</td><td>5 years</td><td>Internal 3.5&quot;</td><td>HDD</td><td>New</td><td class="name"><a href="/drive">Seagate 10 To SATA</a></td></tr>
<tr><td class="price-per-gb hidden">€0,001</td><td class="price-per-tb">€1,00</td><td>€1</td><td>1 TB</td><td>-</td><td>Internal 3.5&quot;</td><td>HDD</td><td>New</td><td></td></tr>
</table>`
	deals := parsePTB(html, "https://pricepertb.test/")
	if len(deals) != 1 {
		t.Fatalf("expected one valid deal, got %d: %#v", len(deals), deals)
	}
	if deals[0].Title == "" || deals[0].URL == "" {
		t.Fatalf("invalid deal accepted: %#v", deals[0])
	}
	if deals[0].URL != "https://pricepertb.test/drive" {
		t.Fatalf("relative URL not resolved: %q", deals[0].URL)
	}
	if deals[0].PriceEUR != 150 || deals[0].PricePerTB != 15 || deals[0].CapacityTB != 10 {
		t.Fatalf("price/capacity mismatch: %#v", deals[0])
	}
}

func TestParseDiskPricesClassifiesExternal25(t *testing.T) {
	html := `<table><tr>
<td>1</td><td>x</td><td>49,99 €</td><td>1 To</td><td>x</td><td>Portable 2,5"</td><td>HDD USB</td><td>New</td><td><a href="https://www.amazon.fr/dp/B012345678">Drive portable</a></td>
</tr></table>`
	deals, err := parseDiskPrices(html)
	if err != nil {
		t.Fatal(err)
	}
	if len(deals) != 1 {
		t.Fatalf("expected one deal, got %d", len(deals))
	}
	if deals[0].DriveCategory == nil || *deals[0].DriveCategory != "external_2_5" {
		t.Fatalf("expected external_2_5, got %#v", deals[0].DriveCategory)
	}
}

func TestFeedSourceInfoDescribesSource(t *testing.T) {
	s := &FeedSource{name: "dealabs", def: domain.ConditionNew}
	info := s.Info()
	if info.Name != "dealabs" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	if info.Description == "" {
		t.Fatal("info.Description should not be empty")
	}
	if len(info.Requires) == 0 || info.Requires[0] == "" {
		t.Fatal("info.Requires should list at least one env key")
	}
}

func TestDiskPricesInfo(t *testing.T) {
	s := &DiskPrices{url: "https://example/"}
	info := s.Info()
	if info.Name != "diskprices" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	if len(info.Categories) == 0 {
		t.Fatal("expected at least one category")
	}
}

func TestPricePerGigInfo(t *testing.T) {
	s := &PricePerGig{apiURL: "https://x"}
	info := s.Info()
	if info.Name != "pricepergig" {
		t.Fatalf("info.Name = %q", info.Name)
	}
}

func TestPricePerTBInfo(t *testing.T) {
	s := &PricePerTB{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "pricepertb" {
		t.Fatalf("info.Name = %q", info.Name)
	}
}

func TestNewTestFetcher(t *testing.T) {
	srv := httptest.NewServer(MustStatusHandler(200, "ok"))
	defer srv.Close()
	fetcher := NewTestFetcher(t, srv)
	if fetcher == nil {
		t.Fatal("nil fetcher")
	}
	body := FetchThroughServer(t, srv, fetcher, "/")
	if body != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
}

func TestNewTestFetcherWithContext(t *testing.T) {
	// The helper is safe to call without a server when only used to
	// construct a fetcher; the nil server is accepted via the leading
	// underscore and is otherwise unused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	f := NewTestFetcher(t, srv)
	if f == nil {
		t.Fatal("nil fetcher")
	}
	// A simple context-canceled call should not panic.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = f.Get(ctx, srv.URL+"/")
}
