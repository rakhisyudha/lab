package interview

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rakhisyudha/lab/internal/ai/evaluator"
	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/db"
)

// EvalResultHandler menangani hasil answer_evaluation untuk pesan interview.
type EvalResultHandler struct {
	store        *Store
	orchestrator *Orchestrator
	clock        clock.Clock
}

func NewEvalResultHandler(store *Store, orch *Orchestrator, c clock.Clock) *EvalResultHandler {
	if c == nil {
		c = clock.System{}
	}
	return &EvalResultHandler{store: store, orchestrator: orch, clock: c}
}

// Handle menerapkan hasil Evaluator ke sesi interview.
func (h *EvalResultHandler) Handle(ctx context.Context, task taskqueue.Task, result evaluator.Result) error {
	var payload struct {
		SessionID          string `json:"session_id"`
		InterviewMessageID string `json:"interview_message_id"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.InterviewMessageID == "" {
		return nil
	}
	msgID, err := uuid.Parse(payload.InterviewMessageID)
	if err != nil {
		return nil
	}
	sessionID, err := uuid.Parse(payload.SessionID)
	if err != nil {
		return nil
	}

	concepts := make([]evaluation.ConceptSignal, 0, len(result.Gateway.Output.ConceptResults))
	for _, c := range result.Gateway.Output.ConceptResults {
		concepts = append(concepts, evaluation.ConceptSignal{
			Code: c.ConceptCode, Coverage: c.Coverage,
		})
	}
	detected := make([]evaluation.MisconceptionSignal, 0, len(result.Gateway.Output.MisconceptionCodes))
	for _, code := range result.Gateway.Output.MisconceptionCodes {
		severity := result.Severity[code]
		if severity == "" {
			severity = "minor"
		}
		detected = append(detected, evaluation.MisconceptionSignal{Code: code, Severity: severity})
	}
	var topicID uuid.UUID
	var contractID *uuid.UUID
	err = db.WithTx(ctx, h.store.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT topic_id FROM questions q
			JOIN interview_messages im ON im.question_id = q.id WHERE im.id=$1`, msgID).Scan(&topicID); err != nil {
			return err
		}
		if task.PromptContractID != nil {
			contractID = task.PromptContractID
		}
		return nil
	})
	if err != nil {
		return err
	}
	return h.orchestrator.OnAnswerEvaluated(ctx, sessionID, msgID, EvalResult{
		Raw: result.RawOutput, TopicID: topicID, ConceptResults: concepts, Detected: detected,
		Relevance: result.Gateway.Output.Relevance, ScorePct: result.Score.ScorePct,
		PromptContractID: contractID,
	})
}

// HandleFailure menandai jawaban interview sebagai failed/unscored.
func (h *EvalResultHandler) HandleFailure(ctx context.Context, task taskqueue.Task, errorCode string) error {
	var payload struct {
		InterviewMessageID string `json:"interview_message_id"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.InterviewMessageID == "" {
		return nil
	}
	msgID, err := uuid.Parse(payload.InterviewMessageID)
	if err != nil {
		return nil
	}
	now := h.clock.Now()
	return db.WithTx(ctx, h.store.pool, func(tx pgx.Tx) error {
		var status string
		var retry int
		var failedAt time.Time
		if err := tx.QueryRow(ctx, `SELECT evaluation_status, evaluation_retry_count,
			COALESCE(evaluation_failed_at, now()) FROM interview_messages WHERE id=$1 FOR UPDATE`,
			msgID).Scan(&status, &retry, &failedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		if status != "pending" {
			return nil
		}
		res := evaluation.ApplyEvaluationEvent(evaluation.AnswerEvalState{
			Status: status, RetryCount: retry, FailedAt: failedAt,
		}, evaluation.EvalEvent{Type: "eval_failed", Now: now}, evaluation.EvalParams{
			MaxEvaluationRetries: 1, EvaluationRetryWindowHours: 24,
		})
		var fp *time.Time
		if !res.State.FailedAt.IsZero() {
			fp = &res.State.FailedAt
		}
		return h.store.UpdateMessageStatus(ctx, tx, msgID, res.State.Status, res.State.RetryCount, fp)
	})
}
