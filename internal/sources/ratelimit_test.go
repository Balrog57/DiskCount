package sources

import (
	"context"
	"testing"
	"time"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestRateLimitableWrapperEnforcesDelay(t *testing.T) {
	inner := &testRateLimitedSource{name: "test-rl"}
	s := wrapRateLimited(inner, 1, time.Second)
	if s.Name() != "test-rl" {
		t.Fatalf("Name: got %q", s.Name())
	}

	// Fetch twice: second call must wait for the ticker.
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := s.Fetch(ctx)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	_, err = s.Fetch(ctx)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 900*time.Millisecond {
		t.Fatalf("expected rate-limited fetches to take >=900ms, took %v", elapsed)
	}
}

func TestRateLimitableInfoPassthrough(t *testing.T) {
	inner := &testRateLimitedSource{name: "discount"}
	s := wrapRateLimited(inner, 2, time.Second)
	d, ok := s.(Describable)
	if !ok {
		t.Fatal("wrapper must implement Describable when inner does")
	}
	info := d.Info()
	if info.Name != "discount" || info.Version != "test" {
		t.Fatalf("Info passthrough: %+v", info)
	}
}

type testRateLimitedSource struct {
	name string
}

func (s *testRateLimitedSource) Name() string                    { return s.name }
func (s *testRateLimitedSource) RateLimit() (int, time.Duration) { return 1, time.Second }
func (s *testRateLimitedSource) Info() SourceInfo                { return SourceInfo{Name: s.name, Version: "test"} }
func (s *testRateLimitedSource) Fetch(ctx context.Context) ([]domain.Deal, error) {
	return nil, nil
}
