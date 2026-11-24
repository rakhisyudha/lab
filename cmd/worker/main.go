// Command worker menjalankan loop AITask dan job pemeliharaan.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/ai/evalresult"
	"github.com/rakhisyudha/lab/internal/ai/evaluator"
	"github.com/rakhisyudha/lab/internal/ai/gateway"
	"github.com/rakhisyudha/lab/internal/ai/jobs"
	"github.com/rakhisyudha/lab/internal/ai/provider"
	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	aiworker "github.com/rakhisyudha/lab/internal/ai/worker"
	"github.com/rakhisyudha/lab/internal/assessment"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/config"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/ratelimit"
	"github.com/rakhisyudha/lab/internal/platform/settings"
	"github.com/rakhisyudha/lab/internal/progress"
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
	settingsSvc := settings.New(settings.NewPostgresStore(pool), c, 30*time.Second)

	var limiter ratelimit.Store
	if cfg.RateLimitStore == config.StoreMemory {
		limiter = ratelimit.NewMemoryStore()
	} else {
		limiter = ratelimit.NewPostgresStore(pool)
	}

	var providers []provider.Provider
	rows, err := pool.Query(ctx, `SELECT provider_name, model_name FROM ai_provider_settings
		WHERE is_enabled ORDER BY fallback_order`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name, model string
		if err := rows.Scan(&name, &model); err != nil {
			rows.Close()
			return err
		}
		prefix := strings.ToUpper(name)
		providers = append(providers, provider.NewHTTPProvider(name,
			os.Getenv(prefix+"_BASE_URL"), model, os.Getenv(prefix+"_API_KEY")))
	}
	rows.Close()

	contracts, err := evaluator.LoadContracts(ctx, pool)
	if err != nil {
		return err
	}
	gw := gateway.New(providers, limiter, settingsSvc, c)
	evalSvc := evaluator.NewService(pool, gw, contracts, settingsSvc, c)
	masterySvc := progress.NewMasteryService(pool, settingsSvc)
	assessmentStore := assessment.NewStore(pool)
	finalizer := assessment.NewFinalizer(assessmentStore, masterySvc)
	resultSvc := evalresult.NewService(pool, evalSvc, finalizer, settingsSvc, c)

	queue := taskqueue.NewPostgresQueue(pool)
	runner := aiworker.New(aiworker.Options{
		Queue: queue, Logger: logger, Clock: c,
		Handlers: map[string]aiworker.Handler{
			"answer_evaluation": aiworker.AnswerEvaluationHandler(evalSvc, resultSvc),
		},
	})

	// Job pemeliharaan: purge bucket dan unscore jawaban failed > jendela retry.
	go func() {
		purge := time.NewTicker(time.Hour)
		unscore := time.NewTicker(15 * time.Minute)
		defer purge.Stop()
		defer unscore.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-purge.C:
				if err := ratelimit.PurgeIdleBuckets(ctx, pool, c.Now(), time.Hour); err != nil {
					logger.Error("worker: purge buckets", slog.Any("error", err))
				}
			case <-unscore.C:
				n, err := jobs.UnscoreExpiredFailed(ctx, pool, settingsSvc, c.Now(),
					func(ctx context.Context, attemptID uuid.UUID, now time.Time) error {
						return finalizer.TryFinalize(ctx, attemptID, now)
					})
				if err != nil {
					logger.Error("worker: unscore", slog.Any("error", err))
				} else if n > 0 {
					logger.Info("worker: unscore selesai", slog.Int("attempts", n))
				}
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() { errCh <- runner.Run(ctx) }()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-signals:
	}

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
