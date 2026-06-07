package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProxyRouterDirectSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("direct-ok"))
	}))
	defer srv.Close()

	f := NewHTTPFetcherWithOptions(Options{
		UserAgent:             "test/1.0",
		PerRequestTimeout:     time.Second,
		DisableBrowserHeaders: true,
	})
	router := NewProxyRouter(f, nil)
	body, err := router.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if body != "direct-ok" {
		t.Fatalf("body = %q, want direct-ok", body)
	}
}

func TestProxyRouterSkipsByparrOnPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer srv.Close()

	// A byparr instance that should never be called; we detect that
	// because it would need a separate handler to record the call.
	byparr := NewByparrClient("http://127.0.0.1:1") // closed port

	f := NewHTTPFetcherWithOptions(Options{
		UserAgent:             "test/1.0",
		PerRequestTimeout:     time.Second,
		DisableBrowserHeaders: true,
	})
	router := NewProxyRouter(f, byparr)
	_, err := router.Fetch(context.Background(), srv.URL+"/missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if fe, ok := err.(*FetchError); !ok || fe.Kind != ErrKindPermanent {
		t.Fatalf("permanent error should not escalate: %v", err)
	}
}

func TestProxyRouterEscalatesTransient(t *testing.T) {
	// First server returns 503 (transient) on the first call, then we
	// point to a byparr-like instance that resolves 200. Since we
	// don't have a real byparr here, the router's escalation will fail
	// with a transport error, which is enough to prove it tried.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	byparr := NewByparrClient("http://127.0.0.1:1")
	f := NewHTTPFetcherWithOptions(Options{
		UserAgent:             "test/1.0",
		PerRequestTimeout:     time.Second,
		DisableBrowserHeaders: true,
	})
	router := NewProxyRouter(f, byparr)
	_, err := router.Fetch(context.Background(), srv.URL+"/")
	if err == nil {
		t.Fatal("expected error from unreachable byparr")
	}
	// The error should mention byparr or transport failure, not the
	// direct 503, because escalation happened.
	if fe, ok := err.(*FetchError); ok && fe.Status == http.StatusServiceUnavailable {
		t.Fatalf("expected escalation past 503, got %v", err)
	}
}
