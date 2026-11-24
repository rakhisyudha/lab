package settings

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

var (
	_ Store = (*postgresStore)(nil)
	_ Store = (*memoryStore)(nil)
)

func newEntry(value, schema string) Entry {
	return Entry{
		Value:       json.RawMessage(value),
		ValueSchema: json.RawMessage(schema),
		IsInitial:   true,
	}
}

func testEntries() map[string]Entry {
	return map[string]Entry{
		"auth.access_token_ttl_seconds":      newEntry(`900`, `{"type":"integer","minimum":60,"maximum":86400}`),
		"ai.quota.user.daily.requests":       newEntry(`200`, `{"type":["integer","null"],"minimum":1}`),
		"ai.quota.user.daily.total_tokens":   newEntry(`null`, `{"type":["integer","null"],"minimum":1}`),
		"mastery.application_evidence_scope": newEntry(`"window"`, `{"enum":["window","all"]}`),
		"golden.live_schedule_cron":          newEntry(`"0 2 * * 1"`, `{"type":"string","maxLength":64}`),
		"golden.live_schedule_enabled":       newEntry(`false`, `{"type":"boolean"}`),
		"mastery.mastered_threshold":         newEntry(`80`, `{"type":"number","minimum":0,"maximum":100}`),
		"scoring.clamp_min":                  newEntry(`0`, `{"type":"number","minimum":0,"maximum":50}`),
		"scoring.clamp_max":                  newEntry(`100`, `{"type":"number","minimum":50,"maximum":100}`),
		"scoring.critical_score_cap":         newEntry(`60`, `{"type":"number","minimum":0,"maximum":100}`),
		"interview.min_topics_per_session":   newEntry(`1`, `{"type":"integer","minimum":1}`),
		"interview.max_topics_per_session":   newEntry(`5`, `{"type":"integer","minimum":1}`),
	}
}

// spyStore menghitung pemanggilan Load/Update sekaligus memastikan service tidak
// menulis saat validasi gagal.
type spyStore struct {
	mu        sync.Mutex
	inner     Store
	loads     int
	updates   int
	lastValue json.RawMessage
}

func (s *spyStore) Load(ctx context.Context) (map[string]Entry, error) {
	s.mu.Lock()
	s.loads++
	s.mu.Unlock()
	return s.inner.Load(ctx)
}

func (s *spyStore) Update(ctx context.Context, actor uuid.UUID, key string, value json.RawMessage) error {
	s.mu.Lock()
	s.updates++
	s.lastValue = cloneRaw(value)
	s.mu.Unlock()
	return s.inner.Update(ctx, actor, key, value)
}

func (s *spyStore) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads, s.updates
}

func assertAppErr(t *testing.T, err error, code apperr.Code, status int) {
	t.Helper()
	ae, ok := apperr.As(err)
	if !ok {
		t.Fatalf("error = %v, ingin apperr %s", err, code)
	}
	if ae.Code != code || ae.HTTPStatus != status {
		t.Fatalf("apperr = (%s, %d), ingin (%s, %d)", ae.Code, ae.HTTPStatus, code, status)
	}
}

func newTestService(t *testing.T) (*Service, *spyStore, *clock.Fake) {
	t.Helper()
	spy := &spyStore{inner: NewMemoryStore(testEntries())}
	fake := clock.NewFake(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	return New(spy, fake, 0), spy, fake
}

func TestSnapshotAccessors(t *testing.T) {
	svc, _, _ := newTestService(t)
	snap, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if got := snap.Int("auth.access_token_ttl_seconds"); got != 900 {
		t.Errorf("Int(ttl) = %d, ingin 900", got)
	}
	if got := snap.Int("auth.access_token_ttl_seconds/ salah"); got != 0 {
		t.Errorf("Int(kunci tidak ada) = %d, ingin 0", got)
	}
	if got := snap.Float("mastery.mastered_threshold"); got != 80 {
		t.Errorf("Float(threshold) = %v, ingin 80", got)
	}
	if got := snap.Bool("golden.live_schedule_enabled"); got {
		t.Errorf("Bool(enabled) = true, ingin false")
	}
	if got := snap.String("golden.live_schedule_cron"); got != "0 2 * * 1" {
		t.Errorf("String(cron) = %q, ingin %q", got, "0 2 * * 1")
	}
	if got := snap.String("mastery.application_evidence_scope"); got != "window" {
		t.Errorf("String(scope) = %q, ingin %q", got, "window")
	}
	if _, ok := snap.Value("kunci.tidak.ada"); ok {
		t.Error("Value(kunci tidak ada) menemukan entri")
	}
	if !snap.AllInitial() {
		t.Error("AllInitial() = false pada snapshot awal")
	}

	keys := snap.Keys()
	if len(keys) != len(testEntries()) {
		t.Fatalf("jumlah kunci = %d, ingin %d", len(keys), len(testEntries()))
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Fatalf("Keys() tidak terurut: %v", keys)
		}
	}
}

func TestSnapshotFilterKeys(t *testing.T) {
	svc, _, _ := newTestService(t)
	snap, err := svc.Snapshot(context.Background(), "golden.live_schedule_cron", "kunci.tidak.ada")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got := snap.Keys(); len(got) != 1 || got[0] != "golden.live_schedule_cron" {
		t.Fatalf("Keys() = %v, ingin hanya golden.live_schedule_cron", got)
	}
	if !snap.AllInitial() {
		t.Error("AllInitial() = false pada snapshot berisi satu entri initial")
	}

	empty, err := svc.Snapshot(context.Background(), "kunci.tidak.ada")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(empty.Keys()) != 0 {
		t.Errorf("snapshot kunci yang tidak ada = %v, ingin kosong", empty.Keys())
	}
	if !empty.AllInitial() {
		t.Error("AllInitial() = false pada snapshot kosong")
	}
}

func TestSnapshotReturnsCopies(t *testing.T) {
	svc, _, _ := newTestService(t)
	snap, err := svc.Snapshot(context.Background(), "auth.access_token_ttl_seconds")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	raw, ok := snap.Value("auth.access_token_ttl_seconds")
	if !ok {
		t.Fatal("Value() tidak menemukan kunci")
	}
	raw[0] = '9' // mengubah salinan tidak boleh memengaruhi snapshot/cache
	again, err := svc.Snapshot(context.Background(), "auth.access_token_ttl_seconds")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got := again.Int("auth.access_token_ttl_seconds"); got != 900 {
		t.Errorf("nilai setelah mutasi salinan = %d, ingin 900", got)
	}
}

func TestCacheTTLWithFakeClock(t *testing.T) {
	spy := &spyStore{inner: NewMemoryStore(testEntries())}
	fake := clock.NewFake(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	svc := New(spy, fake, 30*time.Second)

	for i := 0; i < 3; i++ {
		if _, err := svc.Snapshot(context.Background()); err != nil {
			t.Fatalf("Snapshot() error = %v", err)
		}
	}
	if loads, _ := spy.counts(); loads != 1 {
		t.Fatalf("jumlah Load dalam TTL = %d, ingin 1", loads)
	}

	fake.Advance(29 * time.Second)
	if _, err := svc.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if loads, _ := spy.counts(); loads != 1 {
		t.Fatalf("jumlah Load pada detik ke-29 = %d, ingin 1", loads)
	}

	fake.Advance(time.Second)
	// Ubah nilai langsung di store agar terlihat setelah cache kedaluwarsa.
	newValue := json.RawMessage(`1200`)
	if err := spy.inner.Update(context.Background(), uuid.New(), "auth.access_token_ttl_seconds", newValue); err != nil {
		t.Fatalf("Update store error = %v", err)
	}
	snap, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if loads, _ := spy.counts(); loads != 2 {
		t.Fatalf("jumlah Load setelah TTL = %d, ingin 2", loads)
	}
	if got := snap.Int("auth.access_token_ttl_seconds"); got != 1200 {
		t.Errorf("nilai setelah TTL = %d, ingin 1200", got)
	}
}

func TestNewUsesDefaultTTL(t *testing.T) {
	svc := New(NewMemoryStore(testEntries()), clock.System{}, 0)
	if svc.cacheTTL != defaultCacheTTL {
		t.Errorf("cacheTTL = %v, ingin %v", svc.cacheTTL, defaultCacheTTL)
	}
	negative := New(NewMemoryStore(testEntries()), clock.System{}, -time.Second)
	if negative.cacheTTL != defaultCacheTTL {
		t.Errorf("cacheTTL negatif = %v, ingin %v", negative.cacheTTL, defaultCacheTTL)
	}
}

func TestUpdateSchemaValidation(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{"integer di dalam rentang", "auth.access_token_ttl_seconds", `1200`, false},
		{"integer desimal ditolak", "auth.access_token_ttl_seconds", `900.5`, true},
		{"integer berupa string ditolak", "auth.access_token_ttl_seconds", `"900"`, true},
		{"integer di bawah minimum", "auth.access_token_ttl_seconds", `59`, true},
		{"integer di atas maksimum", "auth.access_token_ttl_seconds", `86401`, true},
		{"null ditolak untuk tipe integer", "auth.access_token_ttl_seconds", `null`, true},
		{"number menerima bilangan bulat", "mastery.mastered_threshold", `90`, false},
		{"number menerima pecahan", "mastery.mastered_threshold", `80.5`, false},
		{"number di atas maksimum", "mastery.mastered_threshold", `101`, true},
		{"enum yang diizinkan", "mastery.application_evidence_scope", `"all"`, false},
		{"enum yang tidak diizinkan", "mastery.application_evidence_scope", `"none"`, true},
		{"enum dengan tipe salah", "mastery.application_evidence_scope", `1`, true},
		{"string di dalam batas panjang", "golden.live_schedule_cron", `"0 3 * * *"`, false},
		{"string melebihi maxLength", "golden.live_schedule_cron", `"` + strings.Repeat("x", 65) + `"`, true},
		{"boolean yang valid", "golden.live_schedule_enabled", `true`, false},
		{"boolean berupa string ditolak", "golden.live_schedule_enabled", `"true"`, true},
		{"JSON rusak ditolak", "auth.access_token_ttl_seconds", `{`, true},
		{"kuota null diterima", "ai.quota.user.daily.requests", `null`, false},
		{"kuota integer 1 diterima", "ai.quota.user.daily.requests", `1`, false},
		{"kuota nol ditolak", "ai.quota.user.daily.requests", `0`, true},
		{"kuota pecahan ditolak", "ai.quota.user.daily.requests", `1.5`, true},
		{"kuota string ditolak", "ai.quota.user.daily.requests", `"5"`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, spy, _ := newTestService(t)
			err := svc.Update(context.Background(), uuid.New(), tt.key, json.RawMessage(tt.value))
			if tt.wantErr {
				assertAppErr(t, err, apperr.CodeValidationError, 400)
			} else if err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			_, updates := spy.counts()
			if tt.wantErr && updates != 0 {
				t.Errorf("Store.Update dipanggil %d kali pada validasi gagal", updates)
			}
			if !tt.wantErr && updates != 1 {
				t.Errorf("Store.Update dipanggil %d kali, ingin 1", updates)
			}
		})
	}
}

func TestUpdateMissingKey(t *testing.T) {
	svc, spy, _ := newTestService(t)
	err := svc.Update(context.Background(), uuid.New(), "tidak.ada", json.RawMessage(`1`))
	assertAppErr(t, err, apperr.CodeNotFound, 404)
	if _, updates := spy.counts(); updates != 0 {
		t.Errorf("Store.Update dipanggil %d kali untuk kunci yang tidak ada", updates)
	}
}

func TestUpdateCrossKeyRules(t *testing.T) {
	const scoreSchema = `{"type":"number","minimum":0,"maximum":100}`

	tests := []struct {
		name           string
		overrides      map[string]string
		schemaOverride map[string]string
		key            string
		value          string
		wantErr        bool
		wantReason     string
	}{
		{
			name:      "clamp_min harus lebih kecil dari clamp_max",
			overrides: map[string]string{"scoring.clamp_max": `40`},
			schemaOverride: map[string]string{
				"scoring.clamp_min": `{"type":"number","minimum":0,"maximum":100}`,
				"scoring.clamp_max": scoreSchema,
			},
			key:        "scoring.clamp_min",
			value:      `40`,
			wantErr:    true,
			wantReason: "clamp_min harus lebih kecil",
		},
		{
			name:       "critical cap tidak boleh melebihi clamp_max",
			key:        "scoring.clamp_max",
			value:      `50`,
			wantErr:    true,
			wantReason: "critical_score_cap",
		},
		{
			name:       "critical cap tidak boleh di bawah clamp_min",
			overrides:  map[string]string{"scoring.clamp_min": `10`},
			key:        "scoring.critical_score_cap",
			value:      `5`,
			wantErr:    true,
			wantReason: "critical_score_cap",
		},
		{
			name:       "min topics tidak boleh melebihi max topics",
			key:        "interview.min_topics_per_session",
			value:      `6`,
			wantErr:    true,
			wantReason: "min_topics_per_session",
		},
		{
			name:       "max topics tidak boleh lebih kecil dari min topics",
			overrides:  map[string]string{"interview.min_topics_per_session": `5`},
			key:        "interview.max_topics_per_session",
			value:      `2`,
			wantErr:    true,
			wantReason: "max_topics_per_session",
		},
		{
			name:    "clamp_min yang masih valid diterima",
			key:     "scoring.clamp_min",
			value:   `45`,
			wantErr: false,
		},
		{
			name:    "min topics yang masih valid diterima",
			key:     "interview.min_topics_per_session",
			value:   `5`,
			wantErr: false,
		},
		{
			name:      "kuota null pada kunci lain tetap diterima",
			overrides: map[string]string{"ai.quota.global.daily.requests": `null`},
			key:       "auth.access_token_ttl_seconds",
			value:     `1000`,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := testEntries()
			for k, v := range tt.overrides {
				e := entries[k]
				if e.Key == "" && e.Value == nil {
					e = newEntry(v, `{"type":["integer","null"],"minimum":1}`)
				}
				e.Value = json.RawMessage(v)
				entries[k] = e
			}
			for k, sc := range tt.schemaOverride {
				e := entries[k]
				e.ValueSchema = json.RawMessage(sc)
				entries[k] = e
			}
			spy := &spyStore{inner: NewMemoryStore(entries)}
			svc := New(spy, clock.System{}, time.Minute)
			err := svc.Update(context.Background(), uuid.New(), tt.key, json.RawMessage(tt.value))
			if tt.wantErr {
				assertAppErr(t, err, apperr.CodeValidationError, 400)
				ae, _ := apperr.As(err)
				details, ok := ae.Details.([]apperr.FieldError)
				if !ok || len(details) == 0 {
					t.Fatalf("details = %#v, ingin []apperr.FieldError", ae.Details)
				}
				if details[0].Field != tt.key {
					t.Errorf("field = %q, ingin %q", details[0].Field, tt.key)
				}
				if !strings.Contains(details[0].Reason, tt.wantReason) {
					t.Errorf("reason = %q, ingin mengandung %q", details[0].Reason, tt.wantReason)
				}
			} else if err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			_, updates := spy.counts()
			want := 0
			if !tt.wantErr {
				want = 1
			}
			if updates != want {
				t.Errorf("Store.Update dipanggil %d kali, ingin %d", updates, want)
			}
		})
	}
}

func TestUpdateFlipsIsInitialAndInvalidatesCache(t *testing.T) {
	svc, spy, _ := newTestService(t)
	ctx := context.Background()

	before, err := svc.Snapshot(ctx, "auth.access_token_ttl_seconds")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if !before.AllInitial() {
		t.Fatal("snapshot awal seharusnya seluruhnya initial")
	}

	actor := uuid.New()
	if err := svc.Update(ctx, actor, "auth.access_token_ttl_seconds", json.RawMessage(`1200`)); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	entries, err := spy.inner.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	e := entries["auth.access_token_ttl_seconds"]
	if e.IsInitial {
		t.Error("IsInitial masih true setelah update")
	}
	if e.UpdatedBy == nil || *e.UpdatedBy != actor {
		t.Errorf("UpdatedBy = %v, ingin %v", e.UpdatedBy, actor)
	}
	if e.UpdatedAt.IsZero() {
		t.Error("UpdatedAt kosong setelah update")
	}
	if other := entries["mastery.mastered_threshold"]; !other.IsInitial {
		t.Error("entri lain ikut berubah status initial-nya")
	}

	after, err := svc.Snapshot(ctx, "auth.access_token_ttl_seconds")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got := after.Int("auth.access_token_ttl_seconds"); got != 1200 {
		t.Errorf("nilai setelah update = %d, ingin 1200", got)
	}
	if after.AllInitial() {
		t.Error("AllInitial() = true setelah update")
	}
	if loads, _ := spy.counts(); loads != 3 {
		t.Errorf("jumlah Load = %d, ingin 3 (awal, update, setelah invalidasi)", loads)
	}
}

func TestMemoryStoreIsolation(t *testing.T) {
	ctx := context.Background()
	input := testEntries()
	store := NewMemoryStore(input)

	// Mutasi map masukan tidak boleh memengaruhi store.
	input["auth.access_token_ttl_seconds"] = newEntry(`1`, `{}`)
	delete(input, "mastery.mastered_threshold")

	entries, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := string(entries["auth.access_token_ttl_seconds"].Value); got != `900` {
		t.Errorf("nilai terdampak mutasi map asli: %s", got)
	}
	if _, ok := entries["mastery.mastered_threshold"]; !ok {
		t.Error("entri hilang karena mutasi map asli")
	}

	// Mutasi hasil Load tidak boleh memengaruhi store.
	entries["auth.access_token_ttl_seconds"].Value[0] = '8'
	delete(entries, "mastery.mastered_threshold")
	again, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := string(again["auth.access_token_ttl_seconds"].Value); got != `900` {
		t.Errorf("nilai terdampak mutasi hasil Load: %s", got)
	}
	if _, ok := again["mastery.mastered_threshold"]; !ok {
		t.Error("entri hilang karena mutasi hasil Load")
	}

	// Mutasi nilai yang dikirim ke Update tidak boleh memengaruhi store.
	value := json.RawMessage(`1200`)
	actor := uuid.New()
	if err := store.Update(ctx, actor, "auth.access_token_ttl_seconds", value); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	value[0] = '9'
	final, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := string(final["auth.access_token_ttl_seconds"].Value); got != `1200` {
		t.Errorf("nilai terdampak mutasi argumen Update: %s", got)
	}
}

func TestMemoryStoreUpdateMissingKey(t *testing.T) {
	err := NewMemoryStore(testEntries()).Update(context.Background(), uuid.New(), "tidak.ada", json.RawMessage(`1`))
	assertAppErr(t, err, apperr.CodeNotFound, 404)
}

func TestServiceConcurrentSnapshots(t *testing.T) {
	svc, _, _ := newTestService(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap, err := svc.Snapshot(context.Background(), "auth.access_token_ttl_seconds")
			if err != nil {
				t.Errorf("Snapshot() error = %v", err)
				return
			}
			if got := snap.Int("auth.access_token_ttl_seconds"); got != 900 {
				t.Errorf("Int() = %d, ingin 900", got)
			}
		}()
	}
	wg.Wait()
}
