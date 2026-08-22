package sources

import (
	"context"
	"log/slog"
	"time"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/scraper"
)

type SourceFactory func(reg *Registry) Source

type Registry struct {
	cfg    *config.Config
	http   scraper.Fetcher
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
		http:   retry, // sources get the retrying fetcher so RETRY_* config takes effect
		retry:  retry,
		byparr: scraper.NewByparrClient(cfg.ByparrURL),
	}
}

// HTTP returns the fetcher sources should use. It is the retrying variant,
// so every source that calls .Get() transparently honours the RETRY_*
// configuration. Use RawHTTP() in the rare case a caller needs the
// underlying *HTTPFetcher (e.g. to reach a private helper that is not on
// the Fetcher interface).
func (r *Registry) HTTP() scraper.Fetcher           { return r.http }
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
		if rl, ok := s.(RateLimitable); ok {
			reqs, period := rl.RateLimit()
			s = wrapRateLimited(s, reqs, period)
		}
		out = append(out, s)
	}
	return out
}

// rateLimitSource wraps a Source with a simple token-bucket rate limiter.
// It uses a background goroutine that ticks at period/reqs so the Source
// is never called more often than the declared limit.
type rateLimitSource struct {
	inner  Source
	ticker *time.Ticker
}

func wrapRateLimited(s Source, reqsPerPeriod int, period time.Duration) Source {
	interval := period / time.Duration(reqsPerPeriod)
	if interval <= 0 {
		interval = time.Second
	}
	return &rateLimitSource{
		inner:  s,
		ticker: time.NewTicker(interval),
	}
}

func (r *rateLimitSource) Name() string                          { return r.inner.Name() }
func (r *rateLimitSource) Info() SourceInfo                      { return infoOf(r.inner) }
func (r *rateLimitSource) HealthCheck(ctx context.Context) error { return healthCheckOf(r.inner, ctx) }

func (r *rateLimitSource) Fetch(ctx context.Context) ([]domain.Deal, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.ticker.C:
	}
	return r.inner.Fetch(ctx)
}

// infoOf returns the source info if the source implements Describable, or
// a minimal placeholder otherwise.
func infoOf(s Source) SourceInfo {
	if d, ok := s.(Describable); ok {
		return d.Info()
	}
	return SourceInfo{Name: s.Name()}
}

// healthCheckOf calls HealthCheck if the source implements it, otherwise
// returns nil (assumes the source is always healthy).
func healthCheckOf(s Source, ctx context.Context) error {
	if h, ok := s.(HealthCheckable); ok {
		return h.HealthCheck(ctx)
	}
	return nil
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
