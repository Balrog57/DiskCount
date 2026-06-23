package sources

import (
	"log/slog"
	"time"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/scraper"
)

type SourceFactory func(reg *Registry) Source

type Registry struct {
	cfg    *config.Config
	http   *scraper.HTTPFetcher
	retry  *scraper.RetryingFetcher
	byparr *scraper.ByparrClient
}

var registeredFactories []SourceFactory

func NewRegistry(cfg *config.Config) *Registry {
	headers, err := scraper.ParseHeadersJSON(cfg.HeadersExtra)
	if err != nil {
		slog.Warn("registry", "err", err.Error())
	}

	perReq := time.Duration(cfg.PerRequestTimeoutSeconds * float64(time.Second))
	if perReq <= 0 {
		perReq = 10 * time.Second
	}

	opts := scraper.Options{
		UserAgent:         cfg.UserAgent,
		UserAgents:        mergeAgents(cfg.UserAgent, cfg.UserAgentPool),
		PerRequestTimeout: perReq,
		ExtraHeaders:      headers,
		BlockedKeywords:   cfg.BlockedDetectionKeywords,
	}

	fetcher := scraper.NewHTTPFetcherWithOptions(opts)
	retry := scraper.NewRetryingFetcher(fetcher, scraper.RetryConfig{
		MaxRetries: cfg.RetryMaxAttempts,
		BaseDelay:  time.Duration(cfg.RetryBaseDelaySeconds * float64(time.Second)),
		MaxDelay:   time.Duration(cfg.RetryMaxDelaySeconds * float64(time.Second)),
		Jitter:     0.3,
	})

	return &Registry{
		cfg:    cfg,
		http:   fetcher,
		retry:  retry,
		byparr: scraper.NewByparrClient(cfg.ByparrURL),
	}
}

func (r *Registry) HTTP() *scraper.HTTPFetcher      { return r.http }
func (r *Registry) Retry() *scraper.RetryingFetcher { return r.retry }
func (r *Registry) Byparr() *scraper.ByparrClient   { return r.byparr }
func (r *Registry) Config() *config.Config          { return r.cfg }

func Register(fn SourceFactory) { registeredFactories = append(registeredFactories, fn) }

// BuildAll instantiates every registered source, filters by EnabledSources if
// the config provides an explicit list, and returns the live ones. The order
// matches the registration order (init() order is otherwise non-deterministic
// in Go, so we sort by name for predictable scans and admin UIs).
func BuildAll(reg *Registry) []Source {
	out := make([]Source, 0, len(registeredFactories))
	for _, fn := range registeredFactories {
		s := fn(reg)
		if s == nil {
			continue
		}
		if !reg.cfg.IsSourceEnabled(s.Name()) {
			slog.Debug("source disabled by config", "src", s.Name())
			continue
		}
		out = append(out, s)
	}
	return out
}

func mergeAgents(primary string, pool []string) []string {
	if len(pool) == 0 {
		return []string{primary}
	}
	out := make([]string, 0, len(pool))
	for _, ua := range pool {
		ua = trimSpaceCopy(ua)
		if ua == "" {
			continue
		}
		out = append(out, ua)
	}
	if len(out) == 0 {
		return []string{primary}
	}
	return out
}

func trimSpaceCopy(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
