package scraper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClassifyHTTPStatus(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorKind
	}{
		{200, 0}, // not reached
		{400, ErrKindPermanent},
		{401, ErrKindAuth},
		{403, ErrKindAuth},
		{404, ErrKindPermanent},
		{429, ErrKindTransient},
		{500, ErrKindTransient},
		{502, ErrKindTransient},
		{503, ErrKindTransient},
	}
	for _, c := range cases {
		err := classifyHTTPStatus("http://x.test/", c.status)
		if c.status == 200 {
			continue
		}
		fe, ok := err.(*FetchError)
		if !ok {
			t.Fatalf("status %d: expected *FetchError, got %T", c.status, err)
		}
		if fe.Kind != c.want {
			t.Fatalf("status %d: got kind %v, want %v", c.status, fe.Kind, c.want)
		}
	}
}

func TestRetryingFetcherRetriesTransient(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f := NewHTTPFetcherWithOptions(Options{
		UserAgent:             "test/1.0",
		PerRequestTimeout:     time.Second,
		DisableBrowserHeaders: true,
	})
	rf := NewRetryingFetcher(f, RetryConfig{
		MaxRetries: 5,
		BaseDelay:  5 * time.Millisecond,
		MaxDelay:   20 * time.Millisecond,
		Jitter:     0,
	})

	body, err := rf.Get(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != "ok" {
		t.Fatalf("body = %q, want %q", body, "ok")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestRetryingFetcherDoesNotRetryPermanent(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	f := NewHTTPFetcherWithOptions(Options{
		UserAgent:             "test/1.0",
		PerRequestTimeout:     time.Second,
		DisableBrowserHeaders: true,
	})
	rf := NewRetryingFetcher(f, RetryConfig{MaxRetries: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond})

	_, err := rf.Get(context.Background(), srv.URL+"/")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsAuth(err) && err.(*FetchError).Kind != ErrKindPermanent {
		// 404 is permanent (not auth); we just want to confirm a single attempt.
		if fe, ok := err.(*FetchError); !ok || fe.Kind != ErrKindPermanent {
			t.Fatalf("unexpected error kind: %v", err)
		}
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("permanent error should not be retried, got %d attempts", got)
	}
}

func TestBlockedKeywordsTriggerTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Just a moment... Checking your browser before accessing the site."))
	}))
	defer srv.Close()

	f := NewHTTPFetcherWithOptions(Options{
		UserAgent:             "test/1.0",
		PerRequestTimeout:     time.Second,
		DisableBrowserHeaders: true,
		BlockedKeywords:       []string{"Just a moment..."},
	})
	_, err := f.Get(context.Background(), srv.URL+"/")
	if err == nil {
		t.Fatalf("expected error from blocked detection")
	}
	fe, ok := err.(*FetchError)
	if !ok {
		t.Fatalf("expected *FetchError, got %T", err)
	}
	if fe.Kind != ErrKindTransient {
		t.Fatalf("blocked detection should be transient, got %v", fe.Kind)
	}
}

func TestIsRetryable(t *testing.T) {
	if !IsRetryable(NewTransientError("u", 0, "x", nil)) {
		t.Fatal("transient should be retryable")
	}
	if IsRetryable(NewPermanentError("u", 0, "x", nil)) {
		t.Fatal("permanent should not be retryable")
	}
	if IsRetryable(errors.New("plain")) {
		t.Fatal("plain errors should not be retryable")
	}
}

func TestClassifyTransportError(t *testing.T) {
	if err := classifyTransportError("u", errors.New("connection refused")); err.(*FetchError).Kind != ErrKindTransient {
		t.Fatal("connection refused should be transient")
	}
	if err := classifyTransportError("u", errors.New("invalid url syntax")); err.(*FetchError).Kind != ErrKindPermanent {
		t.Fatal("invalid url should be permanent")
	}
}
