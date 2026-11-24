// Package interview mengelola Interview_Session, Transcript, dan orkestrasi jawaban.
package interview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var ErrNotFound = errors.New("interview: tidak ditemukan")

// ProfileView adalah DTO interview_profiles.
type ProfileView struct {
	InterviewType           string         `json:"interview_type"`
	MinQuestions            int            `json:"min_questions"`
	MaxQuestions            int            `json:"max_questions"`
	MaxFollowupsPerQuestion int            `json:"max_followups_per_question"`
	TotalCap                int            `json:"total_cap"`
	MaxDepth                int            `json:"max_depth"`
	Weights                 map[string]int `json:"weights"`
}

func (s *Store) ListProfiles(ctx context.Context) ([]ProfileView, error) {
	rows, err := s.pool.Query(ctx, `SELECT interview_type, min_questions, max_questions,
		max_followups_per_question, total_cap, max_depth, w_technical_accuracy,
		w_conceptual_understanding, w_reasoning, w_practical_application, w_communication
		FROM interview_profiles ORDER BY interview_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProfileView, 0)
	for rows.Next() {
		var p ProfileView
		var a, b, c, d, e int
		if err := rows.Scan(&p.InterviewType, &p.MinQuestions, &p.MaxQuestions,
			&p.MaxFollowupsPerQuestion, &p.TotalCap, &p.MaxDepth, &a, &b, &c, &d, &e); err != nil {
			return nil, err
		}
		p.Weights = map[string]int{
			"technical_accuracy": a, "conceptual_understanding": b, "reasoning": c,
			"practical_application": d, "communication": e,
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProfile(ctx context.Context, kind string) (ProfileView, error) {
	rows, err := s.pool.Query(ctx, `SELECT interview_type, min_questions, max_questions,
		max_followups_per_question, total_cap, max_depth, w_technical_accuracy,
		w_conceptual_understanding, w_reasoning, w_practical_application, w_communication
		FROM interview_profiles WHERE interview_type = $1`, kind)
	if err != nil {
		return ProfileView{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return ProfileView{}, ErrNotFound
	}
	var p ProfileView
	var a, b, c, d, e int
	if err := rows.Scan(&p.InterviewType, &p.MinQuestions, &p.MaxQuestions,
		&p.MaxFollowupsPerQuestion, &p.TotalCap, &p.MaxDepth, &a, &b, &c, &d, &e); err != nil {
		return ProfileView{}, err
	}
	p.Weights = map[string]int{
		"technical_accuracy": a, "conceptual_understanding": b, "reasoning": c,
		"practical_application": d, "communication": e,
	}
	return p, nil
}

// Session adalah ringkasan interview_sessions.
type Session struct {
	ID                uuid.UUID       `json:"id"`
	UserID            uuid.UUID       `json:"user_id"`
	Status            string          `json:"status"`
	InterviewType     string          `json:"interview_type"`
	Configuration     json.RawMessage `json:"configuration"`
	CurrentQuestionID *uuid.UUID      `json:"current_question_id"`
	CurrentIndex      int             `json:"current_question_index"`
	SessionState      json.RawMessage `json:"session_state"`
	Locale            string          `json:"locale"`
	RetryOf           *uuid.UUID      `json:"retry_of_session_id"`
	PromptVersions    json.RawMessage `json:"prompt_contract_versions"`
	StartedAt         *time.Time      `json:"started_at"`
	PausedAt          *time.Time      `json:"paused_at"`
	ExpiresAt         *time.Time      `json:"expires_at"`
	CompletedAt       *time.Time      `json:"completed_at"`
	AbandonedAt       *time.Time      `json:"abandoned_at"`
	DeletedAt         *time.Time      `json:"deleted_at"`
	CreatedAt         time.Time       `json:"created_at"`
}

const sessionColumns = `id, user_id, status, interview_type, configuration, current_question_id,
	current_question_index, session_state, locale, retry_of_session_id, prompt_contract_versions,
	started_at, paused_at, expires_at, completed_at, abandoned_at, deleted_at, created_at`

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var s Session
	var cfg, state, versions []byte
	var qid, retry sql.NullString
	var started, paused, expires, completed, abandoned, deleted sql.NullTime
	err := row.Scan(&s.ID, &s.UserID, &s.Status, &s.InterviewType, &cfg, &qid, &s.CurrentIndex,
		&state, &s.Locale, &retry, &versions, &started, &paused, &expires,
		&completed, &abandoned, &deleted, &s.CreatedAt)
	if err != nil {
		return Session{}, err
	}
	s.Configuration, s.SessionState, s.PromptVersions = cfg, state, versions
	if qid.Valid {
		id, _ := uuid.Parse(qid.String)
		s.CurrentQuestionID = &id
	}
	if retry.Valid {
		id, _ := uuid.Parse(retry.String)
		s.RetryOf = &id
	}
	if started.Valid {
		s.StartedAt = &started.Time
	}
	if paused.Valid {
		s.PausedAt = &paused.Time
	}
	if expires.Valid {
		s.ExpiresAt = &expires.Time
	}
	if completed.Valid {
		s.CompletedAt = &completed.Time
	}
	if abandoned.Valid {
		s.AbandonedAt = &abandoned.Time
	}
	if deleted.Valid {
		s.DeletedAt = &deleted.Time
	}
	return s, nil
}

func (s *Store) GetSession(ctx context.Context, id uuid.UUID) (Session, error) {
	sess, err := scanSession(s.pool.QueryRow(ctx, `SELECT `+sessionColumns+` FROM interview_sessions
		WHERE id=$1 AND deleted_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *Store) LockSession(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Session, error) {
	sess, err := scanSession(tx.QueryRow(ctx, `SELECT `+sessionColumns+` FROM interview_sessions
		WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *Store) ListSessions(ctx context.Context, userID uuid.UUID, after *uuid.UUID, limit int) ([]Session, error) {
	q := `SELECT ` + sessionColumns + ` FROM interview_sessions
		WHERE user_id=$1 AND deleted_at IS NULL`
	args := []any{userID}
	if after != nil {
		q += ` AND id > $2`
		args = append(args, *after)
	}
	q += ` ORDER BY id LIMIT $` + itoa(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Session, 0)
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// CreateSession menyimpan sesi baru.
func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO interview_sessions
		(id, user_id, status, interview_type, configuration, current_question_id, current_question_index,
		 session_state, locale, retry_of_session_id, prompt_contract_versions)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		sess.ID, sess.UserID, sess.Status, sess.InterviewType, sess.Configuration,
		sess.CurrentQuestionID, sess.CurrentIndex, sess.SessionState, sess.Locale,
		sess.RetryOf, sess.PromptVersions)
	return err
}

// UpdateSession menyimpan perubahan status, waktu, dan state sesi.
func (s *Store) UpdateSession(ctx context.Context, tx pgx.Tx, sess Session) error {
	_, err := tx.Exec(ctx, `UPDATE interview_sessions SET status=$2, current_question_id=$3,
		current_question_index=$4, session_state=$5, started_at=$6, paused_at=$7,
		expires_at=$8, completed_at=$9, abandoned_at=$10 WHERE id=$1`,
		sess.ID, sess.Status, sess.CurrentQuestionID, sess.CurrentIndex, sess.SessionState,
		sess.StartedAt, sess.PausedAt, sess.ExpiresAt, sess.CompletedAt, sess.AbandonedAt)
	return err
}

// Message adalah satu pesan Transcript.
type Message struct {
	ID         uuid.UUID  `json:"id"`
	SessionID  uuid.UUID  `json:"interview_session_id"`
	Sequence   int        `json:"sequence"`
	Role       string     `json:"role"`
	Kind       string     `json:"kind"`
	QuestionID uuid.UUID  `json:"question_id"`
	ParentID   *uuid.UUID `json:"parent_message_id"`
	Depth      int        `json:"depth"`
	Content    string     `json:"content"`
	EvalStatus string     `json:"evaluation_status"`
	RetryCount int        `json:"evaluation_retry_count"`
	FailedAt   *time.Time `json:"evaluation_failed_at"`
	ContractID *uuid.UUID `json:"evaluation_prompt_contract_id"`
	CreatedAt  time.Time  `json:"created_at"`
}

const messageColumns = `id, interview_session_id, sequence, role, kind, question_id, parent_message_id,
	depth, content, evaluation_status, evaluation_retry_count, evaluation_failed_at,
	evaluation_prompt_contract_id, created_at`

func scanMessage(row interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var parent, contract sql.NullString
	var status, failed sql.NullString
	var failedAt sql.NullTime
	err := row.Scan(&m.ID, &m.SessionID, &m.Sequence, &m.Role, &m.Kind, &m.QuestionID,
		&parent, &m.Depth, &m.Content, &status, &m.RetryCount, &failedAt, &contract, &m.CreatedAt)
	if err != nil {
		return Message{}, err
	}
	if parent.Valid {
		id, _ := uuid.Parse(parent.String)
		m.ParentID = &id
	}
	if contract.Valid {
		id, _ := uuid.Parse(contract.String)
		m.ContractID = &id
	}
	if status.Valid {
		m.EvalStatus = status.String
	}
	if failedAt.Valid {
		m.FailedAt = &failedAt.Time
	}
	_ = failed
	return m, nil
}

func (s *Store) ListMessages(ctx context.Context, sessionID uuid.UUID, after int, limit int) ([]Message, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+messageColumns+` FROM interview_messages
		WHERE interview_session_id=$1 AND sequence>$2 ORDER BY sequence LIMIT $3`, sessionID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Message, 0)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AppendMessage menambah pesan dengan sequence berikutnya.
func (s *Store) AppendMessage(ctx context.Context, tx pgx.Tx, m Message) (Message, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Message{}, err
	}
	var seq int
	// Baris sesi sudah dikunci FOR UPDATE oleh pemanggil, sehingga sequence aman.
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM interview_messages
		WHERE interview_session_id=$1`, m.SessionID).Scan(&seq); err != nil {
		return Message{}, err
	}
	m.ID, m.Sequence = id, seq
	_, err = tx.Exec(ctx, `INSERT INTO interview_messages
		(id, interview_session_id, sequence, role, kind, question_id, parent_message_id, depth,
		 content, evaluation_status, evaluation_retry_count, evaluation_prompt_contract_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		m.ID, m.SessionID, m.Sequence, m.Role, m.Kind, m.QuestionID, m.ParentID, m.Depth,
		m.Content, nullable(m.EvalStatus), m.RetryCount, m.ContractID)
	return m, err
}

func (s *Store) GetMessage(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Message, error) {
	m, err := scanMessage(tx.QueryRow(ctx, `SELECT `+messageColumns+` FROM interview_messages WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	return m, err
}

func (s *Store) UpdateMessageStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string,
	retryCount int, failedAt *time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE interview_messages SET evaluation_status=$2,
		evaluation_retry_count=$3, evaluation_failed_at=$4 WHERE id=$1`, id, status, retryCount, failedAt)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
