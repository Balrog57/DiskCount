package scanner

import (
	"context"
	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/notifier"
	"github.com/Balrog57/DiskCount/internal/rules"
	"github.com/Balrog57/DiskCount/internal/sources"
	"log/slog"
	"strings"
	"time"
)

type ScanReport struct {
	Fetched, Matched, Notified, DryRunNotified int
	Errors                                     []string
}

type Scanner struct {
	cfg  *config.Config
	db   *db.DB
	srcs []sources.Source
	ntf  *notifier.TelegramNotifier
}

func New(cfg *config.Config, dbase *db.DB, srcs []sources.Source, n *notifier.TelegramNotifier) *Scanner {
	return &Scanner{cfg: cfg, db: dbase, srcs: srcs, ntf: n}
}
func (s *Scanner) Sources() []sources.Source { return s.srcs }

func (s *Scanner) RunOnce(ctx context.Context, dryRun bool) *ScanReport {
	now := time.Now().UTC()
	r := &ScanReport{}
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
	alerts, _ := s.db.ListAlerts(ctx, true)
	for _, deal := range deals {
		base, _ := s.db.BaselinePricePerTB(ctx, deal.ProductID(), now, 30)
		if !dryRun {
			s.db.UpsertProduct(ctx, deal)
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
		if !dryRun {
			s.db.RecordObservation(ctx, deal)
		}
	}
}

func ScheduleLoop(ctx context.Context, s *Scanner, cron string) error {
	for {
		r := s.RunOnce(ctx, false)
		slog.Info("scan", "fetched", r.Fetched, "matched", r.Matched, "notified", r.Notified, "errors", len(r.Errors))
		d, err := parDur(cron)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
}

func parDur(c string) (time.Duration, error) {
	if a, ok := strings.CutPrefix(c, "@every "); ok {
		return time.ParseDuration(a)
	}
	return 4 * time.Hour, nil
}
