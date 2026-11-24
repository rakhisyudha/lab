package assessment

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/progress"
)

// Finalizer menyelesaikan Attempt assessment setelah jawaban berstatus final.
type Finalizer struct {
	store   *Store
	mastery *progress.MasteryService
}

func NewFinalizer(store *Store, mastery *progress.MasteryService) *Finalizer {
	return &Finalizer{store: store, mastery: mastery}
}

// TryFinalize menyelesaikan attempt bila berstatus submitted dan tidak ada
// jawaban pending atau failed (Requirement 20.10, 20.11, 20.12).
func (f *Finalizer) TryFinalize(ctx context.Context, attemptID uuid.UUID, now time.Time) error {
	return db.WithTx(ctx, f.store.pool, func(tx pgx.Tx) error {
		attempt, err := f.store.FindAttemptForUpdate(ctx, tx, attemptID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if attempt.Status != "submitted" {
			return nil
		}
		answers, err := f.store.ListAnswers(ctx, attemptID)
		if err != nil {
			return err
		}
		for _, a := range answers {
			if a.EvalStatus != nil && (*a.EvalStatus == "pending" || *a.EvalStatus == "failed") {
				return nil
			}
		}
		weights := map[string]evaluation.QuestionAggregate{}
		for _, a := range answers {
			var w float64
			var level string
			if err := tx.QueryRow(ctx, `SELECT weight, cognitive_level FROM questions WHERE id = $1`,
				a.QuestionID).Scan(&w, &level); err != nil {
				return err
			}
			weights[a.QuestionID.String()] = evaluation.QuestionAggregate{ID: a.QuestionID.String(), Weight: w, CognitiveLevel: level}
		}
		aggInputs := make([]evaluation.AnswerAggregate, 0, len(answers))
		for _, a := range answers {
			var score *float64
			if a.EvalStatus != nil && *a.EvalStatus == "scored" {
				var s sql.NullFloat64
				err := tx.QueryRow(ctx, `SELECT score_pct FROM answer_evaluations WHERE assessment_answer_id = $1`, a.ID).Scan(&s)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				if s.Valid {
					score = &s.Float64
				}
			}
			relevance := ""
			if a.EvalStatus != nil && *a.EvalStatus == "scored" {
				var r sql.NullString
				_ = tx.QueryRow(ctx, `SELECT relevance FROM answer_evaluations WHERE assessment_answer_id = $1`, a.ID).Scan(&r)
				relevance = r.String
			}
			status := ""
			if a.EvalStatus != nil {
				status = *a.EvalStatus
			}
			aggInputs = append(aggInputs, evaluation.AnswerAggregate{
				QuestionID: a.QuestionID.String(), EvalStatus: status,
				IsBlank: a.IsBlank, ScorePct: score, Relevance: relevance,
			})
		}
		agg := evaluation.AggregateAssessment(aggInputs, weights)
		if agg.Status == "deferred" {
			return nil
		}
		if agg.Status == "unscored" {
			return f.store.FinalizeAttempt(ctx, tx, attemptID, "unscored", nil, false, now)
		}
		signals, err := f.signals(ctx, tx, attemptID)
		if err != nil {
			return err
		}
		if err := f.store.FinalizeAttempt(ctx, tx, attemptID, "scored", agg.ScorePct, agg.HasApplicationQuestion, now); err != nil {
			return err
		}
		score := 0.0
		if agg.ScorePct != nil {
			score = *agg.ScorePct
		}
		return f.mastery.RecordAttempt(ctx, attempt.UserID, attempt.TopicID, attemptID, "assessment",
			score, agg.HasApplicationQuestion, now, signals)
	})
}

func (f *Finalizer) signals(ctx context.Context, tx pgx.Tx, attemptID uuid.UUID) ([]progress.EvaluationSignal, error) {
	rows, err := tx.Query(ctx, `SELECT ae.id, ae.created_at,
		COALESCE(array_agg(am.misconception_id::text) FILTER (WHERE am.kind='detected' AND am.misconception_id IS NOT NULL), '{}'),
		COALESCE(array_agg(am.misconception_id::text) FILTER (WHERE am.kind='resolved' AND am.misconception_id IS NOT NULL), '{}')
		FROM answer_evaluations ae
		JOIN assessment_answers aa ON aa.id = ae.assessment_answer_id
		LEFT JOIN answer_evaluation_misconceptions am ON am.answer_evaluation_id = ae.id
		WHERE aa.attempt_id = $1 GROUP BY ae.id, ae.created_at ORDER BY ae.created_at, ae.id`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]progress.EvaluationSignal, 0)
	for rows.Next() {
		var id string
		var created time.Time
		var detected, resolved []string
		if err := rows.Scan(&id, &created, &detected, &resolved); err != nil {
			return nil, err
		}
		// Flag menyimpan id Misconception; ubah ke kode katalog.
		detectedCodes, err := codesForIDs(ctx, tx, detected)
		if err != nil {
			return nil, err
		}
		resolvedCodes, err := codesForIDs(ctx, tx, resolved)
		if err != nil {
			return nil, err
		}
		out = append(out, progress.EvaluationSignal{
			ID: id, DetectedCodes: detectedCodes, ResolvedCodes: resolvedCodes, CreatedAt: created,
		})
	}
	return out, rows.Err()
}

func codesForIDs(ctx context.Context, tx pgx.Tx, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		var code string
		if err := tx.QueryRow(ctx, `SELECT code FROM misconceptions WHERE id = $1`, id).Scan(&code); err == nil {
			out = append(out, code)
		}
	}
	return out, nil
}
