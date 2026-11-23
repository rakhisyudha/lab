package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"deadlock 40P01", &pgconn.PgError{Code: "40P01"}, true},
		{"serialization 40001", &pgconn.PgError{Code: "40001"}, true},
		{"deadlock terbungkus", fmt.Errorf("lapis: %w", &pgconn.PgError{Code: "40P01"}), true},
		{"unique violation 23505", &pgconn.PgError{Code: "23505"}, false},
		{"check violation 23514", &pgconn.PgError{Code: "23514"}, false},
		{"error biasa", errors.New("koneksi putus"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Fatalf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestJitterStaysInRange(t *testing.T) {
	for i := 0; i < 2000; i++ {
		d := jitter()
		if d < 10*time.Millisecond || d >= 50*time.Millisecond {
			t.Fatalf("jitter() = %v, di luar [10ms, 50ms)", d)
		}
	}
}

// noSleep mencatat jeda tanpa menunggu, agar uji retry tetap cepat.
func noSleep(calls *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error {
		*calls = append(*calls, d)
		return nil
	}
}

func fixedPause() time.Duration { return 20 * time.Millisecond }

func TestRetryOnConflictSucceedsAfterTransientFailures(t *testing.T) {
	var sleeps []time.Duration
	attempts := 0
	err := retryOnConflict(context.Background(), MaxTxRetries, fixedPause, noSleep(&sleeps), func() error {
		attempts++
		if attempts < 3 {
			return &pgconn.PgError{Code: "40001"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryOnConflict() error = %v, want nil", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(sleeps) != 2 {
		t.Fatalf("sleeps = %d, want 2 (satu jeda per pengulangan)", len(sleeps))
	}
}

func TestRetryOnConflictGivesUpAfterMaxRetries(t *testing.T) {
	var sleeps []time.Duration
	attempts := 0
	err := retryOnConflict(context.Background(), MaxTxRetries, fixedPause, noSleep(&sleeps), func() error {
		attempts++
		return &pgconn.PgError{Code: "40P01"}
	})
	// 1 percobaan awal + MaxTxRetries pengulangan.
	if attempts != MaxTxRetries+1 {
		t.Fatalf("attempts = %d, want %d", attempts, MaxTxRetries+1)
	}
	if len(sleeps) != MaxTxRetries {
		t.Fatalf("sleeps = %d, want %d", len(sleeps), MaxTxRetries)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40P01" {
		t.Fatalf("error = %v, want terbungkus PgError 40P01", err)
	}
}

func TestRetryOnConflictDoesNotRetryOtherErrors(t *testing.T) {
	var sleeps []time.Duration
	attempts := 0
	want := &pgconn.PgError{Code: "23505"}
	err := retryOnConflict(context.Background(), MaxTxRetries, fixedPause, noSleep(&sleeps), func() error {
		attempts++
		return want
	})
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if len(sleeps) != 0 {
		t.Fatalf("sleeps = %d, want 0", len(sleeps))
	}
}

func TestRetryOnConflictStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	err := retryOnConflict(ctx, MaxTxRetries, fixedPause, sleepCtx, func() error {
		attempts++
		return &pgconn.PgError{Code: "40001"}
	})
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (konteks sudah dibatalkan sebelum jeda)", attempts)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// Uji integrasi ini berjalan hanya bila TEST_DATABASE_URL diisi ke database PostgreSQL
// kosong. Tanpa variabel tersebut, uji dilewati.
func TestPoolAndWithTxAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL tidak diisi, uji integrasi dilewati")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Close()

	t.Run("time zone sesi adalah UTC", func(t *testing.T) {
		var tz string
		if err := pool.QueryRow(ctx, "SHOW TIME ZONE").Scan(&tz); err != nil {
			t.Fatalf("SHOW TIME ZONE: %v", err)
		}
		if tz != "UTC" {
			t.Fatalf("time zone = %q, want UTC", tz)
		}
	})

	t.Run("WithTx commit dan rollback", func(t *testing.T) {
		err := WithTx(ctx, pool, func(tx pgx.Tx) error {
			var one int
			return tx.QueryRow(ctx, "SELECT 1").Scan(&one)
		})
		if err != nil {
			t.Fatalf("WithTx() commit error = %v", err)
		}

		sentinel := errors.New("batalkan")
		err = WithTx(ctx, pool, func(tx pgx.Tx) error {
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("WithTx() error = %v, want sentinel", err)
		}
	})
}
