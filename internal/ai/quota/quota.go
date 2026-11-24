// Package quota menerapkan Kuota_Manager dan batas antrean per user.
// Kuota dibaca dari App_Settings sehingga perubahan Admin berlaku tanpa deploy
// (Requirement 16.2, 16.3, 16.11, 24.2).
package quota

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

// Code error kuota.
const (
	CodeQuotaExceeded = "ai_quota_exceeded"
	CodeQueueLimit    = "ai_queue_limit_exceeded"
)

type SettingsReader interface {
	Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
}

// Decision adalah hasil pemeriksaan kuota sebelum AITask diterima.
type Decision struct {
	Allowed bool
	Code    string
	ResetAt time.Time
}

// Manager memeriksa kuota pemakaian dan jumlah task aktif per user.
type Manager struct {
	pool     *pgxpool.Pool
	settings SettingsReader
	clock    clock.Clock
}

func NewManager(pool *pgxpool.Pool, s SettingsReader, c clock.Clock) *Manager {
	if c == nil {
		c = clock.System{}
	}
	return &Manager{pool: pool, settings: s, clock: c}
}

var quotaKeys = []string{
	"ai.quota.user.daily.requests", "ai.quota.user.daily.total_tokens",
	"ai.quota.user.monthly.requests", "ai.quota.user.monthly.total_tokens",
	"ai.quota.global.daily.requests", "ai.quota.global.daily.total_tokens",
	"ai.quota.global.monthly.requests", "ai.quota.global.monthly.total_tokens",
	"ai.per_user_queue_limit",
}

// Check mengevaluasi seluruh kuota yang berlaku. Bila userID nil (task sistem),
// hanya kuota global yang diperiksa.
func (m *Manager) Check(ctx context.Context, userID *uuid.UUID) (Decision, error) {
	snap, err := m.settings.Snapshot(ctx, quotaKeys...)
	if err != nil {
		return Decision{}, err
	}
	now := m.clock.Now().UTC()
	dailyStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	monthlyStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	if userID != nil {
		limit := snap.Int("ai.per_user_queue_limit")
		if limit <= 0 {
			limit = 20
		}
		var active int
		if err := m.pool.QueryRow(ctx, `SELECT count(*) FROM ai_tasks
			WHERE user_id = $1 AND status IN ('queued','processing')`, *userID).Scan(&active); err != nil {
			return Decision{}, err
		}
		if int64(active) >= limit {
			return Decision{Code: CodeQueueLimit}, nil
		}
	}

	type check struct {
		key     string
		scope   string
		period  string
		metric  string
		resetAt time.Time
	}
	checks := []check{
		{"ai.quota.global.daily.requests", "global", "daily", "requests", dailyStart.AddDate(0, 0, 1)},
		{"ai.quota.global.daily.total_tokens", "global", "daily", "total_tokens", dailyStart.AddDate(0, 0, 1)},
		{"ai.quota.global.monthly.requests", "global", "monthly", "requests", monthlyStart.AddDate(0, 1, 0)},
		{"ai.quota.global.monthly.total_tokens", "global", "monthly", "total_tokens", monthlyStart.AddDate(0, 1, 0)},
	}
	if userID != nil {
		checks = append(checks,
			check{"ai.quota.user.daily.requests", "user", "daily", "requests", dailyStart.AddDate(0, 0, 1)},
			check{"ai.quota.user.daily.total_tokens", "user", "daily", "total_tokens", dailyStart.AddDate(0, 0, 1)},
			check{"ai.quota.user.monthly.requests", "user", "monthly", "requests", monthlyStart.AddDate(0, 1, 0)},
			check{"ai.quota.user.monthly.total_tokens", "user", "monthly", "total_tokens", monthlyStart.AddDate(0, 1, 0)},
		)
	}
	for _, c := range checks {
		raw, ok := snap.Value(c.key)
		if !ok {
			continue
		}
		limit := intFromValue(raw)
		if limit <= 0 { // null = tanpa batas
			continue
		}
		since := dailyStart
		if c.period == "monthly" {
			since = monthlyStart
		}
		used, err := m.usage(ctx, userID, c.scope, since)
		if err != nil {
			return Decision{}, err
		}
		if used >= int64(limit) {
			return Decision{Code: CodeQuotaExceeded, ResetAt: c.resetAt}, nil
		}
	}
	return Decision{Allowed: true}, nil
}

func (m *Manager) usage(ctx context.Context, userID *uuid.UUID, scope string, since time.Time) (int64, error) {
	q := `SELECT COALESCE(SUM(input_tokens + output_tokens), 0) FROM ai_usage_logs WHERE created_at >= $1`
	args := []any{since}
	if scope == "user" && userID != nil {
		q = `SELECT COALESCE(SUM(input_tokens + output_tokens), 0) FROM ai_usage_logs WHERE created_at >= $1 AND user_id = $2`
		args = append(args, *userID)
	}
	var n int64
	err := m.pool.QueryRow(ctx, q, args...).Scan(&n)
	return n, err
}

func intFromValue(raw []byte) int {
	s := string(raw)
	if s == "null" || s == "" {
		return 0
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
