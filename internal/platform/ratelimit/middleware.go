package ratelimit

import (
	"math"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// NewMiddleware memasang pembatas laju untuk satu kelas endpoint (auth, ai,
// general). Kunci bucket berbentuk "http:<class>:<ip>" dan setiap request
// memakan satu token. Waktu diambil dari clock yang disuntikkan agar dapat diuji.
func NewMiddleware(store Store, class string, capacity, ratePerSec float64, c clock.Clock) gin.HandlerFunc {
	if c == nil {
		c = clock.System{}
	}
	return func(ctx *gin.Context) {
		key := "http:" + class + ":" + ctx.ClientIP()
		wait, err := store.TryTake(ctx.Request.Context(), key, capacity, ratePerSec, 1, c.Now())
		if err != nil {
			httpx.Fail(ctx, err)
			return
		}
		if wait > 0 {
			// Header harus ditulis sebelum Fail karena Fail langsung menulis response.
			ctx.Header("Retry-After", retryAfter(wait))
			httpx.Fail(ctx, apperr.New(apperr.CodeRateLimited))
			return
		}
		ctx.Next()
	}
}

// retryAfter mengubah wait menjadi jumlah detik bulat ke atas (minimal 1).
// Permintaan yang mustahil terpenuhi (cost > kapasitas) diberi nilai 1 agar
// klien mencoba lagi dengan jeda singkat, bukan menunggu waktu tak hingga.
func retryAfter(wait time.Duration) string {
	if isInfiniteWait(wait) || wait < 0 {
		return "1"
	}
	seconds := int64(math.Ceil(wait.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10)
}
