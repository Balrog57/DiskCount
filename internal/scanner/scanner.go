package scanner

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/normalize"
	"github.com/Balrog57/DiskCount/internal/notifier"
	"github.com/Balrog57/DiskCount/internal/rules"
	"github.com/Balrog57/DiskCount/internal/sources"
	"github.com/sony/gobreaker"
)

// SourceMetrics captures per-source statistics for the most recent scan.
type SourceMetrics struct {
	Name             string
	FetchDuration    time.Duration
	HTTPStatusCodes  map[int]int
	RetryCount       int
	DealsFetched     int
	BreakerState     string
	BlockedByKeyword string
	Error            string
}

type ScanReport struct {
	Fetched, Accepted, Rejected, Matched, Notified, DryRunNotified int
	Errors                                                         []string
	RejectReasons                                                  map[string]int
	StartedAt, FinishedAt                                          time.Time
	DryRun                                                         bool
	SourceMetrics                                                  []SourceMetrics
	BreakerSkips                                                   map[string]string
}

type Scanner struct {
	cfg  *config.Config
	db   *db.DB
	srcs []sources.Source
	ntf  *notifier.TelegramNotifier

	mu        sync.RWMutex
	last      *ScanReport
	breakers  map[string]*gobreaker.CircuitBreaker
	breakerMu sync.Mutex
}

func New(cfg *config.Config, dbase *db.DB, srcs []sources.Source, n *notifier.TelegramNotifier) *Scanner {
	return &Scanner{
		cfg:      cfg,
		db:       dbase,
		srcs:     srcs,
		ntf:      n,
		breakers: make(map[string]*gobreaker.CircuitBreaker),
	}
}

func (s *Scanner) Sources() []sources.Source { return s.srcs }
func (s *Scanner) LastReport() *ScanReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.last == nil {
		return nil
	}
	cp := *s.last
	cp.Errors = append([]string(nil), s.last.Errors...)
	cp.RejectReasons = make(map[string]int, len(s.last.RejectReasons))
	for k, v := range s.last.RejectReasons {
		cp.RejectReasons[k] = v
	}
	return &cp
}

func (s *Scanner) breakerFor(name string) *gobreaker.CircuitBreaker {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	if cb, ok := s.breakers[name]; ok {
		return cb
	}
	timeout := time.Duration(s.cfg.CircuitBreakerTimeoutS * float64(time.Second))
	if !s.cfg.CircuitBreakerEnabled {
		// Return a never-tripping breaker so call sites stay simple.
		cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        name,
			Timeout:     timeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool { return false },
		})
		s.breakers[name] = cb
		return cb
	}
	threshold := uint32(s.cfg.CircuitBreakerThreshold)
	if threshold < 1 {
		threshold = 5
	}
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:    name,
		Timeout: timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= threshold
		},
	})
	s.breakers[name] = cb
	return cb
}

// BreakerSnapshot returns a per-source snapshot of breaker state.
func (s *Scanner) BreakerSnapshot() map[string]string {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	out := make(map[string]string, len(s.breakers))
	for name, cb := range s.breakers {
		out[name] = cb.State().String()
	}
	return out
}

// ResetBreaker forces a breaker into the closed state. Used by the web admin
// or recovery paths.
func (s *Scanner) ResetBreaker(name string) {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	if _, ok := s.breakers[name]; ok {
		// gobreaker doesn't expose a direct reset, so we drop the entry:
		// a fresh breaker is created on the next lookup, which is in the
		// closed state.
		delete(s.breakers, name)
	}
}

func (s *Scanner) RunOnce(ctx context.Context, dryRun bool) *ScanReport {
	now := time.Now().UTC()
	r := &ScanReport{
		StartedAt:     now,
		DryRun:        dryRun,
		RejectReasons: make(map[string]int),
		BreakerSkips:  make(map[string]string),
	}
	defer func() {
		r.FinishedAt = time.Now().UTC()
		s.mu.Lock()
		s.last = r
		s.mu.Unlock()
	}()

	for _, src := range s.srcs {
		// Apply a small randomised jitter between sources to avoid the
		// pattern of every source firing at the same instant, which is
		// easy to fingerprint.
		s.jitterBefore(ctx)

		ctx2, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.RequestTimeoutSeconds)*time.Second)
		metrics, deals, err := s.fetchWithBreaker(ctx2, src)
		cancel()

		metrics.FetchDuration = time.Since(now)
		r.SourceMetrics = append(r.SourceMetrics, metrics)

		if err != nil {
			if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
				slog.Warn("source breaker open", "src", src.Name())
				r.BreakerSkips[src.Name()] = "open"
				r.Errors = append(r.Errors, src.Name()+": circuit open")
				continue
			}
			slog.Error("fetch", "src", src.Name(), "err", err)
			r.Errors = append(r.Errors, src.Name()+": "+err.Error())
			metrics.Error = err.Error()
			continue
		}
		slog.Info("fetched", "src", src.Name(), "n", len(deals))
		r.Fetched += len(deals)
		if len(deals) > 0 {
			s.proc(ctx, deals, now, dryRun, r)
		}
	}
	return r
}

func (s *Scanner) jitterBefore(ctx context.Context) {
	// 0.5s ± 0.25s by default; bounded by 1.5s. The base value is derived
	// from the source timeout so that quick sources still get some jitter
	// without slowing down the whole scan meaningfully.
	base := 500 * time.Millisecond
	if d := time.Duration(s.cfg.RequestTimeoutSeconds*float64(time.Second)) / 8; d > 0 && d < base {
		base = d
	}
	jitter := time.Duration((rand.Float64()*2 - 1) * float64(base) * 0.5)
	if jitter < 0 {
		jitter = -jitter
	}
	delay := base/2 + jitter
	if delay <= 0 {
		return
	}
	if delay > 1500*time.Millisecond {
		delay = 1500 * time.Millisecond
	}
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}

func (s *Scanner) fetchWithBreaker(ctx context.Context, src sources.Source) (SourceMetrics, []domain.Deal, error) {
	metrics := SourceMetrics{
		Name:            src.Name(),
		HTTPStatusCodes: map[int]int{},
	}
	cb := s.breakerFor(src.Name())
	metrics.BreakerState = cb.State().String()

	res, err := cb.Execute(func() (interface{}, error) {
		return src.Fetch(ctx)
	})
	if err != nil {
		return metrics, nil, err
	}
	deals, _ := res.([]domain.Deal)
	metrics.DealsFetched = len(deals)
	return metrics, deals, nil
}

func (s *Scanner) proc(ctx context.Context, deals []domain.Deal, now time.Time, dryRun bool, r *ScanReport) {
	var alerts []db.Alert
	if s.db != nil {
		alerts, _ = s.db.ListAlerts(ctx, true)
	}
	for _, raw := range deals {
		res := normalize.Deal(raw)
		if res.Reject != nil {
			r.Rejected++
			r.RejectReasons[res.Reject.Reason]++
			if !dryRun && s.db != nil {
				_ = s.db.RecordRejectedDeal(ctx, res.Deal, res.Reject.Reason, res.Reject.Detail)
			}
			continue
		}
		deal := res.Deal
		r.Accepted++
		var base *float64
		if s.db != nil {
			base, _ = s.db.BaselinePricePerTB(ctx, deal.ProductID(), now, 30)
		}
		if !dryRun && s.db != nil {
			s.db.UpsertProduct(ctx, deal)
		}
		if !normalize.IsAlertQuality(deal) {
			if !dryRun && s.db != nil {
				s.db.RecordObservation(ctx, deal)
			}
			continue
		}
		for i := range alerts {
			a := &alerts[i]
			if !rules.AlertMatches(a, deal) {
				continue
			}
			r.Matched++
			last, _ := s.db.LastNotification(ctx, a.ID, deal.ProductID())
			dec := rules.ShouldNotify(a, deal, base, last, now, s.cfg.NotificationPriceDropPct)
			if !dec.ShouldNotify {
				continue
			}
			if dryRun {
				r.DryRunNotified++
				continue
			}
			if s.ntf != nil {
				s.ntf.SendDeal(a.ChatID, a, deal, dec)
				time.Sleep(time.Duration(s.cfg.TelegramMessageDelayS * float64(time.Second)))
			}
			s.db.RecordNotification(ctx, a, deal, dec.Reason, dec.DiscountPct)
			r.Notified++
		}
		if !dryRun && s.db != nil {
			s.db.RecordObservation(ctx, deal)
		}
	}
}

func ScheduleLoop(ctx context.Context, s *Scanner, cron string) error {
	for {
		d, err := parDur(cron)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
		r := s.RunOnce(ctx, false)
		slog.Info("scan", "fetched", r.Fetched, "matched", r.Matched, "notified", r.Notified, "errors", len(r.Errors))
	}
}

func parDur(c string) (time.Duration, error) {
	if a, ok := strings.CutPrefix(c, "@every "); ok {
		return time.ParseDuration(a)
	}
	return 4 * time.Hour, nil
}
