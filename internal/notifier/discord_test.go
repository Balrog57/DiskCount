package notifier

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestDiscordSendsOnlyDealMessageWithoutMentions(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	d := NewDiscord("secret", "42")
	d.baseURL, d.client = srv.URL, srv.Client()
	err := d.SendDeal(&db.Alert{Name: "NAS"}, domain.Deal{Title: "@everyone IronWolf", Source: "test", URL: "https://example.test", CapacityTB: 16, PriceEUR: 280, PricePerTB: 17.5}, domain.NotificationDecision{})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bot secret" || !strings.Contains(gotBody, `"parse":[]`) || !strings.Contains(gotBody, "IronWolf") {
		t.Fatalf("unexpected Discord request: auth=%q body=%s", gotAuth, gotBody)
	}
}
