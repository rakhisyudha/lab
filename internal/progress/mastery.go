package progress

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

// EvaluationSignal adalah hasil Evaluator yang siap dipakai RecordAttempt.
type EvaluationSignal struct {
	ID            string
	DetectedCodes []string
	ResolvedCodes []string
	CreatedAt     time.Time
}

// MasteryService menulis topic_attempts, user_misconceptions, dan mastery_records
// dalam satu transaksi (Requirement 12.6).
type MasteryService struct {
	pool     *pgxpool.Pool
	settings interface {
		Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
	}
}

func NewMasteryService(pool *pgxpool.Pool, s interface {
	Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
}) *MasteryService {
	return &MasteryService{pool: pool, settings: s}
}

var masteryKeys = []string{
	"mastery.mastered_threshold", "mastery.min_attempts", "mastery.window_size",
	"mastery.needs_review_floor", "mastery.application_evidence_scope",
	"mastery.resolve_consecutive_required",
}

func (s *MasteryService) params(ctx context.Context) (evaluation.MasteryParams, int, error) {
	snap, err := s.settings.Snapshot(ctx, masteryKeys...)
	if err != nil {
		return evaluation.MasteryParams{}, 1, err
	}
	p := evaluation.MasteryParams{
		MasteredThreshold:        snap.Float("mastery.mastered_threshold"),
		MinAttempts:              int(snap.Int("mastery.min_attempts")),
		WindowSize:               int(snap.Int("mastery.window_size")),
		NeedsReviewFloor:         snap.Float("mastery.needs_review_floor"),
		ApplicationEvidenceScope: snap.String("mastery.application_evidence_scope"),
		AllInitial:               snap.AllInitial(),
	}
	if p.MasteredThreshold <= 0 {
		p.MasteredThreshold = 80
	}
	if p.MinAttempts <= 0 {
		p.MinAttempts = 3
	}
	if p.WindowSize <= 0 {
		p.WindowSize = 3
	}
	if p.NeedsReviewFloor <= 0 {
		p.NeedsReviewFloor = 50
	}
	if p.ApplicationEvidenceScope == "" {
		p.ApplicationEvidenceScope = "window"
	}
	required := int(snap.Int("mastery.resolve_consecutive_required"))
	if required <= 0 {
		required = 1
	}
	return p, required, nil
}

// RecordAttempt menyimpan Attempt dan menghitung ulang Mastery. Idempoten
// lewat unique index pada sumber Attempt.
func (s *MasteryService) RecordAttempt(ctx context.Context, userID, topicID uuid.UUID,
	attemptID uuid.UUID, sourceType string, score float64, hasApp bool, scoredAt time.Time,
	signals []EvaluationSignal) error {
	p, required, err := s.params(ctx)
	if err != nil {
		return err
	}
	return db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO topic_attempts
			(user_id, topic_id, source_type, assessment_attempt_id, score_pct, has_application_question, scored_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (assessment_attempt_id) DO NOTHING`,
			userID, topicID, sourceType, attemptID, score, hasApp, scoredAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if err := s.applySignals(ctx, tx, userID, signals, required); err != nil {
			return err
		}
		return s.recompute(ctx, tx, userID, topicID, p, &attemptID, scoredAt)
	})
}

func (s *MasteryService) applySignals(ctx context.Context, tx pgx.Tx, userID uuid.UUID,
	signals []EvaluationSignal, required int) error {
	state := map[string]evaluation.MisconceptionRecord{}
	rows, err := tx.Query(ctx, `SELECT m.code, um.status, um.occurrences, um.consecutive_resolve_count,
		um.detected_at, um.resolved_at FROM user_misconceptions um
		JOIN misconceptions m ON m.id = um.misconception_id WHERE um.user_id = $1`, userID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r evaluation.MisconceptionRecord
		var resolved sql.NullTime
		if err := rows.Scan(&r.Code, &r.Status, &r.Occurrences, &r.ConsecutiveResolveCount,
			&r.DetectedAt, &resolved); err != nil {
			rows.Close()
			return err
		}
		if resolved.Valid {
			t := resolved.Time
			r.ResolvedAt = &t
		}
		state[r.Code] = r
	}
	rows.Close()
	events := make([]evaluation.EvalSignalEvent, 0, len(signals))
	for _, sig := range signals {
		events = append(events, evaluation.EvalSignalEvent{
			ID: sig.ID, DetectedCodes: sig.DetectedCodes, ResolvedCodes: sig.ResolvedCodes,
			CreatedAt: sig.CreatedAt,
		})
	}
	next := evaluation.ApplyMisconceptionSignals(state, events, required)
	for code, rec := range next {
		old, exists := state[code]
		if exists && old.Status == rec.Status && old.Occurrences == rec.Occurrences &&
			old.ConsecutiveResolveCount == rec.ConsecutiveResolveCount {
			continue
		}
		var misconceptionID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM misconceptions WHERE code = $1 ORDER BY id LIMIT 1`, code).Scan(&misconceptionID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return err
		}
		if !exists {
			if _, err := tx.Exec(ctx, `INSERT INTO user_misconceptions
				(user_id, misconception_id, status, occurrences, consecutive_resolve_count, detected_at)
				VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (user_id, misconception_id) DO NOTHING`,
				userID, misconceptionID, rec.Status, rec.Occurrences, rec.ConsecutiveResolveCount, rec.DetectedAt); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE user_misconceptions SET status=$3, occurrences=$4,
				consecutive_resolve_count=$5, resolved_at=$6 WHERE user_id=$1 AND misconception_id=$2`,
				userID, misconceptionID, rec.Status, rec.Occurrences, rec.ConsecutiveResolveCount, rec.ResolvedAt); err != nil {
				return err
			}
		}
	}
	return nil
}

// RecomputeMastery menghitung ulang Mastery dari data yang tersimpan.
func (s *MasteryService) RecomputeMastery(ctx context.Context, userID, topicID uuid.UUID) error {
	p, _, err := s.params(ctx)
	if err != nil {
		return err
	}
	return db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.recompute(ctx, tx, userID, topicID, p, nil, time.Time{})
	})
}

func (s *MasteryService) recompute(ctx context.Context, tx pgx.Tx, userID, topicID uuid.UUID,
	p evaluation.MasteryParams, lastAttemptID *uuid.UUID, scoredAt time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO mastery_records (user_id, topic_id) VALUES ($1,$2)
		ON CONFLICT (user_id, topic_id) DO NOTHING`, userID, topicID); err != nil {
		return err
	}
	var ever bool
	var existingMastered sql.NullTime
	if err := tx.QueryRow(ctx, `SELECT COALESCE(evidence->>'ever_mastered','false')::boolean, mastered_at
		FROM mastery_records WHERE user_id=$1 AND topic_id=$2 FOR UPDATE`, userID, topicID).Scan(&ever, &existingMastered); err != nil {
		return err
	}

	rows, err := tx.Query(ctx, `SELECT id, score_pct, has_application_question, scored_at FROM topic_attempts
		WHERE user_id=$1 AND topic_id=$2 ORDER BY scored_at, id`, userID, topicID)
	if err != nil {
		return err
	}
	attempts := make([]evaluation.Attempt, 0)
	for rows.Next() {
		var a evaluation.Attempt
		var id uuid.UUID
		if err := rows.Scan(&id, &a.ScorePct, &a.HasApplicationQuestion, &a.ScoredAt); err != nil {
			rows.Close()
			return err
		}
		a.ID = id.String()
		attempts = append(attempts, a)
	}
	rows.Close()

	rows, err = tx.Query(ctx, `SELECT m.code, m.severity, um.status FROM user_misconceptions um
		JOIN misconceptions m ON m.id = um.misconception_id
		WHERE um.user_id=$1 AND m.topic_id=$2`, userID, topicID)
	if err != nil {
		return err
	}
	misconceptions := make([]evaluation.MisconceptionFlag, 0)
	for rows.Next() {
		var m evaluation.MisconceptionFlag
		if err := rows.Scan(&m.Code, &m.Severity, &m.Status); err != nil {
			rows.Close()
			return err
		}
		misconceptions = append(misconceptions, m)
	}
	rows.Close()

	r := evaluation.ComputeMastery(attempts, misconceptions, ever, p)
	var masteredAt any
	if r.Status == "MASTERED" {
		masteredAt = nowOr(existingMastered, scoredAt)
	}
	var avg, best any
	if r.AvgLast3 != nil {
		avg = *r.AvgLast3
	}
	if r.BestScore != nil {
		best = *r.BestScore
	}
	_, err = tx.Exec(ctx, `UPDATE mastery_records SET status=$3, attempt_count=$4, avg_last3=$5,
		best_score=$6, last_attempt_id=$7, mastered_at=$8, evidence=$9, rule_version='mastery_v1'
		WHERE user_id=$1 AND topic_id=$2`,
		userID, topicID, r.Status, len(attempts), avg, best, lastAttemptID, masteredAt, r.Evidence)
	return err
}

func nowOr(existing sql.NullTime, fallback time.Time) any {
	if existing.Valid {
		return existing.Time
	}
	if fallback.IsZero() {
		return time.Now().UTC()
	}
	return fallback
}

// ResolveManual menyelesaikan catatan Misconception secara manual oleh Admin.
func (s *MasteryService) ResolveManual(ctx context.Context, id uuid.UUID, adminID uuid.UUID, reason string) (uuid.UUID, uuid.UUID, error) {
	if reason == "" {
		return uuid.Nil, uuid.Nil, errors.New("alasan wajib diisi")
	}
	var userID, topicID uuid.UUID
	err := db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `UPDATE user_misconceptions SET status='resolved', resolved_at=now(),
			resolved_by=$2, resolution_reason=$3 WHERE id=$1 RETURNING user_id,
			(SELECT topic_id FROM misconceptions WHERE id = misconception_id)`,
			id, adminID, reason).Scan(&userID, &topicID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := s.RecomputeMastery(ctx, userID, topicID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return userID, topicID, nil
}
