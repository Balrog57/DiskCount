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

func (l *loginLimiter) blocked(key string) bool {
	now := time.Now()
	cutoff := now.Add(-loginRateWindow)
	l.mu.Lock()
	defer l.mu.Unlock()
	times := l.hits[key]
	n := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[n] = t
			n++
		}
	}
	times = times[:n]
	l.hits[key] = times
	return len(times) >= loginRateMaxAttempts
}

func (l *loginLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.hits[key], time.Now())
}
