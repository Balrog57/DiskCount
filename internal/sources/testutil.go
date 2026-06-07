package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Balrog57/DiskCount/internal/scraper"
)

// NewTestFetcher returns a RetryingFetcher pointed at the given test server
// and a cleanup function. The fetcher has a generous per-request timeout so
// CI environments do not flake.
func NewTestFetcher(t *testing.T, _ *httptest.Server) *scraper.RetryingFetcher {
	t.Helper()
	f := scraper.NewHTTPFetcherWithOptions(scraper.Options{
		UserAgent:             "DiskCountTest/1.0",
		PerRequestTimeout:     2 * time.Second,
		MaxRedirects:          3,
		DisableBrowserHeaders: true,
	})
	return scraper.NewRetryingFetcher(f, scraper.RetryConfig{
		MaxRetries: 0,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   50 * time.Millisecond,
	})
}

// FetchThroughServer fetches the given path on the test server and returns
// the body.
func FetchThroughServer(t *testing.T, srv *httptest.Server, fetcher *scraper.RetryingFetcher, path string) string {
	t.Helper()
	if !strings.HasPrefix(path, "/") {
		t.Fatalf("path must start with /: %q", path)
	}
	body, err := fetcher.Get(context.Background(), srv.URL+path)
	if err != nil {
		t.Fatalf("fetch %s: %v", path, err)
	}
	return body
}

// MustStatusHandler returns an http.HandlerFunc that always responds with
// the given status and body. Handy for retry/breaker tests.
func MustStatusHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}
