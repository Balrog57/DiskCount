package scanner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/normalize"
	"github.com/Balrog57/DiskCount/internal/rules"
	"github.com/Balrog57/DiskCount/internal/sources"
	"github.com/sony/gobreaker"
)

type Notifier interface {
	SendDeal(alert *db.Alert, deal domain.Deal, dec domain.NotificationDecision) error
}

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
	cfg   *config.Config
	db    *db.DB
	srcs  []sources.Source
	ntf   Notifier
	ntfMu sync.RWMutex

	mu        sync.RWMutex
	last      *ScanReport
	breakers  map[string]*gobreaker.CircuitBreaker
	breakerMu sync.Mutex

	// zeroStreak tracks consecutive scans that returned zero deals per source.
	// A source that has returned nothing for ZeroStreakThreshold scans is
	// flagged in the report so the admin knows its selectors may be broken.
	zeroStreak          map[string]int
	zeroStreakThreshold int
	// lastDealsCount caches the most recent deal count for each source so
	// the health endpoint can report it without re-running a scan.
	lastDealsCount map[string]int
	zeroStreakMu   sync.RWMutex
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
	}
}

func (s *Scanner) SetNotifier(n Notifier) {
	s.ntfMu.Lock()
	s.ntf = n
	s.ntfMu.Unlock()
}

func (s *Scanner) notifier() Notifier {
	s.ntfMu.RLock()
	defer s.ntfMu.RUnlock()
	return s.ntf
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
// and appends a SourceWarning once the threshold is crossed. The mutex
// guards both maps so SourceHealth() can read them
// concurrently from the web layer.
func (s *Scanner) recordScanResult(name string, deals int, r *ScanReport) {
	s.zeroStreakMu.Lock()
	s.lastDealsCount[name] = deals
	if deals > 0 {
		s.zeroStreak[name] = 0
		s.zeroStreakMu.Unlock()
		return
	}
	s.zeroStreak[name]++
	streak := s.zeroStreak[name]
	threshold := s.zeroStreakThreshold
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

}

// RecordSourceScanResult is the exported wrapper around recordScanResult.
// Tests outside the scanner package use it to seed the streak counters
// without going through a full RunOnce cycle.
func (s *Scanner) RecordSourceScanResult(name string, deals int) {
	s.recordScanResult(name, deals, &ScanReport{})
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
		var err error
		alerts, err = s.db.ListAlerts(ctx, true)
		if err != nil {
			slog.Warn("list alerts", "err", err)
			r.Errors = append(r.Errors, "list alerts: "+err.Error())
		}
	}
	// Snapshot every product's last_seen_at *before* we record new
	// observations. This is the baseline the back-in-stock heuristic
	// compares against: if a deal matches and the product has not been
	// seen for more than the configured threshold, the notification
	// gets a BackInStockHours hint so the user can tell the deal is a
	// return, not a regular price drop.
	backInStockThreshold := time.Duration(s.cfg.BackInStockHours * float64(time.Hour))
	lastSeenSnapshot := map[string]time.Time{}
	// ⚡ Bolt optimization: collect the unique product IDs once and
	// reuse the slice for both the last-seen snapshot and the batched
	// baseline-median lookup, so the whole batch costs two round-trips
	// instead of N. Both queries take the same deduplicated ID slice.
	var productIDs []string
	if s.db != nil && len(deals) > 0 {
		productIDs = make([]string, 0, len(deals))
		seen := make(map[string]struct{}, len(deals))
		for _, d := range deals {
			pid := d.ProductID()
			if _, ok := seen[pid]; ok {
				continue
			}
			seen[pid] = struct{}{}
			productIDs = append(productIDs, pid)
		}
	}
	if s.db != nil && backInStockThreshold > 0 && len(productIDs) > 0 {
		if m, err := s.db.LastSeenMap(ctx, productIDs); err == nil {
			lastSeenSnapshot = m
		} else {
			slog.Warn("lastseen snapshot", "err", err)
		}
	}
	// ⚡ Bolt optimization: fetch every 30-day median price-per-TB in
	// a single grouped query instead of one percentile_cont per deal.
	// Falls back to nil (no baseline) for products with no history.
	baselines := map[string]float64{}
	if s.db != nil && len(productIDs) > 0 {
		if m, err := s.db.BaselinePricePerTBMap(ctx, productIDs, now, 30); err == nil {
			baselines = m
		} else {
			slog.Warn("baseline map", "err", err)
		}
	}
	// ⚡ Batch the last-notification lookup for the whole scan in one
	// query instead of one indexed SELECT per (deal × matching alert).
	// We only need the enabled alert IDs; product IDs are the same slice
	// we already deduplicated above.
	lastNotifs := map[string]*db.Notification{}
	if s.db != nil && len(alerts) > 0 && len(productIDs) > 0 {
		alertIDs := make([]int64, 0, len(alerts))
		for i := range alerts {
			alertIDs = append(alertIDs, alerts[i].ID)
		}
		if m, err := s.db.LastNotificationsMap(ctx, alertIDs, productIDs); err == nil {
			lastNotifs = m
		} else {
			slog.Warn("last-notifications map", "err", err)
		}
	}
	for _, raw := range deals {
		res := normalize.Deal(raw)
		if res.Reject != nil {
			r.Rejected++
			r.RejectReasons[res.Reject.Reason]++
			if !dryRun && s.db != nil {
				if err := s.db.RecordRejectedDeal(ctx, res.Deal, res.Reject.Reason, res.Reject.Detail); err != nil {
					slog.Warn("record rejected deal", "src", res.Deal.Source, "err", err)
					r.Errors = append(r.Errors, "record rejected: "+err.Error())
				}
			}
			continue
		}
		deal := res.Deal
		r.Accepted++
		// Point lookup into the pre-fetched baseline map; nil out when
		// the product has no history so ShouldNotify behaves as before.
		var base *float64
		if v, ok := baselines[deal.ProductID()]; ok {
			base = &v
		}
		if !dryRun && s.db != nil {
			if err := s.db.UpsertProduct(ctx, deal); err != nil {
				// A failing product upsert should not lose the price
				// observation silently, but neither should it abort the
				// whole scan. Log + count, then continue.
				slog.Warn("upsert product", "src", deal.Source, "pid", deal.ProductID(), "err", err)
				r.Errors = append(r.Errors, "upsert product: "+err.Error())
				continue
			}
		}
		if !normalize.IsAlertQuality(deal) {
			if !dryRun && s.db != nil {
				// ⚡ Bolt optimization: product already upserted above; skip
				// the redundant product write and the wrapping transaction.
				if err := s.db.RecordObservationNoUpsert(ctx, deal); err != nil {
					slog.Warn("record observation", "src", deal.Source, "err", err)
					r.Errors = append(r.Errors, "record observation: "+err.Error())
				}
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
			last := lastNotifs[fmt.Sprintf("%d:%s", a.ID, deal.ProductID())]
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
			// ⚡ Bolt optimization: product already upserted earlier in the
			// loop; record just the notification row, skipping the redundant
			// product upsert + wrapping transaction.
			if err := s.db.RecordNotificationNoUpsert(ctx, a, deal, dec.Reason, dec.DiscountPct); err != nil {
				slog.Warn("record notification", "alert", a.ID, "pid", deal.ProductID(), "err", err)
				r.Errors = append(r.Errors, "record notification: "+err.Error())
				continue
			}
			r.Notified++
			if !a.DiscordEnabled {
				continue
			}
			ntf := s.notifier()
			if ntf == nil {
				r.Errors = append(r.Errors, "discord non configure")
				continue
			}
			if err := ntf.SendDeal(a, deal, dec); err != nil {
				slog.Warn("send discord", "alert", a.ID, "err", err)
				r.Errors = append(r.Errors, "envoi Discord: "+err.Error())
			}
		}
		if !dryRun && s.db != nil {
			// ⚡ Bolt optimization: see above; skip the redundant upsert.
			if err := s.db.RecordObservationNoUpsert(ctx, deal); err != nil {
				slog.Warn("record observation", "src", deal.Source, "err", err)
				r.Errors = append(r.Errors, "record observation: "+err.Error())
			}
		}
	}
}

func ScheduleLoop(ctx context.Context, s *Scanner, cron string) error {
	for {
		// Use the shared parser so the scheduler and the web dashboard's
		// "prochain scan" countdown always agree. If the spec is invalid
		// we fall back to the documented default rather than silently
		// changing cadence, and log once so the misconfiguration is visible.
		d, ok := config.ParseScrapeInterval(cron)
		if !ok {
			slog.Warn("invalid SCRAPE_INTERVAL_CRON; using default", "spec", cron, "default", config.DefaultScrapeInterval)
			d = config.DefaultScrapeInterval
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
