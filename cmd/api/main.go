package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xLaiHo/rc_tobias/internal/api"
	"github.com/0xLaiHo/rc_tobias/internal/notification"
	"github.com/0xLaiHo/rc_tobias/internal/platform"
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
	service := notification.NewService(repo)
	router := api.NewRouterWithReadiness(service, func(ctx context.Context) error {
		if err := db.PingContext(ctx); err != nil {
			return err
		}
		return redisClient.Ping(ctx).Err()
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown api server", "error", err)
		}
	}()

	slog.Info("api listening", "addr", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("api server failed", "error", err)
		os.Exit(1)
	}
}
