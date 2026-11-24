package httpx

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// AccessLog menulis satu baris log terstruktur per request setelah handler selesai.
// Field: request_id, user_id (bila ada), method, path, status, duration_ms.
// Query string, header, dan body tidak dicatat, sehingga token dan password tidak
// masuk log (Requirement 18.1, 18.4).
//
// Middleware ini harus dipasang setelah RequestID agar request_id tersedia.
func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		ctx := c.Request.Context()
		status := c.Writer.Status()
		attrs := []any{
			slog.String("request_id", RequestIDFrom(ctx)),
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		}
		if uid := UserIDFrom(ctx); uid != "" {
			attrs = append(attrs, slog.String("user_id", uid))
		}
		level := slog.LevelInfo
		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.String("error", c.Errors.String()))
		}
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		logger.Log(ctx, level, "http_request", attrs...)
	}
}

// Recovery mengubah panic menjadi internal_error. Stack trace hanya dicatat ke log
// dan tidak pernah dikirim ke client (Requirement 18.7). Middleware ini harus
// dipasang setelah RequestID dan AccessLog.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			logger.Error("panic_recovered",
				slog.String("request_id", RequestIDFrom(c.Request.Context())),
				slog.String("panic", fmt.Sprint(rec)),
				slog.String("stack", string(debug.Stack())),
			)
			if !c.Writer.Written() {
				Fail(c, apperr.New(apperr.CodeInternalError))
				return
			}
			c.Abort()
		}()
		c.Next()
	}
}

// CORS mengizinkan origin yang terdaftar di allowedOrigins (Requirement 18.5).
// Perbandingan origin tidak sensitif huruf besar-kecil. Permintaan preflight OPTIONS
// dijawab 204. Origin yang tidak terdaftar tidak menerima header CORS.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[strings.ToLower(strings.TrimSpace(o))] = struct{}{}
	}
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Add("Vary", "Origin")

		origin := c.GetHeader("Origin")
		_, originAllowed := allowed[strings.ToLower(origin)]
		originAllowed = originAllowed && origin != ""

		if originAllowed {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", "X-Request-ID, Retry-After, Content-Language")
		}

		if c.Request.Method == http.MethodOptions {
			if originAllowed {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID, Accept-Language")
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// BodyLimit membatasi ukuran body request (Requirement design 2.1, 256 KiB).
// Content-Length yang melebihi batas ditolak dengan payload_too_large. Body
// tanpa Content-Length dibatasi oleh http.MaxBytesReader; bind JSON mengubah
// kegagalan itu menjadi payload_too_large juga.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			Fail(c, apperr.New(apperr.CodePayloadTooLarge))
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
