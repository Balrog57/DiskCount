package sources

import "testing"

func TestAmazonImageURL(t *testing.T) {
	if got := amazonImageURL(" b012345678 "); got != "https://m.media-amazon.com/images/P/B012345678.jpg" {
		t.Fatalf("amazonImageURL: got %q", got)
	}
	if got := amazonImageURL("short"); got != "" {
		t.Fatalf("invalid ASIN: got %q", got)
	}
}
