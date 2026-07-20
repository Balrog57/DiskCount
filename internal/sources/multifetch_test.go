package sources

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestFetchMultiURLReturnsDealsOnPartialFailure(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>OK</body></html>`))
	}))
	defer srv2.Close()

	fetcher := newTestHTTPFetcher(t)
	res := fetchMultiURL(context.Background(), "test-src", fetcher, nil,
		[]string{srv1.URL, srv2.URL}, false,
		func(html, baseURL string) []domain.Deal {
			if html == "" {
				return nil
			}
			return []domain.Deal{{Source: "test-src", Title: "deal"}}
		})

	if res.failed != 1 || res.total != 2 {
		t.Fatalf("failed/total = %d/%d, want 1/2", res.failed, res.total)
	}
	if len(res.deals) != 1 {
		t.Fatalf("expected 1 deal from the successful URL, got %d", len(res.deals))
	}
	// Partial failure must NOT bubble up as a Transient error (so the breaker
	// doesn't trip when one of N URLs is just down).
	if err := res.asTransientError("test-src"); err != nil {
		t.Fatalf("partial failure should not return error, got %v", err)
	}
}

func TestFetchMultiURLReturnsTransientOnAllFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	fetcher := newTestHTTPFetcher(t)
	res := fetchMultiURL(context.Background(), "test-src", fetcher, nil,
		[]string{srv.URL, srv.URL + "/x"}, false,
		func(html, baseURL string) []domain.Deal { return nil })

	if res.failed != 2 || res.total != 2 {
		t.Fatalf("failed/total = %d/%d, want 2/2", res.failed, res.total)
	}
	err := res.asTransientError("test-src")
	if err == nil {
		t.Fatal("expected Transient error when all URLs failed, got nil")
	}
	if Classify(err) != SeverityTransient {
		t.Fatalf("expected SeverityTransient, got %v (err=%v)", Classify(err), err)
	}
}

func TestFetchMultiURLErrorSummary(t *testing.T) {
	r := multiURLFetchResult{errors: []error{errors.New("a"), errors.New("b")}}
	if got := r.errorSummary(); got != "a; b" {
		t.Fatalf("errorSummary = %q, want %q", got, "a; b")
	}
	if r := (multiURLFetchResult{}); r.errorSummary() != "" {
		t.Fatalf("empty errorSummary should be \"\", got %q", r.errorSummary())
	}
}