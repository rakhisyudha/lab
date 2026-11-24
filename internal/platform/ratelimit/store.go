package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/platform/db"
)

// Store menyimpan state bucket per kunci. Pemanggilan TryTake harus atomic untuk
// satu kunci agar dua request konkuren tidak mengambil token yang sama.
type Store interface {
	TryTake(ctx context.Context, key string, capacity, ratePerSec, cost float64, now time.Time) (wait time.Duration, err error)
}

// memoryStore dipakai pada mode single_instance/dev dan dalam pengujian.
// State hilang saat proses restart (lihat konvensi RATELIMIT_STORE pada design).
type memoryStore struct {
	mu      sync.Mutex
	buckets map[string]Bucket
}

// NewMemoryStore membuat Store in-memory dengan mutex untuk seluruh kunci.
func NewMemoryStore() Store {
	return &memoryStore{buckets: make(map[string]Bucket)}
}

// TryTake membuat bucket penuh pada pemakaian pertama, lalu menjalankan TryTake
// murni di bawah mutex. Konfigurasi kapasitas/laju mengikuti argumen terbaru.
func (s *memoryStore) TryTake(
	ctx context.Context,
	key string,
	capacity, ratePerSec, cost float64,
	now time.Time,
) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[key]
	if !ok {
		b = Bucket{Capacity: capacity, RatePerSec: ratePerSec, Tokens: capacity, UpdatedAt: now}
	} else {
		b.Capacity = capacity
		b.RatePerSec = ratePerSec
	}
	next, wait := TryTake(b, cost, now)
	s.buckets[key] = next
	return wait, nil
}

// postgresStore memakai tabel rate_buckets. Baris dikunci FOR UPDATE selama
// satu transaksi sehingga perhitungan TryTake aman untuk akses konkuren antar instance.
type postgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore membuat Store yang memakai pgxpool dan tabel rate_buckets.
func NewPostgresStore(pool *pgxpool.Pool) Store {
	return &postgresStore{pool: pool}
}

// TryTake mengunci baris bucket, menjalankan TryTake di dalam satu transaksi,
// lalu menyimpan token dan waktu terbaru. Baris dibuat saat pertama kali dipakai.
func (s *postgresStore) TryTake(
	ctx context.Context,
	key string,
	capacity, ratePerSec, cost float64,
	now time.Time,
) (wait time.Duration, err error) {
	const selectQuery = `SELECT tokens, updated_at FROM rate_buckets WHERE key = $1 FOR UPDATE`
	const insertQuery = `INSERT INTO rate_buckets (key, tokens, updated_at) VALUES ($1, $2, $3) ON CONFLICT (key) DO NOTHING`
	const updateQuery = `UPDATE rate_buckets SET tokens = $1, updated_at = $2 WHERE key = $3`

	err = db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		b := Bucket{Capacity: capacity, RatePerSec: ratePerSec}
		readErr := tx.QueryRow(ctx, selectQuery, key).Scan(&b.Tokens, &b.UpdatedAt)
		if errors.Is(readErr, pgx.ErrNoRows) {
			if _, insErr := tx.Exec(ctx, insertQuery, key, capacity, now); insErr != nil {
				return fmt.Errorf("ratelimit: membuat bucket %q: %w", key, insErr)
			}
			readErr = tx.QueryRow(ctx, selectQuery, key).Scan(&b.Tokens, &b.UpdatedAt)
		}
		if readErr != nil {
			return fmt.Errorf("ratelimit: membaca bucket %q: %w", key, readErr)
		}

		next, w := TryTake(b, cost, now)
		if _, updErr := tx.Exec(ctx, updateQuery, next.Tokens, next.UpdatedAt, key); updErr != nil {
			return fmt.Errorf("ratelimit: menyimpan bucket %q: %w", key, updErr)
		}
		wait = w
		return nil
	})
	if err != nil {
		return 0, err
	}
	return wait, nil
}

// PurgeIdleBuckets menghapus bucket yang sudah tidak aktif lebih dari idleFor
// (default 1 jam bila <= 0). Tabel rate_buckets tidak menyimpan kapasitas, jadi
// bucket dengan sisa token (> 0) yang sudah lama tidak disentuh dianggap telah
// penuh kembali setelah masa idle; bucket dengan token habis sengaja dipertahankan.
func PurgeIdleBuckets(ctx context.Context, pool *pgxpool.Pool, now time.Time, idleFor time.Duration) error {
	if idleFor <= 0 {
		idleFor = time.Hour
	}
	const query = `DELETE FROM rate_buckets WHERE updated_at < $1 AND tokens > 0`
	if _, err := pool.Exec(ctx, query, now.Add(-idleFor)); err != nil {
		return fmt.Errorf("ratelimit: membersihkan bucket idle: %w", err)
	}
	return nil
}
