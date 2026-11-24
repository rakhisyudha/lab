package interview

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/progress"
)

// EvalResult adalah sinyal Evaluator yang dibutuhkan orkestrator interview.
type EvalResult struct {
	Raw              json.RawMessage
	TopicID          uuid.UUID
	ConceptResults   []evaluation.ConceptSignal
	Detected         []evaluation.MisconceptionSignal
	Relevance        string
	ScorePct         float64
	PromptContractID *uuid.UUID
}

// Orchestrator memproses hasil evaluasi jawaban interview.
type Orchestrator struct {
	store     *Store
	queue     taskqueue.Queue
	clock     clock.Clock
	finalizer *Finalizer
}

func NewOrchestrator(store *Store, queue taskqueue.Queue, c clock.Clock, f *Finalizer) *Orchestrator {
	if c == nil {
		c = clock.System{}
	}
	return &Orchestrator{store: store, queue: queue, clock: c, finalizer: f}
}

// OnAnswerEvaluated menyimpan hasil, mengalokasikan follow-up, dan menggeser sesi.
func (o *Orchestrator) OnAnswerEvaluated(ctx context.Context, sessionID, messageID uuid.UUID, result EvalResult) error {
	now := o.clock.Now()
	return db.WithTx(ctx, o.store.pool, func(tx pgx.Tx) error {
		sess, err := o.store.LockSession(ctx, tx, sessionID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		msg, err := o.store.GetMessage(ctx, tx, messageID)
		if err != nil {
			return nil
		}
		if sess.Status == "ABANDONED" {
			if msg.EvalStatus == "pending" {
				return o.store.UpdateMessageStatus(ctx, tx, messageID, "unscored", msg.RetryCount, msg.FailedAt)
			}
			return nil
		}
		if msg.EvalStatus == "scored" || msg.EvalStatus == "unscored" {
			return nil // hasil terlambat
		}
		if err := o.store.UpdateMessageStatus(ctx, tx, messageID, "scored", msg.RetryCount, nil); err != nil {
			return err
		}
		evalID, _ := uuid.NewV7()
		_, err = tx.Exec(ctx, `INSERT INTO answer_evaluations
			(id, user_id, topic_id, question_id, locale, interview_message_id, prompt_contract_id,
			 kind, relevance, score_pct, concept_results, raw_output)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'ai',$8,$9,$10,$11)
			ON CONFLICT (interview_message_id) WHERE interview_message_id IS NOT NULL DO NOTHING`,
			evalID, sess.UserID, result.TopicID, msg.QuestionID, sess.Locale, messageID,
			result.PromptContractID, result.Relevance, result.ScorePct, mustJSON(result.ConceptResults), result.Raw)
		if err != nil {
			return err
		}
		for _, code := range detectedCodes(result.Detected) {
			var mid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM misconceptions WHERE code=$1`, code).Scan(&mid); err == nil {
				_, _ = tx.Exec(ctx, `INSERT INTO answer_evaluation_misconceptions
					(answer_evaluation_id, misconception_id, kind) VALUES ($1,$2,'detected')
					ON CONFLICT DO NOTHING`, evalID, mid)
			}
		}
		state := decodeState(sess.SessionState)
		// Retry pada jawaban lama: hanya menambah skor, tanpa follow-up.
		if msg.ID.String() != state.PendingMessageID || state.Phase != "evaluating" {
			return o.store.UpdateSession(ctx, tx, sess)
		}
		state.PendingMessageID = ""
		cfg := loadLimits(sess.Configuration)
		decision := evaluation.AllocateFollowUp(evaluation.FollowUpConfig{
			N: cfg.N, C: cfg.C, F: cfg.F, D: cfg.D,
		}, evaluation.FollowUpState{
			M: state.MainAsked, T: state.AskedTotal, FollowupsByQuestion: state.FollowupsByQuestion,
		}, evaluation.AnsweredRef{QuestionID: msg.QuestionID.String(), Depth: msg.Depth},
			evaluation.EvalSignalInterview{
				Status: "scored", ScorePct: result.ScorePct, Relevance: result.Relevance,
				ConceptResults: result.ConceptResults, DetectedMisconceptions: result.Detected,
			}, state.FinishRequested, evaluation.FollowUpParams{
				TriggerScore: 70, BaseCutoff: 30, ScarcityWeight: 40, CriticalBonus: 20,
			})
		if decision.Ask {
			payload, _ := json.Marshal(map[string]any{
				"session_id": sessionID.String(), "question_id": msg.QuestionID.String(),
				"message_id": messageID.String(), "focus_concepts": decision.FocusConcepts,
				"focus_misconceptions": decision.FocusMisconceptions, "locale": sess.Locale,
			})
			task, err := o.queue.Enqueue(ctx, taskqueue.NewTask{
				UserID: &sess.UserID, TaskType: "interview_followup", Priority: 1,
				Payload: payload, ResourceType: "interview_session", ResourceID: &sessionID,
			}, now)
			if err == nil {
				_, err = o.store.AppendMessage(ctx, tx, Message{
					SessionID: sessionID, Role: "ai", Kind: "follow_up",
					QuestionID: msg.QuestionID, ParentID: &messageID, Depth: msg.Depth + 1,
					Content: "Pertanyaan lanjutan akan dibuat.",
				})
				if err == nil {
					state.Phase = "generating_followup"
					state.FollowupsByQuestion[msg.QuestionID.String()]++
					state.AskedTotal++
					_ = task
				}
			}
		}
		if !decision.Ask {
			newState, action := evaluation.NextStep(evaluation.FollowUpState{
				M: state.MainAsked, T: state.AskedTotal, FollowupsByQuestion: state.FollowupsByQuestion,
			}, evaluation.FollowUpConfig{N: cfg.N, C: cfg.C, F: cfg.F, D: cfg.D}, decision)
			state.MainAsked, state.AskedTotal, state.FollowupsByQuestion = newState.M, newState.T, newState.FollowupsByQuestion
			switch action {
			case "next_main":
				if len(state.QuestionQueue) > 0 {
					qid := parseUUIDValue(parseUUID(state.QuestionQueue[0]))
					state.QuestionQueue = state.QuestionQueue[1:]
					sess.CurrentQuestionID = &qid
					_, err = o.store.AppendMessage(ctx, tx, Message{
						SessionID: sessionID, Role: "ai", Kind: "question",
						QuestionID: qid, Content: "Pertanyaan utama berikutnya.",
					})
					state.Phase = "awaiting_answer"
				} else {
					state.Phase = "done"
				}
			case "finalize":
				state.Phase = "done"
			}
		}
		stateJSON, _ := json.Marshal(state)
		sess.SessionState = stateJSON
		if err := o.store.UpdateSession(ctx, tx, sess); err != nil {
			return err
		}
		if state.Phase == "done" || state.FinishRequested {
			return o.finalizer.Finalize(ctx, sess, now)
		}
		return nil
	})
}

// Finalizer menyelesaikan sesi COMPLETED dan mencatat Attempt per Topic.
type Finalizer struct {
	store   *Store
	queue   taskqueue.Queue
	mastery *progress.MasteryService
	clock   clock.Clock
}

func NewFinalizer(store *Store, queue taskqueue.Queue, m *progress.MasteryService, c clock.Clock) *Finalizer {
	if c == nil {
		c = clock.System{}
	}
	return &Finalizer{store: store, queue: queue, mastery: m, clock: c}
}

// Finalize mengubah status COMPLETED tanpa menunggu evaluasi dimensi (DA-10).
func (f *Finalizer) Finalize(ctx context.Context, sess Session, now time.Time) error {
	if sess.Status == "COMPLETED" || sess.Status == "ABANDONED" {
		return nil
	}
	tx, err := f.store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	sess.Status = "COMPLETED"
	sess.CompletedAt = &now
	state := decodeState(sess.SessionState)
	state.Phase = "done"
	stateJSON, _ := json.Marshal(state)
	sess.SessionState = stateJSON
	if err := f.store.UpdateSession(ctx, tx, sess); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"session_id": sess.ID.String(), "locale": sess.Locale})
	_, _ = f.queue.Enqueue(ctx, taskqueue.NewTask{
		UserID: &sess.UserID, TaskType: "interview_evaluation", Priority: 2,
		Payload: payload, ResourceType: "interview_session", ResourceID: &sess.ID,
	}, now)
	return f.TryRecordInterviewAttempts(ctx, sess.ID, now)
}

// TryRecordInterviewAttempts mencatat Attempt per Topic yang memiliki jawaban scored.
func (f *Finalizer) TryRecordInterviewAttempts(ctx context.Context, sessionID uuid.UUID, now time.Time) error {
	sess, err := f.store.GetSession(ctx, sessionID)
	if err != nil || sess.Status != "COMPLETED" {
		return nil
	}
	rows, err := f.store.pool.Query(ctx, `SELECT question_id, topic_id, score_pct, cognitive_level
		FROM interview_messages im JOIN questions q ON q.id = im.question_id
		WHERE im.interview_session_id=$1 AND im.kind='answer' AND im.evaluation_status IN ('pending','failed')`, sessionID)
	if err != nil {
		return err
	}
	pending := rows.Next()
	rows.Close()
	if pending {
		return nil
	}
	chains, err := f.chains(ctx, sessionID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, ch := range chains {
		if seen[ch.TopicID] {
			continue
		}
		seen[ch.TopicID] = true
		agg := evaluation.AggregateInterviewTopic(chains, ch.TopicID)
		if agg == nil || agg.ScorePct == nil {
			continue
		}
		topicID, err := uuid.Parse(ch.TopicID)
		if err != nil {
			continue
		}
		if err := f.mastery.RecordAttempt(ctx, sess.UserID, topicID, sessionID, "interview",
			*agg.ScorePct, agg.HasApplicationQuestion, now, nil); err != nil {
			return err
		}
	}
	return nil
}

type chainRow struct {
	evaluation.InterviewChain
	TopicID uuid.UUID
}

func detectedCodes(items []evaluation.MisconceptionSignal) []string {
	out := make([]string, 0, len(items))
	for _, m := range items {
		out = append(out, m.Code)
	}
	return out
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

type sessionLimits struct{ N, C, F, D int }

func loadLimits(config []byte) sessionLimits {
	var l sessionLimits
	var cfg struct {
		QuestionCount int `json:"question_count"`
		Limits        struct {
			MaxFollowupsPerQuestion int `json:"max_followups_per_question"`
			TotalCap                int `json:"total_cap"`
			MaxDepth                int `json:"max_depth"`
		} `json:"limits"`
	}
	_ = json.Unmarshal(config, &cfg)
	l.N = cfg.QuestionCount
	l.C, l.F, l.D = cfg.Limits.TotalCap, cfg.Limits.MaxFollowupsPerQuestion, cfg.Limits.MaxDepth
	if l.C <= 0 {
		l.C = l.N + 4
	}
	return l
}

func (f *Finalizer) chains(ctx context.Context, sessionID uuid.UUID) ([]evaluation.InterviewChain, error) {
	rows, err := f.store.pool.Query(ctx, `SELECT q.id, q.topic_id, q.weight, q.cognitive_level,
		im.id, im.parent_message_id, im.evaluation_status, ae.score_pct, ae.relevance
		FROM interview_messages im
		JOIN questions q ON q.id = im.question_id
		LEFT JOIN answer_evaluations ae ON ae.interview_message_id = im.id
		WHERE im.interview_session_id=$1 AND im.kind='answer' ORDER BY im.sequence`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byQuestion := map[string]*evaluation.InterviewChain{}
	order := make([]string, 0)
	for rows.Next() {
		var qid, topicID, msgID uuid.UUID
		var parentID *uuid.UUID
		var weight float64
		var level, status string
		var score *float64
		var relevance *string
		if err := rows.Scan(&qid, &topicID, &weight, &level, &msgID, &parentID, &status, &score, &relevance); err != nil {
			return nil, err
		}
		key := qid.String()
		chain, ok := byQuestion[key]
		if !ok {
			chain = &evaluation.InterviewChain{
				QuestionID: key, TopicID: topicID.String(), Weight: weight, CognitiveLevel: level,
			}
			byQuestion[key] = chain
			order = append(order, key)
		}
		rel := ""
		if relevance != nil {
			rel = *relevance
		}
		chain.Answers = append(chain.Answers, evaluation.ChainAnswer{
			EvalStatus: status, ScorePct: score, Relevance: rel,
		})
	}
	out := make([]evaluation.InterviewChain, 0, len(order))
	for _, key := range order {
		out = append(out, *byQuestion[key])
	}
	return out, rows.Err()
}
