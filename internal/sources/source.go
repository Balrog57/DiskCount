package sources

import (
	"context"
	"github.com/MarcPartensky/DiskCount/internal/domain"
)

type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]domain.Deal, error)
}
