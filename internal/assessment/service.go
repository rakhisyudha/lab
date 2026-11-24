package assessment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rakhisyudha/lab/internal/ai/quota"
	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

type SettingsReader interface {
	Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
}

type Service struct {
	store    *Store
	queue    taskqueue.Queue
	quotas   *quota.Manager
	settings SettingsReader
	clock    clock.Clock
}

func NewService(store *Store, queue taskqueue.Queue, q *quota.Manager, s SettingsReader, c clock.Clock) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{store: store, queue: queue, quotas: q, settings: s, clock: c}
}

func (s *Service) Create(ctx context.Context, userID, topicID uuid.UUID, locale string) (uuid.UUID, error) {
	questions, err := s.store.PickQuestions(ctx, topicID, locale, 10)
	if err != nil {
		return uuid.Nil, err
	}
	if len(questions) == 0 {
		return uuid.Nil, apperr.New(apperr.CodeTranslationNotAvailable)
	}
	hasApp := false
	for _, q := range questions {
		if q.CognitiveLevel == "application" || q.CognitiveLevel == "scenario" {
			hasApp = true
		}
	}
	if !hasApp {
		return uuid.Nil, apperr.New(apperr.CodeInsufficientQuestions)
	}
	return s.store.CreateAttempt(ctx, userID, topicID, locale, questions)
}

func (s *Service) SaveAnswer(ctx context.Context, attemptID, questionID uuid.UUID, text string) error {
	attempt, err := s.store.GetAttempt(ctx, attemptID)
	if errors.Is(err, ErrNotFound) {
		return apperr.New(apperr.CodeNotFound)
	}
	if err != nil {
		return err
	}
	if attempt.Status != "in_progress" {
		return apperr.New(apperr.CodeAttemptNotActive)
	}
	if err := s.store.SaveAnswer(ctx, attemptID, questionID, text, s.clock.Now()); err != nil {
		return apperr.New(apperr.CodeNotFound)
	}
	return nil
}

// Submit menerapkan aturan submit per jawaban dalam satu transaksi dan
// men-enqueue satu AITask per jawaban non-kosong (Requirement 20.7).
func (s *Service) Submit(ctx context.Context, attemptID, userID uuid.UUID, idemKey string) error {
	now := s.clock.Now()
	params, err := s.evalParams(ctx)
	if err != nil {
		return err
	}
	var tasks []taskqueue.NewTask
	err = db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
		attempt, err := s.store.FindAttemptForUpdate(ctx, tx, attemptID)
		if errors.Is(err, ErrNotFound) {
			return apperr.New(apperr.CodeNotFound)
		}
		if err != nil {
			return err
		}
		if attempt.UserID != userID || attempt.Status != "in_progress" {
			return apperr.New(apperr.CodeAttemptNotActive)
		}
		decision, err := s.quotas.Check(ctx, &userID)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return quotaError(decision.Code, decision.ResetAt)
		}
		answers, err := s.store.ListAnswers(ctx, attemptID)
		if err != nil {
			return err
		}
		if err := s.store.SetSubmitted(ctx, tx, attemptID, now); err != nil {
			return err
		}
		for _, a := range answers {
			if a.EvalStatus != nil {
				continue
			}
			res := evaluation.ApplyEvaluationEvent(evaluation.AnswerEvalState{
				Status: "", RetryCount: a.RetryCount, FailedAt: zeroTime(a.FailedAt), IsBlank: a.IsBlank,
			}, evaluation.EvalEvent{Type: "submit", Now: now}, params)
			if res.Outcome != "ok" {
				continue
			}
			var failedAt *time.Time
			if !res.State.FailedAt.IsZero() {
				failedAt = &res.State.FailedAt
			}
			if err := s.store.UpdateAnswerStatus(ctx, tx, a.ID, res.State.Status,
				res.State.RetryCount, failedAt, nil); err != nil {
				return err
			}
			if res.Effect == "enqueue_evaluation" {
				payload, _ := json.Marshal(map[string]any{
					"question_id": a.QuestionID.String(), "user_id": userID.String(),
					"topic_id": attempt.TopicID.String(), "answer_text": deref(a.AnswerText),
					"locale": attempt.Locale, "assessment_answer_id": a.ID.String(),
				})
				tasks = append(tasks, taskqueue.NewTask{
					UserID: &userID, TaskType: "answer_evaluation", Priority: 2,
					Payload: payload, IdempotencyKey: idemKey + ":" + a.ID.String(),
					ResourceType: "assessment_answer", ResourceID: &a.ID,
				})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if _, err := s.queue.Enqueue(ctx, t, now); err != nil {
			return err
		}
	}
	return nil
}

// RetryEvaluation menerapkan Evaluation_Retry pada satu jawaban assessment.
func (s *Service) RetryEvaluation(ctx context.Context, attemptID, questionID, userID uuid.UUID) (int, error) {
	now := s.clock.Now()
	params, err := s.evalParams(ctx)
	if err != nil {
		return 0, err
	}
	status := 200
	err = db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
		a, err := s.store.FindAnswerForUpdate(ctx, tx, attemptID, questionID)
		if errors.Is(err, ErrNotFound) {
			return apperr.New(apperr.CodeNotFound)
		}
		if err != nil {
			return err
		}
		attempt, err := s.store.FindAttemptForUpdate(ctx, tx, attemptID)
		if err != nil {
			return err
		}
		if attempt.UserID != userID {
			return apperr.New(apperr.CodeNotFound)
		}
		res := evaluation.ApplyEvaluationEvent(evaluation.AnswerEvalState{
			Status: deref(a.EvalStatus), RetryCount: a.RetryCount,
			FailedAt: zeroTime(a.FailedAt), IsBlank: a.IsBlank,
		}, evaluation.EvalEvent{Type: "retry_requested", Now: now}, params)
		if res.Outcome == "error" {
			return apperr.WithDetails(apperr.CodeEvaluationNotRetryable, map[string]any{"reason": res.Reason})
		}
		if res.Outcome == "noop_200" {
			return nil
		}
		decision, err := s.quotas.Check(ctx, &userID)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return quotaError(decision.Code, decision.ResetAt)
		}
		payload, _ := json.Marshal(map[string]any{
			"question_id": a.QuestionID.String(), "user_id": userID.String(),
			"topic_id": attempt.TopicID.String(), "answer_text": deref(a.AnswerText),
			"locale": attempt.Locale, "assessment_answer_id": a.ID.String(),
		})
		t, err := s.queue.Enqueue(ctx, taskqueue.NewTask{
			UserID: &userID, TaskType: "answer_evaluation", Priority: 2, Payload: payload,
			IdempotencyKey: fmt.Sprintf("eval-retry:%s:%d", a.ID, res.State.RetryCount),
			ResourceType:   "assessment_answer", ResourceID: &a.ID,
			PromptContractID: a.PromptContractID,
		}, now)
		if err != nil {
			return err
		}
		err = s.store.UpdateAnswerStatus(ctx, tx, a.ID, res.State.Status, res.State.RetryCount, nil, a.PromptContractID)
		if err != nil {
			return err
		}
		_ = t
		status = 202
		return nil
	})
	return status, err
}

func (s *Service) evalParams(ctx context.Context) (evaluation.EvalParams, error) {
	snap, err := s.settings.Snapshot(ctx, "ai.max_evaluation_retries", "ai.evaluation_retry_window_hours", "scoring.clamp_min")
	if err != nil {
		return evaluation.EvalParams{}, err
	}
	max := int(snap.Int("ai.max_evaluation_retries"))
	if max < 0 {
		max = 1
	}
	hours := int(snap.Int("ai.evaluation_retry_window_hours"))
	if hours <= 0 {
		hours = 24
	}
	return evaluation.EvalParams{
		MaxEvaluationRetries: max, EvaluationRetryWindowHours: hours, ClampMin: snap.Float("scoring.clamp_min"),
	}, nil
}

func quotaError(code string, resetAt time.Time) error {
	details := map[string]any{}
	if !resetAt.IsZero() {
		details["reset_at"] = resetAt.UTC()
	}
	switch code {
	case quota.CodeQueueLimit:
		return apperr.WithDetails(apperr.CodeAIQueueLimitExceeded, details)
	default:
		return apperr.WithDetails(apperr.CodeAIQuotaExceeded, details)
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func zeroTime(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}
