package scraper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestByparrReadsResponseField is the regression test for the silent headless
// fallback failure. Byparr (2025+) returns the page HTML in
// solution.response, not solution.body. The old client read only "body", so
// every fallback returned an empty HTML string and sources stayed broken
// even though Byparr had successfully fetched the page.
func TestByparrReadsResponseField(t *testing.T) {
	const pageHTML = "<!DOCTYPE html><html><body><table><tr><td>real content</td></tr></table></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Shape that mirrors current Byparr: body is empty, response holds the HTML.
		resp := map[string]any{
			"status":  "ok",
			"message": "Success",
			"solution": map[string]any{
				"url":       "https://example.test/",
				"status":    200,
				"body":      "", // <-- old field, empty on current Byparr
				"response":  pageHTML,
				"cookies":   []map[string]any{},
				"userAgent": "Mozilla/5.0",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewByparrClient(srv.URL)
	sess, err := c.GetPage(context.Background(), "https://example.test/")
	if err != nil {
		t.Fatalf("GetPage failed: %v", err)
	}
	if sess.HTML != pageHTML {
		t.Fatalf("HTML field = %q, want the page from solution.response", sess.HTML)
	}
	if sess.StatusCode != 200 {
		t.Fatalf("StatusCode = %d, want 200", sess.StatusCode)
	}
}

// TestByparrFallsBackToBodyField verifies the client still works against the
// older FlareSolverr-compatible Byparr that uses solution.body. Both fields
// must be accepted so the client is robust across Byparr versions.
func TestByparrFallsBackToBodyField(t *testing.T) {
	const pageHTML = "<html><body>legacy body field</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"status":  "ok",
			"message": "Success",
			"solution": map[string]any{
				"url":       "https://example.test/",
				"status":    200,
				"body":      pageHTML, // <-- legacy field only
				"cookies":   []map[string]any{},
				"userAgent": "Mozilla/5.0",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewByparrClient(srv.URL)
	sess, err := c.GetPage(context.Background(), "https://example.test/")
	if err != nil {
		t.Fatalf("GetPage failed: %v", err)
	}
	if !strings.Contains(sess.HTML, "legacy body field") {
		t.Fatalf("HTML = %q, want the legacy body content", sess.HTML)
	}
}
