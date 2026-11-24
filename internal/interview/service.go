package interview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	store     *Store
	queue     taskqueue.Queue
	quotas    *quota.Manager
	settings  SettingsReader
	clock     clock.Clock
	contracts map[string]int
}

func NewService(store *Store, queue taskqueue.Queue, q *quota.Manager, s SettingsReader,
	c clock.Clock, contracts map[string]int) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{store: store, queue: queue, quotas: q, settings: s, clock: c, contracts: contracts}
}

// SessionStateJSON adalah bentuk session_state pada interview_sessions.
type SessionStateJSON struct {
	Phase               string         `json:"phase"`
	QuestionQueue       []string       `json:"question_queue"`
	MainAsked           int            `json:"main_asked"`
	FollowupsByQuestion map[string]int `json:"followups_by_question"`
	AskedTotal          int            `json:"asked_total"`
	PendingMessageID    string         `json:"pending_message_id,omitempty"`
	LastAnswerMessageID string         `json:"last_answer_message_id,omitempty"`
	FinishRequested     bool           `json:"finish_requested,omitempty"`
}

func decodeState(raw []byte) SessionStateJSON {
	var s SessionStateJSON
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.FollowupsByQuestion == nil {
		s.FollowupsByQuestion = map[string]int{}
	}
	return s
}

// Create membuat sesi baru dengan konfigurasi dan versi prompt yang di-pin.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, profileKind string,
	topicIDs []uuid.UUID, locale string, retryOf *uuid.UUID) (uuid.UUID, error) {
	profile, err := s.store.GetProfile(ctx, profileKind)
	if err != nil {
		return uuid.Nil, apperr.New(apperr.CodeNotFound)
	}
	snap, err := s.settings.Snapshot(ctx, "interview.min_topics_per_session", "interview.max_topics_per_session")
	if err != nil {
		return uuid.Nil, err
	}
	minTopics := int(snap.Int("interview.min_topics_per_session"))
	maxTopics := int(snap.Int("interview.max_topics_per_session"))
	if minTopics <= 0 {
		minTopics = 1
	}
	if maxTopics <= 0 {
		maxTopics = 5
	}
	if len(topicIDs) < minTopics || len(topicIDs) > maxTopics {
		return uuid.Nil, apperr.Validation([]apperr.FieldError{{
			Field: "topic_ids", Reason: fmt.Sprintf("range_%d_%d", minTopics, maxTopics),
		}}...)
	}
	questions, err := s.pickQuestions(ctx, topicIDs, locale)
	if err != nil {
		return uuid.Nil, err
	}
	if len(questions) == 0 {
		return uuid.Nil, apperr.New(apperr.CodeTranslationNotAvailable)
	}
	weights := map[string]int{}
	for k, v := range profile.Weights {
		weights[k] = v
	}
	configuration := map[string]any{
		"topic_ids":      topicIDs,
		"question_count": len(questions),
		"limits": map[string]int{
			"max_followups_per_question": profile.MaxFollowupsPerQuestion,
			"total_cap":                  profile.TotalCap,
			"max_depth":                  profile.MaxDepth,
		},
		"weights": weights,
	}
	cfgJSON, _ := json.Marshal(configuration)
	queue := make([]string, 0, len(questions))
	for _, q := range questions {
		queue = append(queue, q)
	}
	state := SessionStateJSON{
		Phase: "awaiting_start", QuestionQueue: queue,
		FollowupsByQuestion: map[string]int{},
	}
	stateJSON, _ := json.Marshal(state)
	versions := map[string]int{}
	for _, taskType := range []string{"answer_evaluation", "interview_followup", "interview_evaluation"} {
		versions[taskType] = s.contracts[taskType]
	}
	versionsJSON, _ := json.Marshal(versions)
	id, _ := uuid.NewV7()
	sess := Session{
		ID: id, UserID: userID, Status: "CREATED", InterviewType: profileKind,
		Configuration: cfgJSON, SessionState: stateJSON, Locale: locale,
		RetryOf: retryOf, PromptVersions: versionsJSON,
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func (s *Service) pickQuestions(ctx context.Context, topicIDs []uuid.UUID, locale string) ([]string, error) {
	rows, err := s.store.pool.Query(ctx, `SELECT q.id FROM questions q
		JOIN question_translations qt ON qt.question_id=q.id AND qt.locale=$1 AND qt.status='published'
		WHERE q.topic_id = ANY($2::uuid[]) AND q.deleted_at IS NULL AND q.is_active
		  AND q.purpose IN ('interview','both') ORDER BY q.id`, locale, topicIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id.String())
	}
	return out, rows.Err()
}

func (s *Service) transition(ctx context.Context, sessID, userID uuid.UUID,
	action evaluation.SessionAction) error {
	now := s.clock.Now()
	snap, err := s.settings.Snapshot(ctx, "interview.pause_ttl_days")
	if err != nil {
		return err
	}
	days := int(snap.Int("interview.pause_ttl_days"))
	if days <= 0 {
		days = 7
	}
	return db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
		sess, err := s.store.LockSession(ctx, tx, sessID)
		if errors.Is(err, ErrNotFound) || sess.UserID != userID {
			return apperr.New(apperr.CodeNotFound)
		}
		if err != nil {
			return err
		}
		sessionState := evaluation.SessionState{
			Status: sess.Status, StartedAt: sess.StartedAt, PausedAt: sess.PausedAt,
			ExpiresAt: sess.ExpiresAt, AbandonedAt: sess.AbandonedAt,
			Phase: decodeState(sess.SessionState).Phase,
		}
		res := evaluation.Transition(sessionState, action, now, evaluation.SessionParams{PauseTTLDays: days})
		switch res.ErrorCode {
		case "invalid_session_transition":
			return apperr.New(apperr.CodeInvalidSessionTransition)
		case "session_expired":
			sess.Status = res.Session.Status
			sess.AbandonedAt = res.Session.AbandonedAt
			if err := s.store.UpdateSession(ctx, tx, sess); err != nil {
				return err
			}
			return apperr.New(apperr.CodeSessionExpired)
		}
		sess.Status = res.Session.Status
		sess.StartedAt = res.Session.StartedAt
		sess.PausedAt = res.Session.PausedAt
		sess.ExpiresAt = res.Session.ExpiresAt
		sess.AbandonedAt = res.Session.AbandonedAt
		state := decodeState(sess.SessionState)
		state.Phase = res.Session.Phase
		if action == "start" && len(state.QuestionQueue) > 0 {
			qid := parseUUIDValue(parseUUID(state.QuestionQueue[0]))
			sess.CurrentQuestionID = &qid
			state.MainAsked = 1
			state.AskedTotal = 1
			state.QuestionQueue = state.QuestionQueue[1:]
			if _, err := s.store.AppendMessage(ctx, tx, Message{
				SessionID: sessID, Role: "ai", Kind: "question", QuestionID: qid,
				Content: "Pertanyaan utama pertama.",
			}); err != nil {
				return err
			}
		}
		if action == "finish" {
			state.FinishRequested = true
		}
		stateJSON, _ := json.Marshal(state)
		sess.SessionState = stateJSON
		if action == "finish" {
			// Finalisasi dipanggil setelah transaksi commit (lihat handler).
			return s.store.UpdateSession(ctx, tx, sess)
		}
		return s.store.UpdateSession(ctx, tx, sess)
	})
}

func parseUUID(s string) *uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}

func (s *Service) Start(ctx context.Context, id, uid uuid.UUID) error {
	return s.transition(ctx, id, uid, evaluation.SessionAction("start"))
}
func (s *Service) Pause(ctx context.Context, id, uid uuid.UUID) error {
	return s.transition(ctx, id, uid, evaluation.SessionAction("pause"))
}
func (s *Service) Resume(ctx context.Context, id, uid uuid.UUID) error {
	return s.transition(ctx, id, uid, evaluation.SessionAction("resume"))
}
func (s *Service) Abandon(ctx context.Context, id, uid uuid.UUID) error {
	return s.transition(ctx, id, uid, evaluation.SessionAction("abandon"))
}

// SubmitAnswer menyimpan pesan jawaban dan men-enqueue Evaluator dalam satu transaksi.
func (s *Service) SubmitAnswer(ctx context.Context, sessID, uid uuid.UUID, questionID uuid.UUID,
	messageID string, text string) (uuid.UUID, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return uuid.Nil, apperr.Validation([]apperr.FieldError{{Field: "content", Reason: "required"}}...)
	}
	now := s.clock.Now()
	var parentID uuid.UUID
	if messageID != "" {
		id, err := uuid.Parse(messageID)
		if err != nil {
			return uuid.Nil, apperr.New(apperr.CodeStaleQuestion)
		}
		parentID = id
	}
	var enqueued uuid.UUID
	err := db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
		sess, err := s.store.LockSession(ctx, tx, sessID)
		if errors.Is(err, ErrNotFound) || sess.UserID != uid {
			return apperr.New(apperr.CodeNotFound)
		}
		if err != nil {
			return err
		}
		if sess.Status != "IN_PROGRESS" {
			return apperr.New(apperr.CodeSessionNotActive)
		}
		state := decodeState(sess.SessionState)
		if state.Phase != "awaiting_answer" {
			return apperr.New(apperr.CodeStaleQuestion)
		}
		parent := parentID
		if parent == uuid.Nil {
			// Cari pesan pertanyaan AI aktif untuk pertanyaan utama/follow-up saat ini.
			if err := tx.QueryRow(ctx, `SELECT id FROM interview_messages
				WHERE interview_session_id=$1 AND role='ai' AND question_id=$2
				ORDER BY sequence DESC LIMIT 1`, sessID, parseUUIDValue(sess.CurrentQuestionID)).Scan(&parent); err != nil {
				return apperr.New(apperr.CodeStaleQuestion)
			}
		}
		// Validasi pesan induk milik sesi ini.
		parentMsg, err := s.store.GetMessage(ctx, tx, parent)
		if err != nil || parentMsg.SessionID != sessID {
			return apperr.New(apperr.CodeStaleQuestion)
		}
		decision, err := s.quotas.Check(ctx, &uid)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			if decision.Code == quota.CodeQueueLimit {
				return apperr.New(apperr.CodeAIQueueLimitExceeded)
			}
			return apperr.New(apperr.CodeAIQuotaExceeded)
		}
		var contractID *uuid.UUID
		var cid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM prompt_contracts
			WHERE task_type='answer_evaluation' AND version=$1`,
			s.contracts["answer_evaluation"]).Scan(&cid); err == nil {
			contractID = &cid
		}
		var topicID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT topic_id FROM questions WHERE id=$1`, questionID).Scan(&topicID); err != nil {
			return err
		}
		msg, err := s.store.AppendMessage(ctx, tx, Message{
			SessionID: sessID, Role: "user", Kind: "answer", QuestionID: questionID,
			ParentID: &parent, Depth: parentMsg.Depth + 1, Content: text,
			EvalStatus: "pending", ContractID: contractID,
		})
		if err != nil {
			return err
		}
		state.PendingMessageID = msg.ID.String()
		state.LastAnswerMessageID = msg.ID.String()
		state.Phase = "evaluating"
		stateJSON, _ := json.Marshal(state)
		sess.SessionState = stateJSON
		if err := s.store.UpdateSession(ctx, tx, sess); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"question_id": questionID.String(), "user_id": uid.String(),
			"topic_id": topicID.String(), "answer_text": text,
			"locale": sess.Locale, "interview_message_id": msg.ID.String(),
			"session_id": sessID.String(),
		})
		t, err := s.queue.Enqueue(ctx, taskqueue.NewTask{
			UserID: &uid, TaskType: "answer_evaluation", Priority: 1, Payload: payload,
			ResourceType: "interview_message", ResourceID: &msg.ID,
			PromptContractID: contractID,
		}, now)
		if err != nil {
			return err
		}
		enqueued = t.ID
		return nil
	})
	return enqueued, err
}

func parseUUIDValue(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}
