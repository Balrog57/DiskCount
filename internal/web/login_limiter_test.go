package web

import (
	"net/http"
	"testing"
	"time"
)

func TestLoginLimiterBlocksAfterMaxFailures(t *testing.T) {
	l := newLoginLimiter()
	const ip = "10.0.0.1"
	for i := 0; i < loginRateMaxAttempts; i++ {
		if l.blocked(ip) {
			t.Fatalf("blocked before reaching max attempts at %d", i)
		}
		l.recordFailure(ip)
	}
	if !l.blocked(ip) {
		t.Fatal("expected block after max failures")
	}
	l.reset(ip)
	if l.blocked(ip) {
		t.Fatal("reset should clear the lockout")
	}
}

func TestLoginLimiterPrunesExpiredHits(t *testing.T) {
	l := newLoginLimiter()
	const ip = "10.0.0.2"
	l.hits[ip] = []time.Time{time.Now().Add(-loginRateWindow - time.Minute)}
	if l.blocked(ip) {
		t.Fatal("expired hits should not block")
	}
	if _, ok := l.hits[ip]; ok {
		t.Fatal("expired hits should be pruned")
	}
}

func TestClientIPPrefersForwardedFor(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.9" {
		t.Fatalf("clientIP = %q, want first X-Forwarded-For hop", got)
	}
}
