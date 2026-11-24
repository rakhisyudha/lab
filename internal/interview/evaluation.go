package interview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// DimensionResult adalah satu skor dimensi dari model.
type DimensionResult struct {
	Score     float64 `json:"score"`
	Rationale string  `json:"rationale"`
}

// EvalOutput adalah output interview_evaluation yang sudah tervalidasi.
type EvalOutput struct {
	DimensionScores map[string]DimensionResult `json:"dimension_scores"`
	Confidence      float64                    `json:"confidence"`
	Summary         string                     `json:"summary"`
	Strengths       []string                   `json:"strengths"`
	Improvements    []string                   `json:"improvements"`
}

type EvaluationService struct {
	store *Store
	queue taskqueue.Queue
	clock clock.Clock
	fin   *Finalizer
}

func NewEvaluationService(store *Store, queue taskqueue.Queue, c clock.Clock, fin *Finalizer) *EvaluationService {
	if c == nil {
		c = clock.System{}
	}
	return &EvaluationService{store: store, queue: queue, clock: c, fin: fin}
}

// HandleResult menghitung overall_score di server dan menyimpan interview_evaluations.
func (s *EvaluationService) HandleResult(ctx context.Context, sessionID uuid.UUID, out EvalOutput,
	promptContractID *uuid.UUID, modelName string) error {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	var cfg struct {
		Weights map[string]float64 `json:"weights"`
	}
	_ = json.Unmarshal(sess.Configuration, &cfg)
	dimensions := map[string]evaluation.DimensionScore{}
	for key, d := range out.DimensionScores {
		dimensions[key] = evaluation.DimensionScore{
			Score: int(d.Score + 0.5), Rationale: d.Rationale,
		}
	}
	answerScores := s.answerScores(ctx, sessionID)
	scored, err := evaluation.ScoreInterview(evaluation.InterviewEvaluationOutput{
		DimensionScores: dimensions, Confidence: out.Confidence,
	}, cfg.Weights, answerScores, 0.5, 30)
	if err != nil {
		return err
	}
	dimensionJSON, _ := json.Marshal(scored.DimensionScores)
	weightsJSON, _ := json.Marshal(cfg.Weights)
	strengthsJSON, _ := json.Marshal(out.Strengths)
	improvementsJSON, _ := json.Marshal(out.Improvements)
	raw, _ := json.Marshal(out)
	now := s.clock.Now()
	_, err = s.store.pool.Exec(ctx, `INSERT INTO interview_evaluations
		(id, interview_session_id, status, dimension_scores, weights, overall_score, summary,
		 strengths, improvements, needs_review, prompt_contract_id, model_name, raw_output)
		VALUES ($1,$2,'succeeded',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (interview_session_id) DO UPDATE SET status='succeeded',
		  dimension_scores=EXCLUDED.dimension_scores, weights=EXCLUDED.weights,
		  overall_score=EXCLUDED.overall_score, summary=EXCLUDED.summary,
		  raw_output=EXCLUDED.raw_output`,
		uuid.Must(uuid.NewV7()), sessionID, dimensionJSON, weightsJSON, scored.OverallScore,
		out.Summary, strengthsJSON, improvementsJSON, scored.NeedsReview,
		promptContractID, modelName, raw)
	_ = now
	return err
}

func (s *EvaluationService) answerScores(ctx context.Context, sessionID uuid.UUID) []float64 {
	rows, err := s.store.pool.Query(ctx, `SELECT ae.score_pct FROM answer_evaluations ae
		JOIN interview_messages im ON im.id = ae.interview_message_id
		WHERE im.interview_session_id=$1 AND im.evaluation_status='scored'`, sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]float64, 0)
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// MarkFailed menandai evaluasi dimensi gagal.
func (s *EvaluationService) MarkFailed(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.store.pool.Exec(ctx, `INSERT INTO interview_evaluations
		(id, interview_session_id, status) VALUES ($1,$2,'failed')
		ON CONFLICT (interview_session_id) DO UPDATE SET status='failed'`,
		uuid.Must(uuid.NewV7()), sessionID)
	return err
}

// Report mengembalikan data laporan interview milik pemilik sesi.
func (s *EvaluationService) Report(ctx context.Context, sess Session) (map[string]any, error) {
	var status string
	var dimension, weights, raw []byte
	var overall sql.NullFloat64
	var summary string
	err := s.store.pool.QueryRow(ctx, `SELECT status, dimension_scores, weights, overall_score, summary, raw_output
		FROM interview_evaluations WHERE interview_session_id=$1`, sess.ID).Scan(
		&status, &dimension, &weights, &overall, &summary, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		status = "pending"
		dimension = json.RawMessage("{}")
		weights = json.RawMessage("{}")
	} else if err != nil {
		return nil, err
	}
	chains, err := s.fin.chains(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	topics := make([]map[string]any, 0)
	seen := map[string]bool{}
	for _, ch := range chains {
		if seen[ch.TopicID] {
			continue
		}
		seen[ch.TopicID] = true
		agg := evaluation.AggregateInterviewTopic(chains, ch.TopicID)
		var masteryStatus string
		_ = s.store.pool.QueryRow(ctx, `SELECT status FROM mastery_records
			WHERE user_id=$1 AND topic_id=$2`, sess.UserID, ch.TopicID).Scan(&masteryStatus)
		if masteryStatus == "" {
			masteryStatus = "NOT_STARTED"
		}
		entry := map[string]any{"topic_id": ch.TopicID, "mastery_status": masteryStatus}
		if agg != nil && agg.ScorePct != nil {
			entry["score_pct"] = *agg.ScorePct
			entry["has_application_question"] = agg.HasApplicationQuestion
		}
		topics = append(topics, entry)
	}
	var overallValue any
	if overall.Valid {
		overallValue = overall.Float64
	}
	return map[string]any{
		"session_id": sess.ID.String(), "status": status,
		"evaluation_status": status, "overall_score": overallValue,
		"dimension_scores": json.RawMessage(dimension), "weights": json.RawMessage(weights),
		"summary": summary, "topics": topics,
	}, nil
}

// RetryAnswerEvaluation melakukan retry evaluasi satu jawaban interview.
func (s *EvaluationService) RetryAnswerEvaluation(ctx context.Context, sessID, msgID, uid uuid.UUID) (int, error) {
	now := s.clock.Now()
	status := 200
	err := db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
		sess, err := s.store.LockSession(ctx, tx, sessID)
		if errors.Is(err, ErrNotFound) || sess.UserID != uid {
			return apperr.New(apperr.CodeNotFound)
		}
		if err != nil {
			return err
		}
		if sess.Status == "ABANDONED" {
			return apperr.New(apperr.CodeSessionNotActive)
		}
		msg, err := s.store.GetMessage(ctx, tx, msgID)
		if err != nil || msg.SessionID != sessID || msg.Kind != "answer" {
			return apperr.New(apperr.CodeNotFound)
		}
		if msg.EvalStatus == "pending" || msg.EvalStatus == "scored" {
			return nil
		}
		if msg.EvalStatus == "unscored" || msg.RetryCount >= 1 {
			return apperr.New(apperr.CodeEvaluationNotRetryable)
		}
		t, err := s.queue.Enqueue(ctx, taskqueue.NewTask{
			UserID: &uid, TaskType: "answer_evaluation", Priority: 1,
			Payload: mustJSON(map[string]any{
				"session_id": sessID.String(), "interview_message_id": msgID.String(),
				"question_id": msg.QuestionID.String(), "locale": sess.Locale,
			}),
			ResourceType: "interview_message", ResourceID: &msgID,
			PromptContractID: msg.ContractID,
		}, now)
		if err != nil {
			return err
		}
		if err := s.store.UpdateMessageStatus(ctx, tx, msgID, "pending", msg.RetryCount+1, nil); err != nil {
			return err
		}
		_ = t
		status = 202
		return nil
	})
	return status, err
}

// RetrySessionEvaluation mengirim ulang AITask interview_evaluation.
func (s *EvaluationService) RetrySessionEvaluation(ctx context.Context, sessID, uid uuid.UUID) (uuid.UUID, error) {
	sess, err := s.store.GetSession(ctx, sessID)
	if err != nil || sess.UserID != uid {
		return uuid.Nil, apperr.New(apperr.CodeNotFound)
	}
	t, err := s.queue.Enqueue(ctx, taskqueue.NewTask{
		UserID: &uid, TaskType: "interview_evaluation", Priority: 2,
		Payload:      mustJSON(map[string]any{"session_id": sessID.String(), "locale": sess.Locale}),
		ResourceType: "interview_session", ResourceID: &sessID,
	}, s.clock.Now())
	if err != nil {
		return uuid.Nil, err
	}
	return t.ID, nil
}
