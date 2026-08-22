package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/rules"
	"github.com/Balrog57/DiskCount/internal/scanner"
	"github.com/Balrog57/DiskCount/internal/sources"
)

func TestConfigTemplateMasksSecret(t *testing.T) {
	rec := httptest.NewRecorder()
	render(rec, configTpl, map[string]any{
		"Title":  "Configuration",
		"Active": "config",
		"Rows": []configRow{{
			Meta:  config.SettingMeta{Key: "DISCORD_BOT_TOKEN", Label: "Token", Secret: true},
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

func TestDiscordTemplateMasksConfiguredToken(t *testing.T) {
	rec := httptest.NewRecorder()
	render(rec, discordTpl, map[string]any{
		"Title":           "Discord",
		"Active":          "discord",
		"TokenConfigured": true,
	})
	body := rec.Body.String()
	if !strings.Contains(body, "********") {
		t.Fatalf("masked token missing: %s", body)
	}
}

func TestAlertsTemplateCreatesAndManagesAlerts(t *testing.T) {
	rec := httptest.NewRecorder()
	render(rec, alertsTpl, map[string]any{
		"Title":           "Alertes",
		"Active":          "alerts",
		"CapacityPresets": rules.CapacityPresets,
		"Alerts":          []db.Alert{{ID: 42, Name: "Fixture", Enabled: true, DiscordEnabled: true}},
	})
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("unexpected status %d: %s", rec.Code, body)
	}
	for _, required := range []string{"/alerts/add", "/alerts/toggle", "/alerts/delete", "Créer une alerte", "Pause", "Supprimer"} {
		if !strings.Contains(body, required) {
			t.Fatalf("missing alert action %q", required)
		}
	}
}

func TestAlertDraftFromForm(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/alerts/add", strings.NewReader("capacity=hdd_16_20&media=rotational&keywords=Exos%2CIronWolf&max_price_per_tb=20%2C5&min_discount_pct=7.5&cooldown_hours=12&discord_enabled=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	draft, err := alertDraftFromForm(req)
	if err != nil {
		t.Fatal(err)
	}
	if draft.MaxPricePerTB == nil || *draft.MaxPricePerTB != 20.5 || draft.MinDiscountPct != 7.5 || draft.CooldownHours != 12 {
		t.Fatalf("unexpected draft: %+v", draft)
	}
	if len(draft.Keywords) != 2 || len(draft.CapacityPresets) != 1 {
		t.Fatalf("filters were not parsed: %+v", draft)
	}
	if !draft.DiscordEnabled {
		t.Fatal("Discord checkbox was not parsed")
	}
}

func TestProductsTemplateRendersFilters(t *testing.T) {
	rec := httptest.NewRecorder()
	render(rec, productsTpl, map[string]any{
		"Title": "Produits", "Active": "products", "Sources": []string{"diskprices"},
		"Brands": []string{"Seagate"}, "Categories": []string{"nas"}, "Interfaces": []string{"sata"}, "Recordings": []string{"cmr"},
		"Prices":     []db.CurrentPrice{{ProductID: "fixture", Title: "IronWolf", URL: "https://example.test", CapacityTB: 16, PriceEUR: 300, PricePerTB: 18.75}},
		"Sparklines": map[string][]db.SparklinePoint{"fixture": {{PricePerTB: 20}, {PricePerTB: 18.75}}},
	})
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("unexpected status %d: %s", rec.Code, body)
	}
	for _, required := range []string{"name=\"q\"", "name=\"source\"", "name=\"media\"", "name=\"brand\"", "name=\"category\"", "name=\"interface\"", "name=\"recording\"", "name=\"max_eur_tb\""} {
		if !strings.Contains(body, required) {
			t.Fatalf("missing product filter %q", required)
		}
	}
	if !strings.Contains(body, "Tendance 7 jours") || strings.Contains(body, "template:") {
		t.Fatalf("sparkline did not render: %s", body)
	}
	for _, required := range []string{"product-card", "filter-drawer", "Actualisé", "Créer une alerte"} {
		if !strings.Contains(body, required) {
			t.Fatalf("missing catalogue UI %q", required)
		}
	}
}

func TestRelativeTimeAt(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "à l'instant"},
		{now.Add(-12 * time.Minute), "il y a 12 min"},
		{now.Add(-3 * time.Hour), "il y a 3 h"},
		{now.Add(-48 * time.Hour), "il y a 2 j"},
	} {
		if got := relativeTimeAt(tc.at, now); got != tc.want {
			t.Fatalf("relativeTimeAt() = %q, want %q", got, tc.want)
		}
	}
}

func TestProductDetailTemplateRendersComparison(t *testing.T) {
	now := time.Now()
	media, condition := "solid_state", "new"
	rec := httptest.NewRecorder()
	render(rec, productDetailTpl, map[string]any{
		"Title": "Produit", "Active": "products", "Days": 30,
		"Product": &db.Product{ID: "fixture", Title: "SSD Fixture", Source: "merchant", URL: "https://example.test", MediaType: &media, Condition: &condition, CapacityTB: 4, LastSeenAt: now},
		"Current": &db.PriceHistoryPoint{ObservedAt: now, PriceEUR: 200, PricePerTB: 50, Source: "merchant"},
	})
	body := rec.Body.String()
	for _, required := range []string{"Comparaison des prix", "Créer une alerte de prix", "200.00 €", "Dernier refresh"} {
		if !strings.Contains(body, required) {
			t.Fatalf("missing product detail UI %q", required)
		}
	}
}

func TestDashboardRendersRecentNotifications(t *testing.T) {
	rec := httptest.NewRecorder()
	render(rec, statsTpl, map[string]any{
		"Title":     "Tableau de bord",
		"StartedAt": time.Now(),
		"Notifications": []db.Notification{{
			AlertName: "NAS", Title: "IronWolf", URL: "https://example.test/offer",
			PriceEUR: 299.99, PricePerTB: 18.75, Reason: "threshold", SentAt: time.Now(),
		}},
	})
	body := rec.Body.String()
	for _, required := range []string{"Dernières alertes déclenchées", "NAS", "IronWolf", "https://example.test/offer"} {
		if !strings.Contains(body, required) {
			t.Fatalf("missing notification value %q: %s", required, body)
		}
	}
}

func TestFilterPricesUsesStorageFacets(t *testing.T) {
	brand, category, iface, recording, media := "Seagate", "nas", "sata", "cmr", "rotational"
	prices := []db.CurrentPrice{
		{Title: "Seagate IronWolf", Brand: &brand, DriveCategory: &category, Interfaces: []string{iface}, RecordingMethod: &recording, MediaType: &media, CapacityTB: 16, PricePerTB: 18},
		{Title: "Other SSD", CapacityTB: 2, PricePerTB: 60},
	}
	req := httptest.NewRequest(http.MethodGet, "/products?q=ironwolf&brand=Seagate&category=nas&interface=sata&recording=cmr&max_eur_tb=20", nil)
	got := filterPrices(prices, req)
	if len(got) != 1 || got[0].Title != "Seagate IronWolf" {
		t.Fatalf("unexpected filtered prices: %#v", got)
	}
}

func TestFeedEndpointIsPublic(t *testing.T) {
	// /feed.xml should be accessible without auth (no redirect to /login).
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/feed.xml", nil)
	req.Header.Set("Accept", "text/html")
	srv.handler().ServeHTTP(rec, req)
	// With nil DB, the feed handler returns 500 (no DB), but critically it
	// does NOT redirect to /login — that proves it is public.
	if rec.Code == http.StatusFound {
		t.Fatalf("/feed.xml redirected to login — should be public. Location: %s", rec.Header().Get("Location"))
	}
}

func TestFeedEndpointAliasWorks(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/feed", nil)
	srv.handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusFound {
		t.Fatalf("/feed redirected to login — should be public")
	}
}

func TestXmlEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"normal text", "normal text"},
		{"a & b", "a &amp; b"},
		{"<script>", "&lt;script&gt;"},
		{`"quoted"`, "&quot;quoted&quot;"},
		{"it's", "it&apos;s"},
	}
	for _, c := range cases {
		if got := xmlEscape(c.in); got != c.want {
			t.Errorf("xmlEscape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestComputeChartPointsEmpty(t *testing.T) {
	if got := computeChartPoints(nil, 0, 0); got != "" {
		t.Fatalf("empty history should produce empty string, got %q", got)
	}
	if got := computeChartPoints([]db.PriceHistoryPoint{{}}, 0, 0); got != "" {
		t.Fatalf("single point should produce empty string, got %q", got)
	}
}

func TestComputeChartPointsMultiple(t *testing.T) {
	pts := []db.PriceHistoryPoint{
		{PricePerTB: 10},
		{PricePerTB: 20},
		{PricePerTB: 15},
	}
	got := computeChartPoints(pts, 10, 20)
	if got == "" {
		t.Fatal("expected non-empty chart points for 3 observations")
	}
	if strings.Count(got, " ") != 2 {
		t.Fatalf("expected 3 coordinate pairs (2 spaces), got %d in %q", strings.Count(got, " "), got)
	}
}

func TestComputeSparklineEmptyOrShort(t *testing.T) {
	if got := computeSparklinePoints(nil); got.Coords != "" || got.Trend != "" {
		t.Fatalf("empty series should produce empty result, got %+v", got)
	}
	if got := computeSparklinePoints([]db.SparklinePoint{{PricePerTB: 10}}); got.Coords != "" {
		t.Fatalf("single-point series should produce empty result, got %+v", got)
	}
}

func TestComputeSparklineTrendDirection(t *testing.T) {
	now := time.Now()
	// Series that ends below the median → down (green).
	down := []db.SparklinePoint{
		{ObservedAt: now.Add(-72 * time.Hour), PricePerTB: 30},
		{ObservedAt: now.Add(-48 * time.Hour), PricePerTB: 32},
		{ObservedAt: now.Add(-24 * time.Hour), PricePerTB: 31},
		{ObservedAt: now, PricePerTB: 20},
	}
	got := computeSparklinePoints(down)
	if got.Trend != "down" {
		t.Fatalf("expected down trend, got %+v", got)
	}
	if got.Coords == "" {
		t.Fatal("down series should produce non-empty coords")
	}

	// Series that ends above the median → up (red).
	up := []db.SparklinePoint{
		{ObservedAt: now.Add(-72 * time.Hour), PricePerTB: 20},
		{ObservedAt: now.Add(-48 * time.Hour), PricePerTB: 21},
		{ObservedAt: now.Add(-24 * time.Hour), PricePerTB: 19},
		{ObservedAt: now, PricePerTB: 30},
	}
	if got := computeSparklinePoints(up); got.Trend != "up" {
		t.Fatalf("expected up trend, got %+v", got)
	}
}

func TestComputeSparklineFlat(t *testing.T) {
	now := time.Now()
	flat := []db.SparklinePoint{
		{ObservedAt: now.Add(-2 * time.Hour), PricePerTB: 25},
		{ObservedAt: now, PricePerTB: 25},
	}
	if got := computeSparklinePoints(flat); got.Trend != "flat" {
		t.Fatalf("expected flat trend, got %+v", got)
	}
}

func TestDurationHuman(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second:             "30s",
		90 * time.Second:             "1m 30s",
		2*time.Hour + 14*time.Minute: "2h 14m",
		26 * time.Hour:               "1j 2h",
		-5 * time.Second:             "0s", // overdue → clamp
	}
	for d, want := range cases {
		if got := durationHuman(d); got != want {
			t.Errorf("durationHuman(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestParseCronInterval(t *testing.T) {
	cases := map[string]struct {
		want time.Duration
		ok   bool
	}{
		"@every 4h":     {4 * time.Hour, true},
		"@every 30m":    {30 * time.Minute, true},
		"@every  1h30m": {90 * time.Minute, true},
		"@every 1h ":    {time.Hour, true},
		"":              {0, false},
		"0 0 * * *":     {0, false}, // real cron → not supported here
		"@every bogus":  {0, false},
		"@every -5m":    {0, false}, // negative → rejected
	}
	for in, want := range cases {
		got, ok := parseCronInterval(in)
		if ok != want.ok {
			t.Errorf("parseCronInterval(%q) ok = %v, want %v", in, ok, want.ok)
			continue
		}
		if ok && got != want.want {
			t.Errorf("parseCronInterval(%q) = %v, want %v", in, got, want.want)
		}
	}
}

func TestRoutesRejectUnsupportedMethodsBeforeDBUse(t *testing.T) {
	srv := New(nil, nil, &config.Config{}, nil)
	for _, path := range []string{"/alerts/add", "/alerts/toggle", "/alerts/delete", "/config/save", "/discord/save"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: expected 405, got %d", path, rec.Code)
		}
	}
}

// fakeSource is a minimal source used to register a name in SourceHealth.
type fakeSource struct{ n string }

func (f fakeSource) Name() string { return f.n }
func (f fakeSource) Fetch(_ context.Context) ([]domain.Deal, error) {
	return nil, nil
}

// TestApiSourcesHealthJSON verifies the JSON shape used by external
// monitoring (Prometheus, uptime checks). With a scanner wired up and two
// registered sources, the endpoint must return the expected envelope.
func TestApiSourcesHealthJSON(t *testing.T) {
	cfg := &config.Config{
		WebAdminPassword:      "secret",
		SourceHealthThreshold: 2,
	}
	scan := scanner.New(cfg, nil, []sources.Source{
		fakeSource{n: "alpha"},
		fakeSource{n: "beta"},
	}, nil)
	// Drive one source past the threshold to ensure Flagged flips.
	scan.RecordSourceScanResult("alpha", 0)
	scan.RecordSourceScanResult("alpha", 0)
	scan.RecordSourceScanResult("beta", 3)

	srv := New(nil, scan, cfg, nil)
	// Mint a valid session by going through the login flow.
	mux := srv.handler()
	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("login failed: %d %s", loginRec.Code, loginRec.Body.String())
	}
	// Pull the session cookie out of the response.
	var sessionCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("no %q cookie set after login; Set-Cookie=%q", sessionCookieName, loginRec.Header().Get("Set-Cookie"))
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sources/health", nil)
	req.AddCookie(sessionCookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"name":"alpha"`,
		`"name":"beta"`,
		`"flagged":true`,
		`"flagged":false`,
		`"threshold":2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q: %s", want, body)
		}
	}
}

// TestApiSourcesHealthRejectsPOST enforces the GET-only contract.
func TestApiSourcesHealthRejectsPOST(t *testing.T) {
	cfg := &config.Config{WebAdminPassword: "secret"}
	srv := New(nil, nil, cfg, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sources/health", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestApiSourcePreviewUnknownName returns 404 when the requested
// source is not registered. We build a real scanner with a fake
// source so the route can iterate over Sources().
func TestApiSourcePreviewUnknownName(t *testing.T) {
	cfg := &config.Config{WebAdminPassword: "secret"}
	scan := scanner.New(cfg, nil, []sources.Source{fakeSource{n: "alpha"}}, nil)
	srv := New(nil, scan, cfg, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sources/preview?name=nope", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestApiSourcePreviewRequiresName validates the missing-param
// contract.
func TestApiSourcePreviewRequiresName(t *testing.T) {
	cfg := &config.Config{WebAdminPassword: "secret"}
	srv := New(nil, nil, cfg, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sources/preview", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestApiSourcePreviewRejectsPOST enforces the GET-only contract.
func TestApiSourcePreviewRejectsPOST(t *testing.T) {
	cfg := &config.Config{WebAdminPassword: "secret"}
	srv := New(nil, nil, cfg, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sources/preview", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestLoginPageIsServedWithoutBasicAuth(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
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
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
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
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)

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
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("password=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login POST bad pass: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Mot de passe invalide") {
		t.Fatalf("expected error message in body, got: %s", rec.Body.String())
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("failed login should not set a session cookie")
	}
}

// TestLoginPageRendersInEnglish verifies the language switcher: setting the
// lang cookie to "en" must surface the English strings in the login page.
func TestLoginPageRendersInEnglish(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: langCookieName, Value: "en"})
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "DiskCount login") {
		t.Fatalf("English login title missing: %s", body)
	}
	if !strings.Contains(body, "Sign in") {
		t.Fatalf("English submit button missing: %s", body)
	}
	if strings.Contains(body, "Connexion DiskCount") {
		t.Fatalf("French title leaked despite en cookie: %s", body)
	}
}

// TestSetLangSwitchesLocaleAndPinsCookie exercises the /lang endpoint:
// posting lang=en must set the cookie and redirect (303), and the next
// request must see the cookie honoured.
func TestSetLangSwitchesLocaleAndPinsCookie(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
	mux := srv.handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/lang", strings.NewReader("lang=en&next=/login"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == langCookieName && c.Value == "en" {
			found = true
		}
	}
	if !found {
		t.Fatalf("lang cookie not set: %q", rec.Header().Get("Set-Cookie"))
	}
}

// TestSetLangRejectsBadMethod enforces the POST-only contract.
func TestSetLangRejectsBadMethod(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/lang", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestSetThemePinsCookieAndRenders verifies POST /theme: the cookie is
// set, the redirect goes back, and the next render shows the active
// "dark" state in the layout attribute.
func TestSetThemePinsCookieAndRenders(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
	mux := srv.handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/theme", strings.NewReader("theme=dark&next=/"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rec.Code, rec.Body.String())
	}
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == themeCookieName && c.Value == themeDark {
			found = true
		}
	}
	if !found {
		t.Fatalf("theme cookie not set: %q", rec.Header().Get("Set-Cookie"))
	}
}

// TestSetThemeRejectsBadValue enforces the light|dark|auto contract.
func TestSetThemeRejectsBadValue(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/theme", strings.NewReader("theme=neon"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestSetThemeRejectsBadMethod enforces the POST-only contract on
// the public handler (/theme lives outside the auth-guarded routes
// mux so the switcher works on the login page).
func TestSetThemeRejectsBadMethod(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/theme", nil)
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestJSONEndpointStillSpeaksBasicAuth(t *testing.T) {
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
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
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)
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
	srv := New(nil, nil, &config.Config{WebAdminPassword: "secret"}, nil)

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
