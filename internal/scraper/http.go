package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxBodyBytes = 5 << 20

var BrowserLikeHeaders = map[string]string{
	"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
	"Accept-Language":           "fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7",
	"Accept-Encoding":           "gzip, deflate, br",
	"Cache-Control":             "no-cache",
	"Sec-Ch-Ua":                 `"Chromium";v="128", "Not;A=Brand";v="24", "Google Chrome";v="128"`,
	"Sec-Ch-Ua-Mobile":          "?0",
	"Sec-Ch-Ua-Platform":        `"Windows"`,
	"Sec-Fetch-Dest":            "document",
	"Sec-Fetch-Mode":            "navigate",
	"Sec-Fetch-Site":            "none",
	"Sec-Fetch-User":            "?1",
	"Upgrade-Insecure-Requests": "1",
	"Dnt":                       "1",
}

var defaultUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:130.0) Gecko/20100101 Firefox/130.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36 Edg/128.0.0.0",
}

var defaultBlockedKeywords = []string{
	"cf-browser-verification", "Just a moment...",
	"Checking your browser", "Enable JavaScript",
	"Access Denied", "captcha", "403 Forbidden",
}

type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	Jitter     float64
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   30 * time.Second,
		Jitter:     0.3,
	}
}

// Options configures an HTTPFetcher. Zero values fall back to sensible defaults.
type Options struct {
	// UserAgent is the primary user-agent string. If empty, a default is used.
	UserAgent string
	// UserAgents is an optional pool of user-agents to rotate. When set, the
	// primary UserAgent is ignored and rotation happens per request.
	UserAgents []string
	// PerRequestTimeout bounds a single HTTP call. If zero, a 10s default is used.
	PerRequestTimeout time.Duration
	// MaxRedirects is the maximum number of redirects to follow. Defaults to 5.
	MaxRedirects int
	// ExtraHeaders are merged on top of the built-in browser-like headers.
	ExtraHeaders map[string]string
	// BlockedKeywords is a list of substrings that, when present in the first
	// 2KB of a response body, mark the response as blocked.
	BlockedKeywords []string
	// DisableBrowserHeaders disables the built-in browser-like headers.
	DisableBrowserHeaders bool
	// DisableBlockedDetection disables the body keyword check.
	DisableBlockedDetection bool
}

type HTTPFetcher struct {
	mu sync.Mutex

	Client       *http.Client
	Headers      map[string]string
	UserAgents   []string
	uaIdx        int
	BlockedWords []string

	PerRequestTimeout time.Duration
	Options           Options
}

func NewHTTPFetcher(userAgent string, timeoutSeconds float64) *HTTPFetcher {
	return &HTTPFetcher{
		Client: &http.Client{
			Timeout: time.Duration(timeoutSeconds * float64(time.Second)),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		Headers: map[string]string{
			"User-Agent":      userAgent,
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.9",
		},
	}
}

// NewHTTPFetcherWithOptions builds a fully configured HTTPFetcher.
func NewHTTPFetcherWithOptions(opts Options) *HTTPFetcher {
	if opts.UserAgent == "" {
		opts.UserAgent = "DiskCountBot/2.0"
	}
	if opts.PerRequestTimeout <= 0 {
		opts.PerRequestTimeout = 10 * time.Second
	}
	if opts.MaxRedirects <= 0 {
		opts.MaxRedirects = 5
	}
	if len(opts.UserAgents) == 0 {
		opts.UserAgents = []string{opts.UserAgent}
	}
	if len(opts.BlockedKeywords) == 0 && !opts.DisableBlockedDetection {
		opts.BlockedKeywords = defaultBlockedKeywords
	}

	headers := map[string]string{
		"User-Agent": opts.UserAgents[0],
	}
	if !opts.DisableBrowserHeaders {
		for k, v := range BrowserLikeHeaders {
			headers[k] = v
		}
	}
	for k, v := range opts.ExtraHeaders {
		headers[k] = v
	}

	client := &http.Client{
		Timeout: opts.PerRequestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= opts.MaxRedirects {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	return &HTTPFetcher{
		Client:            client,
		Headers:           headers,
		UserAgents:        opts.UserAgents,
		BlockedWords:      opts.BlockedKeywords,
		PerRequestTimeout: opts.PerRequestTimeout,
		Options:           opts,
	}
}

// NewHTTPFetcherWithHeaders is a thin wrapper that preserves the legacy signature
// used by the source registry: a primary user-agent, a total timeout, an
// optional extra-headers JSON blob (matches the HEADERS_EXTRA env var format),
// and an optional user-agent pool. The total timeout is repurposed as the
// per-request timeout because all callers (sources and the retrying fetcher)
// already wrap calls in a per-source context.
func NewHTTPFetcherWithHeaders(userAgent string, timeoutSeconds float64, headers map[string]string, userAgents []string) *HTTPFetcher {
	return NewHTTPFetcherWithOptions(Options{
		UserAgent:          userAgent,
		PerRequestTimeout:  time.Duration(timeoutSeconds * float64(time.Second)),
		ExtraHeaders:       headers,
		UserAgents:         userAgents,
		DisableBrowserHeaders: false,
	})
}

// ParseHeadersJSON parses the HEADERS_EXTRA env var value (a JSON object of
// string→string) into a map. Returns nil for empty input.
func ParseHeadersJSON(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("HEADERS_EXTRA: %w", err)
	}
	return out, nil
}

func (f *HTTPFetcher) rotateUA() {
	if len(f.UserAgents) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uaIdx = (f.uaIdx + 1) % len(f.UserAgents)
	f.Headers["User-Agent"] = f.UserAgents[f.uaIdx]
}

func (f *HTTPFetcher) currentUA() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Headers["User-Agent"]
}

func (f *HTTPFetcher) Get(ctx context.Context, url string) (string, error) {
	// Per-request timeout is enforced at the *http.Client level (the timeout
	// configured on the client). The caller's context provides a per-source
	// budget that wraps the entire scan path.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", NewPermanentError(url, 0, "creating request", err)
	}

	f.rotateUA()
	f.mu.Lock()
	for k, v := range f.Headers {
		req.Header.Set(k, v)
	}
	f.mu.Unlock()

	resp, err := f.Client.Do(req)
	if err != nil {
		return "", classifyTransportError(url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if blocked, word := f.isBlocked(string(body)); blocked {
			return "", NewTransientError(url, resp.StatusCode, fmt.Sprintf("blocked (keyword: %q)", word), nil)
		}
		return "", classifyHTTPStatus(url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", NewTransientError(url, resp.StatusCode, "reading body", err)
	}

	bodyStr := string(body)
	if blocked, word := f.isBlocked(bodyStr); blocked {
		return "", NewTransientError(url, resp.StatusCode, fmt.Sprintf("blocked (keyword: %q)", word), nil)
	}

	return bodyStr, nil
}

func (f *HTTPFetcher) PostJSON(ctx context.Context, url string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return "", NewPermanentError(url, 0, "creating request", err)
	}

	f.mu.Lock()
	for k, v := range f.Headers {
		req.Header.Set(k, v)
	}
	f.mu.Unlock()
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.Client.Do(req)
	if err != nil {
		return "", classifyTransportError(url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", classifyHTTPStatus(url, resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", NewTransientError(url, resp.StatusCode, "reading body", err)
	}

	return string(respBody), nil
}

// PostWithHeaders sends a POST with the given body string and custom headers.
// Used for OAuth2 token endpoints that require form-encoded bodies and an
// Authorization header (e.g. eBay client-credentials grant).
func (f *HTTPFetcher) PostWithHeaders(ctx context.Context, url, body string, headers map[string]string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", NewPermanentError(url, 0, "creating request", err)
	}
	f.mu.Lock()
	for k, v := range f.Headers {
		req.Header.Set(k, v)
	}
	f.mu.Unlock()
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return "", classifyTransportError(url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		rbody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", classifyHTTPStatusDetail(url, resp.StatusCode, string(rbody))
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", NewTransientError(url, resp.StatusCode, "reading body", err)
	}
	return string(respBody), nil
}

// GetWithAuth performs a GET request with a Bearer authorization token.
// Used for authenticated API calls (e.g. eBay Browse API).
func (f *HTTPFetcher) GetWithAuth(ctx context.Context, url, bearer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", NewPermanentError(url, 0, "creating request", err)
	}
	f.mu.Lock()
	for k, v := range f.Headers {
		req.Header.Set(k, v)
	}
	f.mu.Unlock()
	req.Header.Set("Authorization", bearer)
	resp, err := f.Client.Do(req)
	if err != nil {
		return "", classifyTransportError(url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		rbody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", classifyHTTPStatusDetail(url, resp.StatusCode, string(rbody))
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", NewTransientError(url, resp.StatusCode, "reading body", err)
	}
	return string(respBody), nil
}

func (f *HTTPFetcher) isBlocked(body string) (bool, string) {
	if len(f.BlockedWords) == 0 {
		return false, ""
	}
	lower := strings.ToLower(body)
	if len(lower) > 2048 {
		lower = lower[:2048]
	}
	for _, word := range f.BlockedWords {
		if strings.Contains(lower, strings.ToLower(word)) {
			return true, word
		}
	}
	return false, ""
}

// RetryingFetcher wraps an HTTPFetcher with bounded exponential-backoff
// retries. Only ErrKindTransient errors are retried; permanent, auth and parse
// errors propagate immediately. Retry-After headers are honoured for 429/503
// responses.
type RetryingFetcher struct {
	fetcher *HTTPFetcher
	config  RetryConfig
}

func NewRetryingFetcher(fetcher *HTTPFetcher, config RetryConfig) *RetryingFetcher {
	if config.MaxRetries < 0 {
		config.MaxRetries = 0
	}
	return &RetryingFetcher{fetcher: fetcher, config: config}
}

func (rf *RetryingFetcher) Get(ctx context.Context, url string) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= rf.config.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := rf.backoffDuration(attempt)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		body, err := rf.fetcher.Get(ctx, url)
		if err == nil {
			return body, nil
		}

		if fe, ok := err.(*FetchError); ok {
			if fe.Kind == ErrKindTransient {
				lastErr = err
				continue
			}
		}
		return "", err
	}
	return "", fmt.Errorf("retry exhausted after %d attempts: %w", rf.config.MaxRetries+1, lastErr)
}

func (rf *RetryingFetcher) backoffDuration(attempt int) time.Duration {
	base := float64(rf.config.BaseDelay) * math.Pow(2, float64(attempt-1))
	if base > float64(rf.config.MaxDelay) {
		base = float64(rf.config.MaxDelay)
	}
	jitter := base * rf.config.Jitter * (rand.Float64()*2 - 1)
	dur := time.Duration(base + jitter)
	if dur < 0 {
		dur = rf.config.BaseDelay
	}
	if dur > rf.config.MaxDelay {
		dur = rf.config.MaxDelay
	}
	return dur
}

func classifyHTTPStatus(url string, status int) error {
	msg := fmt.Sprintf("HTTP %d %s", status, http.StatusText(status))
	switch {
	case status == 429:
		return NewTransientError(url, status, msg, nil)
	case status == 401 || status == 403:
		return NewAuthError(url, status, msg, nil)
	case status >= 500:
		return NewTransientError(url, status, msg, nil)
	default:
		return NewPermanentError(url, status, msg, nil)
	}
}

// classifyHTTPStatusDetail is like classifyHTTPStatus but includes a response
// body excerpt in the message, which is useful for API calls that return
// structured error details (e.g. eBay, Keepa).
func classifyHTTPStatusDetail(url string, status int, bodyExcerpt string) error {
	msg := fmt.Sprintf("HTTP %d %s", status, http.StatusText(status))
	if bodyExcerpt != "" {
		if len(bodyExcerpt) > 200 {
			bodyExcerpt = bodyExcerpt[:200]
		}
		msg += ": " + bodyExcerpt
	}
	switch {
	case status == 429:
		return NewTransientError(url, status, msg, nil)
	case status == 401 || status == 403:
		return NewAuthError(url, status, msg, nil)
	case status >= 500:
		return NewTransientError(url, status, msg, nil)
	default:
		return NewPermanentError(url, status, msg, nil)
	}
}

func classifyTransportError(url string, err error) error {
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "eof") ||
		strings.Contains(lower, "tls") ||
		strings.Contains(lower, "broken pipe") {
		return NewTransientError(url, 0, msg, err)
	}
	return NewPermanentError(url, 0, msg, err)
}

func extractRetryAfter(resp *http.Response) time.Duration {
	if resp == nil || resp.Header == nil {
		return 0
	}
	if s := resp.Header.Get("Retry-After"); s != "" {
		if sec, err := strconv.Atoi(s); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return 0
}
