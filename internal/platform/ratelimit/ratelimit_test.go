package ratelimit

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"pgregory.net/rapid"

	"github.com/rakhisyudha/lab/internal/platform/clock"
)

// refilledTokens menghitung isi ulang sesuai spesifikasi TryTake. Dipakai untuk
// membuktikan bahwa penolakan tidak pernah mengurangi token.
func refilledTokens(b Bucket, now time.Time) float64 {
	elapsed := now.Sub(b.UpdatedAt).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	tokens := b.Tokens + elapsed*b.RatePerSec
	switch {
	case tokens < 0:
		return 0
	case tokens > b.Capacity:
		return b.Capacity
	default:
		return tokens
	}
}

// TestPropW20_TokenBucketMenjagaBatasKapasitasDanLaju adalah Property 20 (wajib,
// area 6: token bucket dan backoff). Kapasitas, laju, selisih waktu non-negatif,
// dan biaya dibangkitkan rapid; urutan TryTake dijalankan secara berurutan.
func TestPropW20_TokenBucketMenjagaBatasKapasitasDanLaju(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		capacity := rapid.Float64Range(1, 500).Draw(t, "kapasitas")
		rate := rapid.Float64Range(0.1, 50).Draw(t, "laju_per_detik")
		start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		now := start
		b := Bucket{Capacity: capacity, RatePerSec: rate, Tokens: capacity, UpdatedAt: now}
		var taken float64

		for i := rapid.IntRange(1, 40).Draw(t, "jumlah_langkah"); i > 0; i-- {
			elapsed := rapid.Float64Range(0, 8).Draw(t, "selisih_waktu")
			cost := rapid.Float64Range(0, capacity*1.5).Draw(t, "biaya")
			now = now.Add(time.Duration(elapsed * float64(time.Second)))

			before := b
			next, wait := TryTake(b, cost, now)
			b = next

			// (1) Token selalu berada pada rentang [0, capacity].
			if b.Tokens < -1e-9 || b.Tokens > capacity+1e-9 {
				t.Fatalf("token = %v, want [0, %v]", b.Tokens, capacity)
			}
			if !b.UpdatedAt.Equal(now) {
				t.Fatalf("UpdatedAt = %v, want %v", b.UpdatedAt, now)
			}
			refill := refilledTokens(before, now)

			// (4) Biaya melebihi kapasitas tidak pernah berhasil.
			if cost > capacity {
				if !isInfiniteWait(wait) {
					t.Fatalf("cost %v > kapasitas %v: wait = %v, want +Inf", cost, capacity, wait)
				}
				if math.Abs(b.Tokens-refill) > 1e-9 {
					t.Fatalf("cost > kapasitas mengubah token: got %v, want %v", b.Tokens, refill)
				}
				continue
			}
			if isInfiniteWait(wait) {
				t.Fatalf("cost %v <= kapasitas %v: wait = +Inf", cost, capacity)
			}

			if wait == 0 {
				// Token diambil dan dicatat sebagai total cost berhasil.
				taken += cost
				if math.Abs(b.Tokens-(refill-cost)) > 1e-6 {
					t.Fatalf("setelah ambil: token = %v, want %v", b.Tokens, refill-cost)
				}
				continue
			}

			// (3) Saat wait > 0 token tidak berkurang, dan retry setelah wait
			// tanpa pengambilan lain harus berhasil.
			if b.Tokens < refill-1e-9 {
				t.Fatalf("penolakan mengurangi token: got %v, want >= %v", b.Tokens, refill)
			}
			retryAt := now.Add(wait)
			retry, retryWait := TryTake(b, cost, retryAt)
			if retryWait != 0 {
				t.Fatalf("retry setelah wait %v: wait = %v, want 0", wait, retryWait)
			}
			if want := refilledTokens(b, retryAt) - cost; math.Abs(retry.Tokens-want) > 1e-6 {
				t.Fatalf("retry: token = %v, want %v", retry.Tokens, want)
			}
		}

		// (2) Total cost yang berhasil diambil pada [0, T] dibatasi kapasitas + laju*T.
		T := now.Sub(start).Seconds()
		limit := capacity + rate*T
		if taken < 0 || taken > limit+1e-6*math.Max(1, limit) {
			t.Fatalf("total diambil = %v, want [0, %v] (T=%v)", taken, limit, T)
		}
	})
}

// stubStore merekam argumen middleware dan mengembalikan wait/error sesuai skenario.
type stubStore struct {
	wait time.Duration
	err  error
	key  string
	now  time.Time
	cost float64
}

func (s *stubStore) TryTake(_ context.Context, key string, _, _ float64, cost float64, now time.Time) (time.Duration, error) {
	s.key, s.now, s.cost = key, now, cost
	return s.wait, s.err
}

// TestNewMiddlewareRetryAfter memeriksa status 429, Retry-After, dan kunci bucket.
func TestNewMiddlewareRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := time.Date(2026, time.February, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name           string
		wait           time.Duration
		err            error
		wantStatus     int
		wantRetryAfter string
		wantCode       string
	}{
		{"diizinkan", 0, nil, http.StatusNoContent, "", ""},
		{"wait dibulatkan ke atas", 1200 * time.Millisecond, nil, http.StatusTooManyRequests, "2", "rate_limited"},
		{"wait kurang dari satu detik", 250 * time.Millisecond, nil, http.StatusTooManyRequests, "1", "rate_limited"},
		{"wait tak hingga", WaitInfinite, nil, http.StatusTooManyRequests, "1", "rate_limited"},
		{"store gagal", 0, errors.New("db mati"), http.StatusInternalServerError, "", "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubStore{wait: tt.wait, err: tt.err}
			engine := gin.New()
			engine.GET("/x", NewMiddleware(stub, "auth", 10, 1, clock.NewFake(base)), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.RemoteAddr = "203.0.113.7:54321"
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
			if got := w.Header().Get("Retry-After"); got != tt.wantRetryAfter {
				t.Errorf("Retry-After = %q, want %q", got, tt.wantRetryAfter)
			}
			if tt.wantCode != "" && !strings.Contains(w.Body.String(), tt.wantCode) {
				t.Errorf("body = %s, want mengandung %q", w.Body.String(), tt.wantCode)
			}
			if want := "http:auth:203.0.113.7"; stub.key != want {
				t.Errorf("kunci = %q, want %q", stub.key, want)
			}
			if !stub.now.Equal(base) {
				t.Errorf("now = %v, want %v", stub.now, base)
			}
			if stub.cost != 1 {
				t.Errorf("cost = %v, want 1", stub.cost)
			}
		})
	}
}

// TestMemoryStoreTryTake memastikan state antar pemanggilan disimpan per kunci.
func TestMemoryStoreTryTake(t *testing.T) {
	s := NewMemoryStore()
	base := time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC)
	const key = "http:general:203.0.113.7"

	for i := 0; i < 2; i++ {
		wait, err := s.TryTake(context.Background(), key, 2, 1, 1, base)
		if err != nil || wait != 0 {
			t.Fatalf("pengambilan %d: wait = %v, err = %v; want 0, nil", i+1, wait, err)
		}
	}
	wait, err := s.TryTake(context.Background(), key, 2, 1, 1, base)
	if err != nil || wait != time.Second {
		t.Fatalf("pengambilan ketiga: wait = %v, err = %v; want 1s, nil", wait, err)
	}
	wait, err = s.TryTake(context.Background(), key, 2, 1, 1, base.Add(2*time.Second))
	if err != nil || wait != 0 {
		t.Fatalf("setelah isi ulang: wait = %v, err = %v; want 0, nil", wait, err)
	}
	wait, err = s.TryTake(context.Background(), "http:general:198.51.100.9", 2, 1, 3, base)
	if err != nil || wait != WaitInfinite {
		t.Fatalf("cost > kapasitas: wait = %v, err = %v; want WaitInfinite, nil", wait, err)
	}
}
