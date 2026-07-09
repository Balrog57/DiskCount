package scanner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/i18n"
	"github.com/Balrog57/DiskCount/internal/normalize"
	"github.com/Balrog57/DiskCount/internal/notifier"
	"github.com/Balrog57/DiskCount/internal/rules"
	"github.com/Balrog57/DiskCount/internal/sources"
	"github.com/sony/gobreaker"
)

// Notifier is the subset of notifier.TelegramNotifier that the scanner
// needs. Defining it here breaks the import cycle that would otherwise
// exist between scanner and notifier if we used the concrete type, and
// makes the scanner trivially mockable in tests.
type Notifier interface {
	SendDeal(chatID int64, alert *db.Alert, deal domain.Deal, dec domain.NotificationDecision) error
	SendAdminMessage(chatID int64, text string) error
}

// SourceMetrics captures per-source statistics for the most recent scan.
type SourceMetrics struct {
	Name            string
	FetchDuration   time.Duration
	HTTPStatusCodes map[int]int
	RetryCount      int
	DealsFetched    int
	BreakerState    string
	BlockedByKeyword string
	Error           string
}

type ScanReport struct {
	Fetched, Accepted, Rejected, Matched, Notified, DryRunNotified int
	Errors                                                         []string
	RejectReasons                                                  map[string]int
	StartedAt, FinishedAt                                          time.Time
	DryRun                                                         bool
	SourceMetrics                                                  []SourceMetrics
	BreakerSkips                                                   map[string]string
	// SourceWarnings lists sources that returned zero deals for several
	// consecutive scans, hinting at broken selectors or a dead feed.
	SourceWarnings []SourceWarning
}

// SourceWarning flags a source whose reliability is in question.
type SourceWarning struct {
	Name             string
	ConsecutiveZeros int
	Message          string
}

// SourceHealthEntry is a snapshot of the current health state for a single
// source. It is exposed via Scanner.SourceHealth and the /api/sources/health
// web endpoint so operators can poll monitoring without scraping the HTML
// dashboard.
type SourceHealthEntry struct {
	Name             string `json:"name"`
	ConsecutiveZeros int    `json:"consecutive_zeros"`
	Flagged          bool   `json:"flagged"`
	LastDeals        int    `json:"last_deals_count"`
}

type Scanner struct {
	cfg  *config.Config
	db   *db.DB
	srcs []sources.Source
	ntf  Notifier

	mu       sync.RWMutex
	last     *ScanReport
	breakers map[string]*gobreaker.CircuitBreaker
	breakerMu sync.Mutex

	// zeroStreak tracks consecutive scans that returned zero deals per source.
	// A source that has returned nothing for ZeroStreakThreshold scans is
	// flagged in the report so the admin knows its selectors may be broken.
	zeroStreak          map[string]int
	zeroStreakThreshold int
	// lastDealsCount caches the most recent deal count for each source so
	// the health endpoint can report it without re-running a scan.
	lastDealsCount map[string]int
	// notifiedSources remembers whether we already paged the admin for a
	// given source's current streak, so a source that stays broken across
	// many scans does not flood Telegram. The flag is cleared as soon as
	// the source returns at least one deal.
	notifiedSources   map[string]bool
	zeroStreakMu      sync.RWMutex
}

func New(cfg *config.Config, dbase *db.DB, srcs []sources.Source, n Notifier) *Scanner {
	threshold := cfg.SourceHealthThreshold
	if threshold < 1 {
		threshold = 3
	}
	return &Scanner{
		cfg:                 cfg,
		db:                  dbase,
		srcs:                srcs,
		ntf:                 n,
		breakers:            make(map[string]*gobreaker.CircuitBreaker),
		zeroStreak:          make(map[string]int),
		zeroStreakThreshold: threshold,
		lastDealsCount:      make(map[string]int),
		notifiedSources:     make(map[string]bool),
	}
}

// ZeroStreakThreshold returns the current alerting threshold. It is exposed
// so the web admin can render the same value the scanner uses internally.
func (s *Scanner) ZeroStreakThreshold() int {
	if s.zeroStreakThreshold < 1 {
		return 3
	}
	return s.zeroStreakThreshold
}

// SourceHealth returns the current health snapshot for every configured
// source. Sources that have never been scanned yet appear with zero counts
// and ConsecutiveZeros=0.
func (s *Scanner) SourceHealth() []SourceHealthEntry {
	threshold := s.zeroStreakThreshold
	if threshold < 1 {
		threshold = 3
	}
	s.zeroStreakMu.RLock()
	defer s.zeroStreakMu.RUnlock()
	out := make([]SourceHealthEntry, 0, len(s.srcs))
	for _, src := range s.srcs {
		name := src.Name()
		streak := s.zeroStreak[name]
		last := s.lastDealsCount[name]
		out = append(out, SourceHealthEntry{
			Name:             name,
			ConsecutiveZeros: streak,
			Flagged:          streak >= threshold,
			LastDeals:        last,
		})
	}
	return out
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
	cp.SourceWarnings = append([]SourceWarning(nil), s.last.SourceWarnings...)
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

		// Track consecutive zero-deal scans to detect broken selectors.
		s.recordScanResult(src.Name(), len(deals), r)


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

// recordScanResult updates the zero-deal streak counters for a single source
// and, when the threshold is crossed for the first time in a streak, pushes
// an administrative Telegram message and appends a SourceWarning to the
// report. The mutex guards both maps so SourceHealth() can read them
// concurrently from the web layer.
func (s *Scanner) recordScanResult(name string, deals int, r *ScanReport) {
	s.zeroStreakMu.Lock()
	s.lastDealsCount[name] = deals
	if deals > 0 {
		s.zeroStreak[name] = 0
		s.notifiedSources[name] = false
		s.zeroStreakMu.Unlock()
		return
	}
	s.zeroStreak[name]++
	streak := s.zeroStreak[name]
	threshold := s.zeroStreakThreshold
	alreadyNotified := s.notifiedSources[name]
	s.zeroStreakMu.Unlock()

	if streak < threshold {
		return
	}
	msg := fmt.Sprintf("aucun deal depuis %d scans consecutifs (selecteurs possiblement casses)", streak)
	r.SourceWarnings = append(r.SourceWarnings, SourceWarning{
		Name:             name,
		ConsecutiveZeros: streak,
		Message:          msg,
	})
	slog.Warn("source health", "src", name, "consecutive_zeros", streak)

	if s.cfg.SourceHealthNotify && !alreadyNotified {
		s.notifyAdminSourceHealth(name, streak)
	}
}

// RecordSourceScanResult is the exported wrapper around recordScanResult.
// Tests outside the scanner package use it to seed the streak counters
// without going through a full RunOnce cycle.
func (s *Scanner) RecordSourceScanResult(name string, deals int) {
	s.recordScanResult(name, deals, &ScanReport{})
}

// notifyAdminSourceHealth sends a one-shot Telegram ping to the configured
// admin chat ID and marks the source as notified so the next scans in the
// same streak do not flood the channel. It is a no-op when the admin chat
// ID is unset, the notifier is nil, or delivery fails (we still log).
func (s *Scanner) notifyAdminSourceHealth(name string, streak int) {
	if s.ntf == nil {
		return
	}
	chatID, err := strconv.ParseInt(s.cfg.TelegramAdminChatID, 10, 64)
	if err != nil || chatID <= 0 {
		return
	}
	loc := i18n.ParseLocale(s.cfg.AdminLocale)
	if err := s.ntf.SendAdminMessage(chatID, notifier.FormatSourceHealthAlert(name, streak, loc)); err != nil {
		slog.Warn("admin notify", "src", name, "err", err)
		return
	}
	s.zeroStreakMu.Lock()
	s.notifiedSources[name] = true
	s.zeroStreakMu.Unlock()
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
	// Snapshot every product's last_seen_at *before* we record new
	// observations. This is the baseline the back-in-stock heuristic
	// compares against: if a deal matches and the product has not been
	// seen for more than the configured threshold, the notification
	// gets a BackInStockHours hint so the user can tell the deal is a
	// return, not a regular price drop.
	backInStockThreshold := time.Duration(s.cfg.BackInStockHours * float64(time.Hour))
	lastSeenSnapshot := map[string]time.Time{}
	if s.db != nil && backInStockThreshold > 0 && len(deals) > 0 {
		ids := make([]string, 0, len(deals))
		seen := make(map[string]struct{}, len(deals))
		for _, d := range deals {
			pid := d.ProductID()
			if _, ok := seen[pid]; ok {
				continue
			}
			seen[pid] = struct{}{}
			ids = append(ids, pid)
		}
		if m, err := s.db.LastSeenMap(ctx, ids); err == nil {
			lastSeenSnapshot = m
		} else {
			slog.Warn("lastseen snapshot", "err", err)
		}
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
		// Compute the absence duration *before* we record the new
		// observation. lastSeenSnapshot is taken at the start of the
		// batch so all candidates see the same baseline.
		var absence time.Duration
		if backInStockThreshold > 0 {
			if last, ok := lastSeenSnapshot[deal.ProductID()]; ok && !last.IsZero() {
				absence = now.Sub(last)
			}
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
			if absence >= backInStockThreshold {
				dec.BackInStockHours = absence.Hours()
				// Override the reason so the user-facing message
				// makes it obvious this is a restock, not a discount.
				dec.Reason = "back_in_stock"
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
