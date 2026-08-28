package web

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	loginRateMaxAttempts = 5
	loginRateWindow      = 15 * time.Minute
	loginRateMaxKeys     = 4096
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

// clientIP returns the TCP peer address. X-Forwarded-For is ignored:
// the admin listener is published on 0.0.0.0, so clients can spoof that
// header. Behind a reverse proxy all users share the proxy address,
// which still rate-limits brute force without allowing a bypass.
func clientIP(r *http.Request) string {
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

func (l *loginLimiter) gcLocked(now time.Time) {
	for key := range l.hits {
		l.pruneLocked(key, now)
	}
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
	if _, exists := l.hits[key]; !exists && len(l.hits) >= loginRateMaxKeys {
		l.gcLocked(now)
		if len(l.hits) >= loginRateMaxKeys {
			return
		}
	}
	l.hits[key] = append(times, now)
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}
