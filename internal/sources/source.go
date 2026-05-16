package sources

import (
	"context"
	"github.com/Balrog57/DiskCount/internal/domain"
)

type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]domain.Deal, error)
}
