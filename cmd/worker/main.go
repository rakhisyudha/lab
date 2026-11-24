// Command worker menjalankan loop AITask dan job pemeliharaan.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	aiworker "github.com/rakhisyudha/lab/internal/ai/worker"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/config"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/ratelimit"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL.Reveal())
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("worker: koneksi database: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c := clock.System{}
	queue := taskqueue.NewPostgresQueue(pool)

	// Job pemeliharaan bucket berjalan bersamaan dengan loop AITask.
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := ratelimit.PurgeIdleBuckets(ctx, pool, c.Now(), time.Hour); err != nil {
					logger.Error("worker: purge buckets", slog.Any("error", err))
				}
			}
		}
	}()

	runner := aiworker.New(aiworker.Options{
		Queue: queue, Handlers: map[string]aiworker.Handler{}, Logger: logger, Clock: c,
	})

	errCh := make(chan error, 1)
	go func() { errCh <- runner.Run(ctx) }()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-signals:
	}

	// Graceful shutdown: beri waktu 30 detik bagi handler yang berjalan selesai.
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	select {
	case err := <-errCh:
		return err
	case <-shutdownCtx.Done():
		logger.Warn("worker: shutdown timeout")
		return nil
	}
}
