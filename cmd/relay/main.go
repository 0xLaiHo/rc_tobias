package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xLaiHo/rc_tobias/internal/outbox"
	"github.com/0xLaiHo/rc_tobias/internal/platform"
	"github.com/0xLaiHo/rc_tobias/internal/stream"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := platform.LoadConfig()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	db, entClient, err := platform.OpenDatabase(ctx, cfg)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	defer entClient.Close()

	if err := platform.Migrate(ctx, db, entClient); err != nil {
		slog.Error("migrate database", "error", err)
		os.Exit(1)
	}

	redisClient := platform.NewRedisClient(cfg)
	defer redisClient.Close()

	relay := outbox.NewRelay(
		outbox.NewPostgresStore(db),
		stream.NewPublisher(redisClient, cfg.StreamName),
		outbox.RelayConfig{BatchSize: 50, LockerID: hostname("relay")},
	)

	ticker := time.NewTicker(cfg.RelayPollInterval)
	defer ticker.Stop()
	slog.Info("relay started", "poll_interval", cfg.RelayPollInterval)
	for {
		if err := relay.PublishDue(ctx); err != nil {
			slog.Error("publish due outbox events", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func hostname(fallback string) string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return fallback
	}
	return name
}
