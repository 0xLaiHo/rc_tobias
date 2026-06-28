package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/0xLaiHo/rc_tobias/internal/delivery"
	"github.com/0xLaiHo/rc_tobias/internal/notification"
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

	repo := notification.NewEntRepository(entClient)
	processor := delivery.NewProcessor(
		repo,
		delivery.NewHTTPDispatcher(cfg.HTTPTimeout),
		delivery.DefaultRetryPolicy(),
		time.Now,
	)

	var wg sync.WaitGroup
	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}
	slog.Info("worker process started", "workers", workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		consumerName := hostname("worker") + "-" + strconv.Itoa(i)
		go func(name string) {
			defer wg.Done()
			consumer := stream.NewConsumer(redisClient, cfg.StreamName, cfg.ConsumerGroup, name, processor)
			if err := consumer.EnsureGroup(ctx); err != nil {
				slog.Error("ensure redis stream group", "consumer", name, "error", err)
				return
			}
			for ctx.Err() == nil {
				hadError := false
				if err := consumer.ClaimAndProcess(ctx, 30*time.Second, 10); err != nil && ctx.Err() == nil {
					hadError = true
					slog.Error("claim pending redis messages", "consumer", name, "error", err)
				}
				if err := consumer.ReadAndProcess(ctx, 10, cfg.WorkerBlockTimeout); err != nil && ctx.Err() == nil {
					hadError = true
					slog.Error("read redis messages", "consumer", name, "error", err)
				}
				if hadError {
					select {
					case <-ctx.Done():
					case <-time.After(time.Second):
					}
				}
			}
		}(consumerName)
	}
	wg.Wait()
}

func hostname(fallback string) string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return fallback
	}
	return name
}
