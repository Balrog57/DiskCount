package web

import (
	"github.com/Balrog57/DiskCount/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFProtection(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)
	rec := httptest.NewRecorder()

	// Create a POST request (simulating state mutation)
	req := httptest.NewRequest(http.MethodPost, "/alerts/toggle", strings.NewReader("owner_user_id=1&alert_id=1&enabled=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", "secret")

	// Malicious request (Wrong Origin)
	req.Header.Set("Origin", "http://evil.com")
	req.Host = "diskcount.local"

	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for CSRF, got %d", rec.Code)
	}

	// Valid request (Same Origin)
	// We use srv.routes().ServeHTTP instead of handler to bypass outer layers or
	// better yet, we just test a request that we know will 400 before DB access
	// like missing required form fields
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/alerts/toggle", strings.NewReader("bad_data=1"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.SetBasicAuth("admin", "secret")
	req2.Header.Set("Origin", "http://diskcount.local")
	req2.Host = "diskcount.local"

	srv.handler().ServeHTTP(rec2, req2)

	if rec2.Code == http.StatusForbidden {
		t.Fatalf("did not expect 403 Forbidden for valid CSRF")
	}
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec2.Code)
	}
}
