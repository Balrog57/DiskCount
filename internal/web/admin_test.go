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

func TestLoginPageIsServedWithoutBasicAuth(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("Accept", "text/html")
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login GET: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `name="password"`) {
		t.Fatalf("login form missing password field: %s", body)
	}
	if !strings.Contains(body, `autocomplete="current-password"`) {
		t.Fatalf("login form should hint password managers: %s", body)
	}
}

func TestProtectedPathRedirectsToLoginWhenNoSession(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/alerts", nil)
	req.Header.Set("Accept", "text/html")
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/login") {
		t.Fatalf("expected redirect to /login, got %q", loc)
	}
}

func TestLoginPostSetsSessionAndReachesDashboard(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("password=secret&next=%2Falerts"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login POST: expected 303, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Location") != "/alerts" {
		t.Fatalf("login redirect target = %q", rec.Header().Get("Location"))
	}
	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, sessionCookieName+"=") {
		t.Fatalf("expected session cookie, got %q", cookie)
	}

	var nextCalled bool
	guarded := srv.withAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/alerts", nil)
	req2.Header.Set("Accept", "text/html")
	for _, c := range rec.Result().Cookies() {
		req2.AddCookie(c)
	}
	guarded.ServeHTTP(rec2, req2)
	if !nextCalled {
		t.Fatalf("session cookie did not grant access: status=%d location=%q", rec2.Code, rec2.Header().Get("Location"))
	}
}

func TestLoginPostRejectsBadPassword(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("password=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login POST bad pass: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Mot de passe incorrect") {
		t.Fatalf("expected error message in body")
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("failed login should not set a session cookie")
	}
}

func TestJSONEndpointStillSpeaksBasicAuth(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)
	guarded := srv.withAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth("admin", "secret")
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("basic auth rejected on JSON endpoint: %d %s", rec.Code, rec.Body.String())
	}
}

func TestJSONEndpointRejectsAnonymous(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)
	guarded := srv.withAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not run for anonymous JSON request")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	req.Header.Set("Accept", "application/json")
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("expected WWW-Authenticate header")
	}
}

func TestLogoutClearsSession(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("password=secret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login failed: %d", rec.Code)
	}
	cookies := rec.Result().Cookies()

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/logout", nil)
	for _, c := range cookies {
		req2.AddCookie(c)
	}
	srv.handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusSeeOther {
		t.Fatalf("logout: expected 303, got %d", rec2.Code)
	}
	cleared := rec2.Header().Get("Set-Cookie")
	if !strings.Contains(cleared, sessionCookieName+"=") || !strings.Contains(cleared, "Max-Age=0") && !strings.Contains(cleared, "expires=") {
		t.Fatalf("expected cookie clearing, got %q", cleared)
	}
}
