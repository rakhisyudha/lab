package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// postgresStore membaca dan menulis app_settings pada PostgreSQL.
type postgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore membuat Store yang memakai pgxpool. Nilai tidak pernah
// ditulis ke log: error hanya menyebut konteks operasi, bukan isi nilai.
func NewPostgresStore(pool *pgxpool.Pool) Store {
	return &postgresStore{pool: pool}
}

// Load mengembalikan seluruh baris app_settings. Baris kosong menghasilkan peta
// kosong, bukan error.
func (s *postgresStore) Load(ctx context.Context) (map[string]Entry, error) {
	const query = `SELECT key, value, value_schema, description, is_initial_value, updated_by, updated_at ` +
		`FROM app_settings ORDER BY key`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("settings: memuat pengaturan: %w", err)
	}
	defer rows.Close()

	entries := make(map[string]Entry)
	for rows.Next() {
		var (
			e         Entry
			value     []byte
			schema    []byte
			updatedBy uuid.NullUUID
		)
		if err := rows.Scan(&e.Key, &value, &schema, &e.Description, &e.IsInitial, &updatedBy, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("settings: memindai pengaturan: %w", err)
		}
		e.Value = cloneRaw(value)
		e.ValueSchema = cloneRaw(schema)
		if updatedBy.Valid {
			actor := updatedBy.UUID
			e.UpdatedBy = &actor
		}
		entries[e.Key] = e
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: membaca pengaturan: %w", err)
	}
	return entries, nil
}

// Update menjalankan satu pernyataan SQL atomik: nilai baru, actor, dan status
// bukan nilai awal ditulis sekaligus. Trigger database mengurus updated_at.
func (s *postgresStore) Update(ctx context.Context, actor uuid.UUID, key string, value json.RawMessage) error {
	const query = `UPDATE app_settings SET value = $1, updated_by = $2, is_initial_value = false WHERE key = $3`

	tag, err := s.pool.Exec(ctx, query, value, actor, key)
	if err != nil {
		return fmt.Errorf("settings: menyimpan pengaturan %q: %w", key, err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.New(apperr.CodeNotFound)
	}
	return nil
}

// memoryStore dipakai pada mode single_instance/dev dan dalam pengujian.
type memoryStore struct {
	mu      sync.Mutex
	entries map[string]Entry
}

// NewMemoryStore membuat Store in-memory. Map masukan disalin mendalam, sehingga
// perubahan pada map asli tidak memengaruhi isi store.
func NewMemoryStore(entries map[string]Entry) Store {
	return &memoryStore{entries: cloneEntries(entries)}
}

// Load mengembalikan salinan seluruh entri.
func (s *memoryStore) Load(ctx context.Context) (map[string]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneEntries(s.entries), nil
}

// Update mengubah satu entri di bawah mutex. UpdatedAt memakai waktu sistem UTC
// karena konstruktor memory store tidak menerima clock (lihat konvensi clock:
// seluruh kode bisnis memakai interface, tetapi store hanya mencatat audit).
func (s *memoryStore) Update(ctx context.Context, actor uuid.UUID, key string, value json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]
	if !ok {
		return apperr.New(apperr.CodeNotFound)
	}
	e.Value = cloneRaw(value)
	e.IsInitial = false
	e.UpdatedBy = &actor
	e.UpdatedAt = time.Now().UTC()
	s.entries[key] = e
	return nil
}
