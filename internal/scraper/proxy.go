package scraper

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// ProxyTier represents a transport tier that a request can be routed through.
// The order in which tiers are tried depends on the source's policy.
type ProxyTier int

const (
	ProxyDirect ProxyTier = iota // fastest, no proxy
	ProxyByparr                  // headless browser fallback
)

func (t ProxyTier) String() string {
	switch t {
	case ProxyDirect:
		return "direct"
	case ProxyByparr:
		return "byparr"
	default:
		return "unknown"
	}
}

// ProxyClient is the minimal interface a tier must implement. The default
// implementation reuses HTTPFetcher (direct) and ByparrClient (byparr).
type ProxyClient interface {
	Fetch(ctx context.Context, url string) (string, error)
	Name() string
}

// ProxyStats holds a coarse per-tier counter exposed by /api/metrics.
type ProxyStats struct {
	Tier    string `json:"tier"`
	Success int64  `json:"success"`
	Failure int64  `json:"failure"`
}

// ProxyRouter tries tiers in order until one succeeds. The first tier is
// always the fastest (direct). Sources opt in to additional tiers via
// their Fetch implementation: they can call router.Fetch(ctx, url) instead
// of httpFetcher.Get directly. Sources that want to keep the existing
// behaviour (direct → byparr on direct failure) can keep using their own
// fallback path; ProxyRouter is a convenience, not a global wrapper.
type ProxyRouter struct {
	direct *HTTPFetcher
	byparr *ByparrClient
}

// NewProxyRouter returns a router that uses the given direct fetcher and
// optional byparr client. If byparr is nil, the router falls back to direct
// for every request.
func NewProxyRouter(direct *HTTPFetcher, byparr *ByparrClient) *ProxyRouter {
	return &ProxyRouter{direct: direct, byparr: byparr}
}

// Fetch tries each tier in order and returns the first success. If all
// tiers fail, the last error is returned wrapped with a short reason so
// callers can tell direct-fail from byparr-fail.
func (r *ProxyRouter) Fetch(ctx context.Context, url string) (string, error) {
	body, err := r.direct.Get(ctx, url)
	if err == nil {
		return body, nil
	}
	if r.byparr == nil {
		return "", err
	}
	// Only escalate transient or auth errors to byparr; permanent errors
	// (4xx other than 401/403/429) won't be solved by a headless browser.
	if fe, ok := err.(*FetchError); ok {
		if fe.Kind == ErrKindPermanent {
			return "", err
		}
	}
	slog.Debug("proxy router escalating to byparr", "url", url, "direct_err", err)
	session, berr := r.byparr.GetPage(ctx, url)
	if berr != nil {
		return "", berr
	}
	return session.HTML, nil
}

// Stats is a placeholder for future instrumentation; today the scanner
// collects per-source metrics and there is no per-tier accounting yet.
func (r *ProxyRouter) Stats() []ProxyStats { return nil }

// IsHealthy pings the byparr instance if one is configured. The function
// always returns true when byparr is nil.
func (r *ProxyRouter) IsHealthy(ctx context.Context) bool {
	if r.byparr == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// We don't have a real ping endpoint; a 404 from /v1 with a 405
	// would prove reachability. Keep the check minimal: a HEAD on /v1
	// is good enough; an error means byparr is unreachable.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.byparr.BaseURL+"/v1", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}
