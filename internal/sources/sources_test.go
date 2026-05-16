package sources

import "testing"

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
