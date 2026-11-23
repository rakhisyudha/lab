// Package db menyediakan koneksi pgxpool dan helper transaksi.
package db

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxTxRetries adalah jumlah pengulangan transaksi setelah percobaan pertama
// gagal dengan deadlock (40P01) atau serialization failure (40001).
const MaxTxRetries = 3

// Rentang jitter jeda antar pengulangan: [10 ms, 50 ms).
const (
	jitterMin  = 10 * time.Millisecond
	jitterSpan = 40 * time.Millisecond
)

// SQLSTATE yang memicu pengulangan transaksi.
const (
	sqlStateDeadlockDetected = "40P01"
	sqlStateSerialization    = "40001"
)

// TxBeginner memulai transaksi. *pgxpool.Pool memenuhi interface ini.
type TxBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// NewPool membuat pgxpool dari URL database. Setiap koneksi baru menjalankan
// SET TIME ZONE 'UTC' agar seluruh kolom timestamptz diproses dalam UTC.
// Pool dibuat tanpa menguji koneksi. Pemanggil dapat memanggil Ping bila perlu.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// Pesan error pgx dapat memuat URL dengan kredensial, jadi tidak diteruskan.
		return nil, errors.New("db: URL database tidak dapat di-parse")
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SET TIME ZONE 'UTC'"); err != nil {
			return fmt.Errorf("db: set time zone UTC: %w", err)
		}
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: membuat pool: %w", err)
	}
	return pool, nil
}

// WithTx menjalankan fn di dalam satu transaksi. Commit dilakukan bila fn
// mengembalikan nil, selain itu transaksi di-rollback. Bila transaksi gagal
// dengan 40P01 atau 40001, transaksi diulang dari awal hingga MaxTxRetries kali
// dengan jeda jitter 10 sampai 50 ms.
func WithTx(ctx context.Context, b TxBeginner, fn func(tx pgx.Tx) error) error {
	return retryOnConflict(ctx, MaxTxRetries, jitter, sleepCtx, func() error {
		return runTx(ctx, b, fn)
	})
}

func runTx(ctx context.Context, b TxBeginner, fn func(tx pgx.Tx) error) error {
	tx, err := b.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("db: memulai transaksi: %w", err)
	}
	// Rollback setelah commit berhasil mengembalikan pgx.ErrTxClosed, sehingga diabaikan.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit transaksi: %w", err)
	}
	return nil
}

// IsRetryable melaporkan apakah err berasal dari deadlock (40P01) atau
// serialization failure (40001), dengan memeriksa seluruh rantai error.
func IsRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == sqlStateDeadlockDetected || pgErr.Code == sqlStateSerialization
}

// retryOnConflict menjalankan op. Bila op gagal dengan error yang dapat diulang,
// op dijalankan lagi setelah jeda pause() hingga maxRetries kali pengulangan.
// Error yang tidak dapat diulang langsung dikembalikan tanpa pengulangan.
// sleep dan pause diparameterkan agar logika ini dapat diuji tanpa menunggu.
func retryOnConflict(
	ctx context.Context,
	maxRetries int,
	pause func() time.Duration,
	sleep func(context.Context, time.Duration) error,
	op func() error,
) error {
	for retries := 0; ; retries++ {
		err := op()
		if err == nil || !IsRetryable(err) {
			return err
		}
		if retries >= maxRetries {
			return fmt.Errorf("db: transaksi gagal setelah %d pengulangan: %w", maxRetries, err)
		}
		if serr := sleep(ctx, pause()); serr != nil {
			return serr
		}
	}
}

// jitter mengembalikan jeda acak dalam rentang [10 ms, 50 ms).
func jitter() time.Duration {
	return jitterMin + rand.N(jitterSpan)
}

// sleepCtx menunggu d atau berhenti lebih awal bila ctx selesai.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
