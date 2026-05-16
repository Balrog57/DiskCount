package scanner

import (
	"context"
	"log/slog"
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
)

type ScanReport struct {
	Fetched, Accepted, Rejected, Matched, Notified, DryRunNotified int
	Errors                                                         []string
	RejectReasons                                                  map[string]int
	StartedAt, FinishedAt                                          time.Time
	DryRun                                                         bool
}

type Scanner struct {
	cfg  *config.Config
	db   *db.DB
	srcs []sources.Source
	ntf  *notifier.TelegramNotifier
	mu   sync.RWMutex
	last *ScanReport
}

func New(cfg *config.Config, dbase *db.DB, srcs []sources.Source, n *notifier.TelegramNotifier) *Scanner {
	return &Scanner{cfg: cfg, db: dbase, srcs: srcs, ntf: n}
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

func (s *Scanner) RunOnce(ctx context.Context, dryRun bool) *ScanReport {
	now := time.Now().UTC()
	r := &ScanReport{StartedAt: now, DryRun: dryRun, RejectReasons: make(map[string]int)}
	defer func() {
		r.FinishedAt = time.Now().UTC()
		s.mu.Lock()
		s.last = r
		s.mu.Unlock()
	}()
	for _, src := range s.srcs {
		ctx2, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.RequestTimeoutSeconds)*time.Second)
		deals, err := src.Fetch(ctx2)
		cancel()
		if err != nil {
			slog.Error("fetch", "src", src.Name(), "err", err)
			r.Errors = append(r.Errors, src.Name()+": "+err.Error())
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
