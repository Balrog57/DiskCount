package sources

import (
	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/scraper"
)

type SourceFactory func(reg *Registry) Source

type Registry struct {
	cfg    *config.Config
	http   *scraper.HTTPFetcher
	byparr *scraper.ByparrClient
}

var registeredFactories []SourceFactory

func NewRegistry(cfg *config.Config) *Registry {
	return &Registry{
		cfg:    cfg,
		http:   scraper.NewHTTPFetcher(cfg.UserAgent, cfg.RequestTimeoutSeconds),
		byparr: scraper.NewByparrClient(cfg.ByparrURL),
	}
}

func (r *Registry) HTTP() *scraper.HTTPFetcher    { return r.http }
func (r *Registry) Byparr() *scraper.ByparrClient { return r.byparr }
func (r *Registry) Config() *config.Config        { return r.cfg }

func Register(fn SourceFactory) { registeredFactories = append(registeredFactories, fn) }

func BuildAll(reg *Registry) []Source {
	var out []Source
	for _, fn := range registeredFactories {
		if s := fn(reg); s != nil {
			out = append(out, s)
		}
	}
	return out
}
