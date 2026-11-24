package jobs

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AbandonExpiredSessions menandai sesi PAUSED yang kedaluwarsa sebagai ABANDONED
// dan menjadikan jawaban pending/failed sebagai unscored (Requirement 11.8).
func AbandonExpiredSessions(ctx context.Context, pool *pgxpool.Pool, now time.Time) (int, error) {
	tag, err := pool.Exec(ctx, `UPDATE interview_sessions SET status='ABANDONED', abandoned_at=$1
		WHERE status='PAUSED' AND expires_at <= $1 AND deleted_at IS NULL`, now)
	if err != nil {
		return 0, err
	}
	_, err = pool.Exec(ctx, `UPDATE interview_messages SET evaluation_status='unscored'
		WHERE interview_session_id IN (
			SELECT id FROM interview_sessions WHERE status='ABANDONED' AND abandoned_at=$1
		) AND evaluation_status IN ('pending','failed')`, now)
	return int(tag.RowsAffected()), err
}

// PurgeRetention mengisi deleted_at setelah retention_until terlewati.
func PurgeRetention(ctx context.Context, pool *pgxpool.Pool, now time.Time) (int, error) {
	tag, err := pool.Exec(ctx, `UPDATE interview_sessions SET deleted_at=$1
		WHERE retention_until IS NOT NULL AND retention_until <= $1 AND deleted_at IS NULL`, now)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
