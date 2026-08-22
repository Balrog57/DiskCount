package scraper

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
)

// CookieStore wraps a *cookiejar.Jar with a per-domain mutex so concurrent
// requests for the same domain don't race. The jar itself is safe for
// concurrent reads; we guard the SetCookies call because callers typically
// need to read-modify-write in one critical section when merging Byparr
// cookies with the jar's current state.
type CookieStore struct {
	mu  sync.Mutex
	jar *cookiejar.Jar
}

// NewCookieStore returns a CookieStore with an empty in-memory jar.
func NewCookieStore() *CookieStore {
	jar, _ := cookiejar.New(nil)
	return &CookieStore{jar: jar}
}

// Cookies returns the cookies for the given URL. Safe for concurrent use.
func (c *CookieStore) Cookies(u *url.URL) []*http.Cookie {
	if c == nil || c.jar == nil {
		return nil
	}
	return c.jar.Cookies(u)
}

// SetCookies stores the cookies for the given URL. Safe for concurrent use.
func (c *CookieStore) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if c == nil || c.jar == nil || len(cookies) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.jar.SetCookies(u, cookies)
}

// MergeFromByparr takes cookies returned by a Byparr session and merges
// them into the store. The function is a thin convenience around
// SetCookies: it converts Byparr's []map[string]interface{} shape into
// http.Cookie values.
func (c *CookieStore) MergeFromByparr(u *url.URL, byparrCookies []map[string]interface{}) {
	if c == nil || u == nil {
		return
	}
	cookies := make([]*http.Cookie, 0, len(byparrCookies))
	for _, ck := range byparrCookies {
		name, _ := ck["name"].(string)
		value, _ := ck["value"].(string)
		domain, _ := ck["domain"].(string)
		path, _ := ck["path"].(string)
		if name == "" {
			continue
		}
		if path == "" {
			path = "/"
		}
		hc := &http.Cookie{Name: name, Value: value, Path: path}
		if domain != "" {
			hc.Domain = domain
		}
		cookies = append(cookies, hc)
	}
	c.SetCookies(u, cookies)
}

// CookieAwareRequest is a convenience for callers that want to attach the
// stored cookies to an outgoing *http.Request. The returned request is
// independent of the store and can be mutated freely.
func (c *CookieStore) CookieAwareRequest(ctx context.Context, method, target string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return req, nil
	}
	u, err := url.Parse(target)
	if err != nil {
		return req, nil
	}
	for _, ck := range c.Cookies(u) {
		req.AddCookie(ck)
	}
	return req, nil
}
