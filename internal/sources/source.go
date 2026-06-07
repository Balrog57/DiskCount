package sources

import (
	"context"
	"time"

	"github.com/Balrog57/DiskCount/internal/domain"
)

type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]domain.Deal, error)
}

// SourceInfo describes a source's metadata. It is exposed via the optional
// Describable interface so that new sources can be documented without
// breaking the base interface. The web admin and the registry use it to
// build catalogs and to skip sources whose required config keys are
// missing.
type SourceInfo struct {
	Name        string
	Description string
	Categories  []string
	Requires    []string
	Version     string
}

// Describable is an optional interface. A source that implements it will be
// introspected by BuildAll to populate the catalog. Sources that don't
// implement it fall back to Name() only.
type Describable interface {
	Info() SourceInfo
}

// HealthCheckable is an optional interface. A source that implements it
// can be probed by the registry before a scan: a non-nil error skips the
// source for the current run. Sources that don't implement it are always
// assumed healthy.
type HealthCheckable interface {
	HealthCheck(ctx context.Context) error
}

// RateLimitable is an optional interface. A source that implements it
// declares how many requests it can make per period; the registry wraps
// the source with a token-bucket limiter. Sources that don't implement it
// are unlimited.
type RateLimitable interface {
	RateLimit() (requestsPerPeriod int, period time.Duration)
}
