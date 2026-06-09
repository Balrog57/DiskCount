package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
)

func TestConfigTemplateMasksSecret(t *testing.T) {
	rec := httptest.NewRecorder()
	render(rec, configTpl, map[string]any{
		"Title":  "Configuration",
		"Active": "config",
		"Rows": []configRow{{
			Meta:  config.SettingMeta{Key: "TELEGRAM_BOT_TOKEN", Label: "Token", Secret: true},
			Value: "masked-sensitive-fixture",
		}},
		"RestartMsg": "restart",
	})
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("unexpected status %d: %s", rec.Code, body)
	}
	if strings.Contains(body, "masked-sensitive-fixture") {
		t.Fatalf("secret leaked in template: %s", body)
	}
	if !strings.Contains(body, "********") {
		t.Fatalf("masked secret placeholder missing: %s", body)
	}
}

func TestUsersTemplateDoesNotExposeAdminControls(t *testing.T) {
	rec := httptest.NewRecorder()
	render(rec, usersTpl, map[string]any{
		"Title":  "Utilisateurs",
		"Active": "users",
	})
	body := rec.Body.String()
	for _, forbidden := range []string{"is_admin", ">Role<", ">admin<"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("admin control leaked in users template: %q", forbidden)
		}
	}
}

func TestAlertsTemplateDoesNotCreateAlerts(t *testing.T) {
	rec := httptest.NewRecorder()
	render(rec, alertsTpl, map[string]any{
		"Title":  "Alertes",
		"Active": "alerts",
		"Alerts": []alertRow{{
			Alert: db.Alert{ID: 42, OwnerUserID: 84, Name: "Fixture", Enabled: true},
			Owner: "Owner",
		}},
	})
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("unexpected status %d: %s", rec.Code, body)
	}
	for _, required := range []string{"/alerts/toggle", "/alerts/delete", "Pause", "Supprimer"} {
		if !strings.Contains(body, required) {
			t.Fatalf("missing alert action %q", required)
		}
	}
	for _, forbidden := range []string{"/alerts/add", "Creer une alerte", "draft:start"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("alert creation leaked in web template: %q", forbidden)
		}
	}
}

func TestProductsTemplateRendersFilters(t *testing.T) {
	rec := httptest.NewRecorder()
	render(rec, productsTpl, map[string]any{
		"Title":   "Produits",
		"Active":  "products",
		"Sources": []string{"diskprices"},
	})
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("unexpected status %d: %s", rec.Code, body)
	}
	for _, required := range []string{"name=\"source\"", "name=\"media\"", "name=\"max_eur_tb\""} {
		if !strings.Contains(body, required) {
			t.Fatalf("missing product filter %q", required)
		}
	}
}

func TestRoutesRejectUnsupportedMethodsBeforeDBUse(t *testing.T) {
	srv := New(nil, nil, &config.Config{}, nil, false)
	for _, path := range []string{"/alerts/toggle", "/alerts/delete", "/config/save", "/users/add", "/users/toggle"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: expected 405, got %d", path, rec.Code)
		}
	}
}

func TestCSRFProtection(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "test"}, nil, false)
	handler := srv.withAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		method     string
		origin     string
		referer    string
		host       string
		wantStatus int
	}{
		{"GET allowed without headers", "GET", "", "", "example.com", 200},
		{"POST missing origin/referer", "POST", "", "", "example.com", 403},
		{"POST origin matches host", "POST", "https://example.com", "", "example.com", 200},
		{"POST referer matches host", "POST", "", "https://example.com/path", "example.com", 200},
		{"POST origin mismatch", "POST", "https://evil.com", "", "example.com", 403},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", nil)
			req.Host = tc.host
			req.SetBasicAuth("admin", "test")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}
