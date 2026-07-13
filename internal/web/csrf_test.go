package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Balrog57/DiskCount/internal/config"
)

func TestCSRFProtectionRejectsMismatchedOrigin(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/alerts/toggle", strings.NewReader("alert_id=1&enabled=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.com")

	// Add valid session
	req.SetBasicAuth("admin", "secret")

	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected CSRF block (403), got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCSRFProtectionRejectsNullOrigin(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/alerts/toggle", strings.NewReader("alert_id=1&enabled=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "null")

	req.SetBasicAuth("admin", "secret")

	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected CSRF block for null origin (403), got %d", rec.Code)
	}
}

func TestCSRFProtectionAllowsValidOrigin(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/alerts/toggle", strings.NewReader("alert_id=1&enabled=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "admin.example.com"
	req.Header.Set("Origin", "https://admin.example.com")

	req.SetBasicAuth("admin", "secret")

	// Since we mock it, we just want to ensure it doesn't fail on CSRF,
	// it might fail with 500/etc because db is nil, but not 403.
	srv.handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("expected valid origin to pass CSRF check, got %d", rec.Code)
	}
}
