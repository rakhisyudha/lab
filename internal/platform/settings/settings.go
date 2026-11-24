// Package settings mengelola app_settings: konfigurasi runtime yang dapat
// diubah Admin lewat API. Nilai disimpan sebagai JSON dan divalidasi terhadap
// value_schema sebelum ditulis ke database (Requirement 24.2).
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// ErrInvalidValue adalah sentinel untuk nilai atau skema yang tidak dapat
// divalidasi. Kegagalan validasi yang dikembalikan ke API dibungkus apperr.
var ErrInvalidValue = errors.New("settings: nilai tidak valid")

// defaultCacheTTL dipakai bila caller tidak menentukan TTL positif.
const defaultCacheTTL = 30 * time.Second

// Nama kunci yang terlibat aturan lintas kunci.
const (
	keyClampMin    = "scoring.clamp_min"
	keyClampMax    = "scoring.clamp_max"
	keyCriticalCap = "scoring.critical_score_cap"
	keyMinTopics   = "interview.min_topics_per_session"
	keyMaxTopics   = "interview.max_topics_per_session"
	quotaKeyPrefix = "ai.quota."
)

// Entry adalah satu baris app_settings.
type Entry struct {
	Key         string
	Value       json.RawMessage
	ValueSchema json.RawMessage
	Description string
	IsInitial   bool
	UpdatedBy   *uuid.UUID
	UpdatedAt   time.Time
}

// Store menyediakan akses ke app_settings.
type Store interface {
	// Load mengembalikan seluruh entri. Implementasi wajib mengembalikan salinan
	// yang tidak boleh ikut berubah saat caller memodifikasinya.
	Load(ctx context.Context) (map[string]Entry, error)

	// Update menulis value, mencatat actor, dan menandai baris sebagai bukan
	// nilai awal. Operasi harus atomik dan mengembalikan apperr not_found bila
	// key tidak ada.
	Update(ctx context.Context, actor uuid.UUID, key string, value json.RawMessage) error
}

// Snapshot adalah tampilan konfigurasi yang sudah dibatasi pada kunci tertentu.
// Isinya tidak dapat diubah oleh pemanggil.
type Snapshot struct {
	entries map[string]Entry
}

// Value mengembalikan salinan nilai mentah untuk key dan true bila key ada.
func (s Snapshot) Value(key string) (json.RawMessage, bool) {
	e, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	return cloneRaw(e.Value), true
}

// Int mengembalikan nilai integer untuk key, atau 0 bila tidak ada/tidak valid.
// Bilangan pecahan tidak dibulatkan.
func (s Snapshot) Int(key string) int64 {
	raw, ok := s.Value(key)
	if !ok {
		return 0
	}
	e, err := decodeJSON(raw)
	if err != nil {
		return 0
	}
	n, ok := e.(json.Number)
	if !ok {
		return 0
	}
	r, ok := ratFromNumber(n)
	if !ok || !r.IsInt() || !r.Num().IsInt64() {
		return 0
	}
	return r.Num().Int64()
}

// Float mengembalikan nilai number untuk key, atau 0 bila tidak ada/tidak valid.
func (s Snapshot) Float(key string) float64 {
	raw, ok := s.Value(key)
	if !ok {
		return 0
	}
	e, err := decodeJSON(raw)
	if err != nil {
		return 0
	}
	n, ok := e.(json.Number)
	if !ok {
		return 0
	}
	f, err := n.Float64()
	if err != nil {
		return 0
	}
	return f
}

// Bool mengembalikan nilai boolean untuk key, atau false bila tidak ada/tidak valid.
func (s Snapshot) Bool(key string) bool {
	raw, ok := s.Value(key)
	if !ok {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false
	}
	return b
}

// String mengembalikan string untuk key, atau "" bila tidak ada/tidak valid.
func (s Snapshot) String(key string) string {
	raw, ok := s.Value(key)
	if !ok {
		return ""
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return v
}

// AllInitial melaporkan apakah semua entri dalam snapshot masih berupa nilai
// awal. Snapshot kosong dianggap seluruhnya initial.
func (s Snapshot) AllInitial() bool {
	for _, e := range s.entries {
		if !e.IsInitial {
			return false
		}
	}
	return true
}

// Keys mengembalikan kunci dalam snapshot secara terurut.
func (s Snapshot) Keys() []string {
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Service menyimpan cache snapshot dan menjalankan validasi sebelum update.
type Service struct {
	store    Store
	clock    clock.Clock
	cacheTTL time.Duration

	mu         sync.Mutex
	cache      map[string]Entry
	cachedAt   time.Time
	cacheValid bool
}

// New membuat Service. TTL nonpositif diganti 30 detik; clock nil diganti clock sistem.
func New(store Store, c clock.Clock, cacheTTL time.Duration) *Service {
	if cacheTTL <= 0 {
		cacheTTL = defaultCacheTTL
	}
	if c == nil {
		c = clock.System{}
	}
	return &Service{store: store, clock: c, cacheTTL: cacheTTL}
}

// Snapshot mengembalikan salinan konfigurasi dari cache (TTL 30 detik secara
// default). Bila keys diberikan, hanya kunci itu yang disertakan; kunci yang
// tidak ada diabaikan, bukan error.
func (s *Service) Snapshot(ctx context.Context, keys ...string) (Snapshot, error) {
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.cacheValid || now.Sub(s.cachedAt) >= s.cacheTTL {
		entries, err := s.store.Load(ctx)
		if err != nil {
			return Snapshot{}, err
		}
		s.cache = cloneEntries(entries)
		s.cachedAt = now
		s.cacheValid = true
	}

	return Snapshot{entries: selectEntries(s.cache, keys)}, nil
}

// Update memvalidasi key, value terhadap skemanya, dan aturan lintas kunci
// sebelum meneruskan penulisan ke Store. Cache dihapus setelah update berhasil.
func (s *Service) Update(ctx context.Context, actor uuid.UUID, key string, value json.RawMessage) error {
	// Ambil snapshot segar: dipakai untuk keberadaan key, skema, dan aturan
	// lintas kunci yang mungkin dipengaruhi nilai baru.
	entries, err := s.store.Load(ctx)
	if err != nil {
		return err
	}
	entry, ok := entries[key]
	if !ok {
		return apperr.New(apperr.CodeNotFound)
	}
	if err := validateValue(key, entry.ValueSchema, value); err != nil {
		return err
	}

	proposed := cloneEntries(entries)
	updated := proposed[key]
	updated.Value = cloneRaw(value)
	proposed[key] = updated
	if err := validateCrossKey(Snapshot{entries: proposed}, key); err != nil {
		return err
	}

	if err := s.store.Update(ctx, actor, key, cloneRaw(value)); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// invalidate menghapus cache setelah penulisan berhasil. Dipanggil dari Update,
// bukan dari Store, agar kegagalan update tidak membuang cache yang masih valid.
func (s *Service) invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.cacheValid = false
	s.cachedAt = time.Time{}
	s.mu.Unlock()
}

// validateCrossKey menjalankan aturan yang melibatkan lebih dari satu kunci.
// snapshot sudah memuat nilai usulan untuk key yang sedang diubah.
func validateCrossKey(snapshot Snapshot, key string) error {
	fail := func(reason string) error {
		return apperr.Validation(apperr.FieldError{Field: key, Reason: reason})
	}

	clampMin, hasClampMin := numberFromSnapshot(snapshot, keyClampMin)
	clampMax, hasClampMax := numberFromSnapshot(snapshot, keyClampMax)
	if hasClampMin && hasClampMax && clampMin.Cmp(clampMax) >= 0 {
		return fail("scoring.clamp_min harus lebih kecil dari scoring.clamp_max")
	}

	criticalCap, hasCriticalCap := numberFromSnapshot(snapshot, keyCriticalCap)
	if hasCriticalCap && hasClampMin && hasClampMax &&
		(criticalCap.Cmp(clampMin) < 0 || criticalCap.Cmp(clampMax) > 0) {
		return fail("scoring.critical_score_cap harus berada di antara scoring.clamp_min dan scoring.clamp_max")
	}

	minTopics, hasMinTopics := numberFromSnapshot(snapshot, keyMinTopics)
	maxTopics, hasMaxTopics := numberFromSnapshot(snapshot, keyMaxTopics)
	if hasMinTopics && hasMaxTopics && minTopics.Cmp(maxTopics) > 0 {
		return fail("interview.min_topics_per_session tidak boleh melebihi interview.max_topics_per_session")
	}

	minQuota := big.NewRat(1, 1)
	for _, k := range snapshot.Keys() {
		if !strings.HasPrefix(k, quotaKeyPrefix) {
			continue
		}
		raw, ok := snapshot.Value(k)
		if !ok {
			continue
		}
		v, err := decodeJSON(raw)
		if err != nil {
			return fail("nilai " + k + " bukan JSON yang valid")
		}
		if v == nil { // null = tanpa batas (A-14)
			continue
		}
		n, ok := v.(json.Number)
		if !ok {
			return fail("nilai " + k + " harus null atau integer")
		}
		r, ok := ratFromNumber(n)
		if !ok || !r.IsInt() || r.Cmp(minQuota) < 0 {
			return fail("nilai " + k + " harus null atau integer minimal 1")
		}
	}

	return nil
}

// numberFromSnapshot mengambil angka dari entri snapshot.
func numberFromSnapshot(s Snapshot, key string) (*big.Rat, bool) {
	raw, ok := s.Value(key)
	if !ok {
		return nil, false
	}
	return numberFromJSON(raw)
}

// cloneRaw menyalin RawMessage agar pemanggil tidak dapat mengubah data asli.
func cloneRaw(b json.RawMessage) json.RawMessage {
	if b == nil {
		return nil
	}
	out := make(json.RawMessage, len(b))
	copy(out, b)
	return out
}

// cloneEntry menyalin Entry, termasuk slice JSON dan pointer UpdatedBy.
func cloneEntry(e Entry) Entry {
	e.Value = cloneRaw(e.Value)
	e.ValueSchema = cloneRaw(e.ValueSchema)
	if e.UpdatedBy != nil {
		id := *e.UpdatedBy
		e.UpdatedBy = &id
	}
	return e
}

// cloneEntries menyalin seluruh peta beserta isinya.
func cloneEntries(in map[string]Entry) map[string]Entry {
	out := make(map[string]Entry, len(in))
	for k, e := range in {
		out[k] = cloneEntry(e)
	}
	return out
}

// selectEntries mengambil salinan entri untuk keys. Peta kosong berarti semua entri.
func selectEntries(in map[string]Entry, keys []string) map[string]Entry {
	if len(keys) == 0 {
		return cloneEntries(in)
	}
	out := make(map[string]Entry, len(keys))
	for _, k := range keys {
		if e, ok := in[k]; ok {
			out[k] = cloneEntry(e)
		}
	}
	return out
}
