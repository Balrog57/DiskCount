package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateFetchURLBlocksInternalTargets(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/",
		"http://localhost/admin",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"file:///etc/passwd",
	}
	for _, raw := range cases {
		if err := validateFetchURL(raw); err == nil {
			t.Fatalf("expected %q to be blocked", raw)
		}
	}
}

func TestHTTPFetcherBlocksRedirectToInternalHost(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("secret"))
	}))
	defer internal.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer srv.Close()

	f := NewHTTPFetcherWithOptions(Options{
		UserAgent:             "test/1.0",
		PerRequestTimeout:     time.Second,
		DisableBrowserHeaders: true,
	})
	_, err := f.Get(context.Background(), srv.URL+"/")
	if err == nil {
		t.Fatal("expected redirect to internal host to be blocked")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("unexpected error: %v", err)
	}
}
