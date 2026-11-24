// Command api adalah HTTP server Backend_Lab (Gin, prefix /api/v1).
// Binary ini merakit seluruh modul Phase 1: auth, konten, CMS, dan progress.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/content"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/config"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/ratelimit"
	"github.com/rakhisyudha/lab/internal/platform/settings"
	"github.com/rakhisyudha/lab/internal/progress"
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
	progressHandler.RegisterRoutes(api.Group("/me", middleware.Authenticate()))
	adminGroup := api.Group("/admin", middleware.Authenticate(), auth.RequireRole("admin"))
	taskHandler.RegisterRoutes(api, adminGroup, middleware.Authenticate())

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
