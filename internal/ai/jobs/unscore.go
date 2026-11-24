// Package jobs berisi job terjadwal worker, termasuk unscore jawaban failed
// setelah jendela retry (Requirement 25.9, 20.12).
package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

type SettingsReader interface {
	Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
}

// FinalizeAssessment dipanggil setelah jawaban assessment menjadi unscored.
type FinalizeAssessment func(ctx context.Context, attemptID uuid.UUID, now time.Time) error

// UnscoreExpiredFailed mengubah jawaban failed melewati jendela retry menjadi
// unscored. UPDATE ... RETURNING membuat job dan Evaluation_Retry saling
// menimpa dengan aman (salah satu menang).
func UnscoreExpiredFailed(ctx context.Context, pool *pgxpool.Pool, s SettingsReader,
	now time.Time, finalize FinalizeAssessment) (int, error) {
	snap, err := s.Snapshot(ctx, "ai.evaluation_retry_window_hours")
	if err != nil {
		return 0, err
	}
	hours := int(snap.Int("ai.evaluation_retry_window_hours"))
	if hours <= 0 {
		hours = 24
	}
	cutoff := now.Add(-time.Duration(hours) * time.Hour)

	var attempts []uuid.UUID
	err = db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE assessment_answers SET evaluation_status='unscored'
			WHERE evaluation_status='failed' AND evaluation_failed_at <= $1 RETURNING attempt_id`, cutoff)
		if err != nil {
			return err
		}
		seen := map[uuid.UUID]bool{}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if !seen[id] {
				seen[id] = true
				attempts = append(attempts, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE interview_messages SET evaluation_status='unscored'
			WHERE evaluation_status='failed' AND evaluation_failed_at <= $1`, cutoff)
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, attemptID := range attempts {
		if finalize != nil {
			if err := finalize(ctx, attemptID, now); err != nil {
				return len(attempts), err
			}
		}
	}
	return len(attempts), nil
}
