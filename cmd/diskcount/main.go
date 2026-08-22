package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/notifier"
	"github.com/Balrog57/DiskCount/internal/scanner"
	"github.com/Balrog57/DiskCount/internal/sources"
	"github.com/Balrog57/DiskCount/internal/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	bootstrap := config.LoadBootstrap()
	dbase, err := db.New(ctx, bootstrap.DatabaseURL)
	if err != nil {
		slog.Error("db connect", "error", err)
		os.Exit(1)
	}
	defer dbase.Close()

	if err := dbase.Migrate(ctx); err != nil {
		slog.Error("db migrate", "error", err)
		os.Exit(1)
	}

	imported, err := dbase.ImportAppConfig(ctx, config.ImportableEnvValues())
	if err != nil {
		slog.Warn("app config import", "error", err)
	} else if imported > 0 {
		slog.Info("app config imported", "count", imported)
	}

	appValues, err := dbase.ListAppConfig(ctx)
	if err != nil {
		slog.Warn("app config load", "error", err)
	}
	cfg := config.LoadWithAppValues(appValues)
	cfg.DatabaseURL = bootstrap.DatabaseURL
	cfg.WebAdminAddr = bootstrap.WebAdminAddr

	if errs := cfg.Validate(); len(errs) > 0 {
		for _, err := range errs {
			slog.Warn("config validation", "err", err.Error())
		}
	}

	reg := sources.NewRegistry(cfg)
	srcs := sources.BuildAll(reg)
	slog.Info("sources loaded", "count", len(srcs))
	var sourceNames []string
	for _, src := range srcs {
		sourceNames = append(sourceNames, src.Name())
	}

	notify := notifier.NewDiscord(cfg.DiscordBotToken, cfg.DiscordChannelID)
	scan := scanner.New(cfg, dbase, srcs, notify)
	webSrv := web.New(dbase, scan, cfg, sourceNames)
	errCh := make(chan error, 1)
	go func() { errCh <- webSrv.Run(ctx, cfg.WebAdminAddr) }()

	go func() {
		slog.Info("initial scan starting")
		report := scan.RunOnce(context.Background(), false)
		slog.Info("initial scan done", "fetched", report.Fetched, "notified", report.Notified, "errors", len(report.Errors))
	}()
	go scanner.ScheduleLoop(ctx, scan, cfg.ScrapeIntervalCron)

	fmt.Println("DiskCount v2.0 running...")
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			slog.Error("web admin", "error", err)
			os.Exit(1)
		}
	}

	slog.Info("shutting down")
}
