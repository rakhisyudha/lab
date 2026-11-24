// Package evalresult menangani hasil AITask answer_evaluation dan menulis
// answer_evaluations secara idempoten (Requirement 14.6, 25.8).
package evalresult

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/ai/evaluator"
	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	"github.com/rakhisyudha/lab/internal/assessment"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

type Service struct {
	pool      *pgxpool.Pool
	evaluator *evaluator.Service
	finalizer *assessment.Finalizer
	settings  interface {
		Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
	}
	clock clock.Clock
}

func NewService(pool *pgxpool.Pool, ev *evaluator.Service, f *assessment.Finalizer,
	s interface {
		Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
	}, c clock.Clock) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{pool: pool, evaluator: ev, finalizer: f, settings: s, clock: c}
}

// HandleSuccess memproses hasil Evaluator yang sukses.
func (s *Service) HandleSuccess(ctx context.Context, task taskqueue.Task, result evaluator.Result) error {
	var p evaluator.Payload
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		return err
	}
	if p.AssessmentAnswerID == nil {
		return nil // jawaban interview ditangani Phase 4.
	}
	answerID := *p.AssessmentAnswerID
	now := s.clock.Now()
	var attemptID, userID, topicID uuid.UUID
	var evalStatus string
	err := db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT aa.attempt_id, aa.evaluation_status, a.user_id, a.topic_id
			FROM assessment_answers aa JOIN assessment_attempts a ON a.id = aa.attempt_id
			WHERE aa.id = $1 FOR UPDATE`, answerID).Scan(&attemptID, &evalStatus, &userID, &topicID); err != nil {
			return err
		}
		// Hasil terlambat dibuang tanpa menulis answer_evaluations.
		if evalStatus != "pending" {
			return nil
		}
		evalID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		paramsJSON, _ := json.Marshal(map[string]any{
			"score_pct": result.Score.ScorePct, "needs_review": result.Score.NeedsReview,
		})
		raw, _ := json.Marshal(result.RawOutput)
		_, err = tx.Exec(ctx, `INSERT INTO answer_evaluations
			(id, user_id, topic_id, question_id, locale, assessment_answer_id, ai_task_id,
			 prompt_contract_id, model_name, kind, relevance, rubric_score, score_pct, score_hint,
			 concept_results, penalties, feedback, confidence, needs_review, params_snapshot, raw_output)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'ai',$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
			ON CONFLICT (assessment_answer_id) WHERE assessment_answer_id IS NOT NULL DO NOTHING`,
			evalID, userID, topicID, p.QuestionID, p.Locale, answerID, task.ID,
			task.PromptContractID, "replay", result.Gateway.Output.Relevance, result.Score.RubricScore,
			result.Score.ScorePct, result.Gateway.Output.ScoreHint, mustJSON(result.Gateway.Output.ConceptResults),
			mustJSON(result.Score.Penalties), result.Gateway.Output.Feedback, result.Gateway.Output.Confidence,
			result.Score.NeedsReview, paramsJSON, raw)
		if err != nil {
			return err
		}
		for _, code := range result.Gateway.Output.MisconceptionCodes {
			var mid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM misconceptions WHERE code = $1 AND topic_id = $2`,
				code, topicID).Scan(&mid); err != nil {
				continue
			}
			_, err := tx.Exec(ctx, `INSERT INTO answer_evaluation_misconceptions
				(answer_evaluation_id, misconception_id, kind) VALUES ($1,$2,'detected')
				ON CONFLICT DO NOTHING`, evalID, mid)
			if err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE assessment_answers SET evaluation_status='scored' WHERE id=$1`, answerID)
		return err
	})
	if err != nil {
		return err
	}
	if evalStatus == "pending" {
		return s.finalizer.TryFinalize(ctx, attemptID, now)
	}
	return nil
}

// HandleFailure menandai jawaban failed atau unscored bila retry habis.
func (s *Service) HandleFailure(ctx context.Context, task taskqueue.Task, errorCode string) error {
	var p evaluator.Payload
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		return err
	}
	if p.AssessmentAnswerID == nil {
		return nil
	}
	answerID := *p.AssessmentAnswerID
	now := s.clock.Now()
	snap, err := s.settings.Snapshot(ctx, "ai.max_evaluation_retries", "ai.evaluation_retry_window_hours")
	if err != nil {
		return err
	}
	params := evaluation.EvalParams{
		MaxEvaluationRetries:       int(snap.Int("ai.max_evaluation_retries")),
		EvaluationRetryWindowHours: int(snap.Int("ai.evaluation_retry_window_hours")),
	}
	if params.MaxEvaluationRetries < 0 {
		params.MaxEvaluationRetries = 1
	}
	var attemptID uuid.UUID
	var changed bool
	err = db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		var retryCount int
		var failedAt time.Time
		if err := tx.QueryRow(ctx, `SELECT attempt_id, evaluation_status, evaluation_retry_count,
			COALESCE(evaluation_failed_at, now()) FROM assessment_answers WHERE id=$1 FOR UPDATE`,
			answerID).Scan(&attemptID, &status, &retryCount, &failedAt); err != nil {
			return err
		}
		if status != "pending" {
			return nil
		}
		res := evaluation.ApplyEvaluationEvent(evaluation.AnswerEvalState{
			Status: status, RetryCount: retryCount, FailedAt: failedAt,
		}, evaluation.EvalEvent{Type: "eval_failed", Now: now}, params)
		var fp *time.Time
		if !res.State.FailedAt.IsZero() {
			fp = &res.State.FailedAt
		}
		if _, err := tx.Exec(ctx, `UPDATE assessment_answers SET evaluation_status=$2,
			evaluation_failed_at=$3 WHERE id=$1`, answerID, res.State.Status, fp); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil || !changed {
		return err
	}
	return s.finalizer.TryFinalize(ctx, attemptID, now)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

var _ = errors.New
