package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
)

func TestWithCSRF(t *testing.T) {
	srv := &Server{}
	handler := srv.withCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	// Test GET request (allowed without headers)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for GET request, got %d", rec.Code)
	}

	// Test POST request without headers (blocked)
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.Host = "example.com"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for POST without headers, got %d", rec.Code)
	}

	// Test POST request with mismatched Origin (blocked)
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://attacker.com")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for POST with mismatched Origin, got %d", rec.Code)
	}

	// Test POST request with matching Origin (allowed)
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for POST with matching Origin, got %d", rec.Code)
	}

	// Test POST request with matching Referer (allowed)
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.Host = "example.com"
	req.Header.Set("Referer", "http://example.com/some/path")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for POST with matching Referer, got %d", rec.Code)
	}
}

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
