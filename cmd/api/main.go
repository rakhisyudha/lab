// Command api adalah HTTP server Backend_Lab (Gin, prefix /api/v1).
// Binary ini merakit seluruh modul Phase 1: auth, konten, CMS, dan progress.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/admin"
	"github.com/rakhisyudha/lab/internal/ai/evaluator"
	"github.com/rakhisyudha/lab/internal/ai/gateway"
	"github.com/rakhisyudha/lab/internal/ai/provider"
	"github.com/rakhisyudha/lab/internal/ai/quota"
	"github.com/rakhisyudha/lab/internal/ai/schema"
	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	"github.com/rakhisyudha/lab/internal/assessment"
	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/content"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/golden"
	"github.com/rakhisyudha/lab/internal/interview"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/config"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/ratelimit"
	"github.com/rakhisyudha/lab/internal/platform/settings"
	"github.com/rakhisyudha/lab/internal/progress"
	"github.com/rakhisyudha/lab/internal/recommendation"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	withWorker := flag.Bool("with-worker", false, "jalankan worker di proses yang sama (single-instance/dev)")
	flag.Parse()
	_ = withWorker // worker AITask dipasang pada Phase 2.

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
		return fmt.Errorf("api: koneksi database: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c := clock.System{}
	settingsSvc := settings.New(settings.NewPostgresStore(pool), c, 30*time.Second)

	authStore := auth.NewStore(pool)
	issuer := auth.NewTokenIssuer(cfg.JWTSigningKey.Reveal())
	authSvc := auth.NewService(authStore, issuer, settingsSvc, c, auth.DefaultPasswordPolicy(), 0)
	authHandler := auth.NewHandler(authSvc)
	middleware := auth.NewMiddleware(issuer)

	var limiter ratelimit.Store
	if cfg.RateLimitStore == config.StoreMemory {
		limiter = ratelimit.NewMemoryStore()
	} else {
		limiter = ratelimit.NewPostgresStore(pool)
	}
	authLimiter := ratelimit.NewMiddleware(limiter, "auth", 10, 10.0/60.0, c)
	generalLimiter := ratelimit.NewMiddleware(limiter, "general", 120, 2, c)

	postgresQueue := taskqueue.NewPostgresQueue(pool)
	taskHandler := taskqueue.NewHandler(postgresQueue)

	contentHandler := content.NewHandler(content.NewStore(pool))
	progressHandler := progress.NewHandler(progress.NewStore(pool))

	engine, api := httpx.NewRouter(httpx.Options{
		Logger:             logger,
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		HealthCheck:        func(ctx context.Context) error { return pool.Ping(ctx) },
	})

	api.Use(generalLimiter)
	authHandler.RegisterRoutes(api, middleware, authLimiter)
	contentHandler.RegisterRoutes(api, api.Group("/admin", middleware.Authenticate(), auth.RequireRole("admin")))
	meGroup := api.Group("/me", middleware.Authenticate())
	progressHandler.RegisterRoutes(meGroup)
	adminGroup := api.Group("/admin", middleware.Authenticate(), auth.RequireRole("admin"))

	masterySvc := progress.NewMasteryService(pool, settingsSvc)
	assessmentStore := assessment.NewStore(pool)
	assessmentSvc := assessment.NewService(assessmentStore, postgresQueue,
		quota.NewManager(pool, settingsSvc, c), settingsSvc, c)
	assessment.NewHandler(assessmentSvc, assessmentStore).RegisterRoutes(api, middleware.Authenticate())
	progressHandler.RegisterAdminRoutes(adminGroup, masterySvc)
	recommendation.NewHandler(pool, settingsSvc).RegisterRoutes(meGroup)

	// Versi Prompt_Contract aktif di-pin pada setiap sesi interview.
	contractVersions := map[string]int{}
	contractRows, err := pool.Query(ctx, `SELECT task_type, version FROM prompt_contracts WHERE status='active'`)
	if err != nil {
		return err
	}
	for contractRows.Next() {
		var taskType string
		var version int
		if err := contractRows.Scan(&taskType, &version); err != nil {
			contractRows.Close()
			return err
		}
		contractVersions[taskType] = version
	}
	contractRows.Close()

	interviewStore := interview.NewStore(pool)
	interviewFinalizer := interview.NewFinalizer(interviewStore, postgresQueue, masterySvc, c)
	interviewEvalSvc := interview.NewEvaluationService(interviewStore, postgresQueue, c, interviewFinalizer)
	interview.NewHandler(interview.NewService(interviewStore, postgresQueue,
		quota.NewManager(pool, settingsSvc, c), settingsSvc, c, contractVersions), interviewStore).
		RegisterRoutes(api, middleware.Authenticate(), interviewEvalSvc, interviewFinalizer)
	taskHandler.RegisterRoutes(api, adminGroup, middleware.Authenticate())

	goldenStore := golden.NewStore(pool)

	// Provider aktif dibaca dari ai_provider_settings; kredensial dari environment.
	var providers []provider.Provider
	providerRows, err := pool.Query(ctx, `SELECT provider_name, model_name FROM ai_provider_settings
		WHERE is_enabled ORDER BY fallback_order`)
	if err != nil {
		return err
	}
	for providerRows.Next() {
		var name, model string
		if err := providerRows.Scan(&name, &model); err != nil {
			providerRows.Close()
			return err
		}
		prefix := strings.ToUpper(name)
		providers = append(providers, provider.NewHTTPProvider(name,
			os.Getenv(prefix+"_BASE_URL"), model, os.Getenv(prefix+"_API_KEY")))
	}
	providerRows.Close()

	contracts, err := evaluator.LoadContracts(ctx, pool)
	if err != nil {
		return err
	}
	gw := gateway.New(providers, limiter, settingsSvc, c)
	evalSvc := evaluator.NewService(pool, gw, contracts, settingsSvc, c)
	liveEvaluator := func(ctx context.Context, cs golden.Case) (schema.Output, evaluation.AnswerScore, error) {
		owner := uuid.Nil
		if err := pool.QueryRow(ctx, `SELECT owner_id FROM golden_cases WHERE id = $1`, cs.ID).Scan(&owner); err != nil {
			return schema.Output{}, evaluation.AnswerScore{}, err
		}
		var topicID uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT topic_id FROM questions WHERE id = $1`, cs.QuestionID).Scan(&topicID); err != nil {
			return schema.Output{}, evaluation.AnswerScore{}, err
		}
		payload, _ := json.Marshal(evaluator.Payload{
			QuestionID: cs.QuestionID, UserID: owner, TopicID: topicID,
			AnswerText: cs.SampleAnswer, Locale: cs.Locale,
		})
		res, err := evalSvc.Evaluate(ctx, taskqueue.Task{Payload: payload, MaxAttempts: 5})
		if err != nil {
			return schema.Output{}, evaluation.AnswerScore{}, err
		}
		return res.Gateway.Output, res.Score, nil
	}
	goldenRunner := golden.NewRunner(goldenStore, settingsSvc, c, liveEvaluator)
	golden.NewHandler(goldenStore, goldenRunner).RegisterRoutes(adminGroup)
	admin.NewHandler(pool, settingsSvc).RegisterRoutes(adminGroup)

	addr := os.Getenv("PORT")
	if addr == "" {
		addr = ":8080"
	} else if addr[0] != ':' {
		addr = ":" + addr
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api: server siap", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-signals:
	}

	shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	return srv.Shutdown(shutdownCtx)
}
