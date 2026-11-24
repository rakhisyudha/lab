// Package httpx berisi komponen HTTP bersama: Error_Envelope, Request_ID, access log,
// recovery, CORS, batas ukuran body, binding JSON, dan health check (design 2.1, 2.2).
package httpx

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIPrefix adalah prefix seluruh endpoint API (Requirement 9.1).
const APIPrefix = "/api/v1"

// MaxBodyBytes adalah batas ukuran body default: 256 KiB (design 2.1).
const MaxBodyBytes int64 = 256 << 10

// Options adalah dependensi router HTTP.
type Options struct {
	// Logger menerima access log dan log error. Nil memakai slog.Default().
	Logger *slog.Logger
	// CORSAllowedOrigins adalah daftar origin yang diizinkan (Requirement 18.5).
	CORSAllowedOrigins []string
	// HealthCheck dipanggil oleh GET /api/v1/health. Nil berarti selalu sehat.
	// Error hanya dicatat ke log, tidak dikirim ke client.
	HealthCheck func(ctx context.Context) error
	// MaxBodyBytes membatasi ukuran body. Nol memakai MaxBodyBytes (256 KiB).
	MaxBodyBytes int64
}

// NewRouter membuat engine Gin dengan urutan middleware:
// RequestID, AccessLog, Recovery, CORS, BodyLimit. Urutan ini memastikan log mencatat
// status final, panic tetap menghasilkan envelope dengan request_id, dan preflight
// CORS tidak menyentuh body.
//
// Engine mengembalikan Error_Envelope untuk rute yang tidak ada. Grup kedua
// adalah grup /api/v1 tempat endpoint domain didaftarkan.
func NewRouter(opts Options) (*gin.Engine, *gin.RouterGroup) {
	maxBody := opts.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = MaxBodyBytes
	}

	engine := gin.New()
	engine.Use(
		RequestID(),
		AccessLog(opts.Logger),
		Recovery(opts.Logger),
		CORS(opts.CORSAllowedOrigins),
		BodyLimit(maxBody),
	)
	engine.NoRoute(NotFound())

	api := engine.Group(APIPrefix)
	api.GET("/health", Health(opts.HealthCheck))
	return engine, api
}

// HealthBody adalah response health check. Hanya memuat status, tanpa konfigurasi
// internal (Requirement 10.11).
type HealthBody struct {
	Status string `json:"status"`
}

// Health mengembalikan 200 dengan status "ok", atau 503 dengan status "unavailable"
// bila check gagal. Endpoint ini tidak memerlukan autentikasi.
func Health(check func(ctx context.Context) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		if check != nil {
			if err := check(c.Request.Context()); err != nil {
				_ = c.Error(err)
				c.JSON(http.StatusServiceUnavailable, HealthBody{Status: "unavailable"})
				return
			}
		}
		c.JSON(http.StatusOK, HealthBody{Status: "ok"})
	}
}
