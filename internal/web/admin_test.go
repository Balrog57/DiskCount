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

func TestCSRFMiddleware(t *testing.T) {
	srv := New(nil, nil, &config.Config{}, nil, false)

	// Test POST without Origin/Referer
	req := httptest.NewRequest(http.MethodPost, "/not-found", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", rec.Code)
	}

	// Test POST with incorrect Origin
	req = httptest.NewRequest(http.MethodPost, "/not-found", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://evil.com")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", rec.Code)
	}

	// Test POST with correct Origin
	req = httptest.NewRequest(http.MethodPost, "/not-found", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "https://example.com")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	// Because /not-found doesn't exist, mux will return 404
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", rec.Code)
	}

	// Test GET without Origin (should pass CSRF, hit 404)
	req = httptest.NewRequest(http.MethodGet, "/not-found", nil)
	req.Host = "example.com"
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", rec.Code)
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
