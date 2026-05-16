package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Balrog57/DiskCount/internal/bot"
	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/notifier"
	"github.com/Balrog57/DiskCount/internal/scanner"
	"github.com/Balrog57/DiskCount/internal/sources"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	cfg := config.Get()
	if cfg.TelegramBotToken == "" {
		slog.Error("TELEGRAM_BOT_TOKEN is required")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	dbase, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect", "error", err)
		os.Exit(1)
	}
	defer dbase.Close()

	if err := dbase.Migrate(ctx); err != nil {
		slog.Warn("db migrate", "error", err)
	}

	reg := sources.NewRegistry(cfg)
	srcs := sources.BuildAll(reg)
	slog.Info("sources loaded", "count", len(srcs))

	notify := notifier.New(nil, cfg.TelegramMessageDelayS)
	scan := scanner.New(cfg, dbase, srcs, notify)

	b, err := bot.New(cfg, dbase, scan)
	if err != nil {
		slog.Error("bot create", "error", err)
		os.Exit(1)
	}
	notify.Bot = b.TB

	go func() {
		slog.Info("initial scan starting")
		report := scan.RunOnce(context.Background(), false)
		slog.Info("initial scan done", "fetched", report.Fetched, "notified", report.Notified, "errors", len(report.Errors))
	}()

	go scanner.ScheduleLoop(ctx, scan, cfg.ScrapeIntervalCron)

	fmt.Println("DiskCount v2.0 running...")
	go b.TB.Start()
	<-ctx.Done()

	slog.Info("shutting down")
}
