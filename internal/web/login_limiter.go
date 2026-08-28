package web

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginRateMaxAttempts = 5
	loginRateWindow      = 15 * time.Minute
)

// loginLimiter tracks failed authentication attempts per client IP to
// slow brute-force attacks against the admin password.
type loginLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{hits: make(map[string][]time.Time)}
}

// clientIP returns the connecting address. When DiskCount sits behind a
// reverse proxy, the first X-Forwarded-For hop is used; otherwise
// RemoteAddr. Direct exposure without a trusted proxy lets clients spoof
// X-Forwarded-For — the same trust model as requestIsSecure.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (l *loginLimiter) pruneLocked(key string, now time.Time) []time.Time {
	cutoff := now.Add(-loginRateWindow)
	times := l.hits[key]
	n := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[n] = t
			n++
		}
	}
	times = times[:n]
	if len(times) == 0 {
		delete(l.hits, key)
		return nil
	}
	l.hits[key] = times
	return times
}

func (l *loginLimiter) blocked(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pruneLocked(key, now)) >= loginRateMaxAttempts
}

func (l *loginLimiter) recordFailure(key string) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	times := l.pruneLocked(key, now)
	l.hits[key] = append(times, now)
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}
