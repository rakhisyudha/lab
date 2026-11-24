// Package assessment mengelola assessment attempt, jawaban, submit, dan
// Evaluation_Retry (Requirement 20, 25).
package assessment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var ErrNotFound = errors.New("assessment: tidak ditemukan")

// QuestionRow adalah pertanyaan yang dipilih untuk attempt.
type QuestionRow struct {
	ID             uuid.UUID
	Weight         float64
	CognitiveLevel string
	Prompt         string
	Position       int
}

// PickQuestions memilih Question published pada Topic dan Locale dengan
// purpose assessment/both, mengutamakan pertanyaan aplikatif.
func (s *Store) PickQuestions(ctx context.Context, topicID uuid.UUID, locale string, limit int) ([]QuestionRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT q.id, q.weight, q.cognitive_level, qt.prompt
		FROM questions q JOIN question_translations qt
		  ON qt.question_id = q.id AND qt.locale = $2 AND qt.status = 'published'
		WHERE q.topic_id = $1 AND q.deleted_at IS NULL AND q.is_active
		  AND q.purpose IN ('assessment','both')
		ORDER BY CASE WHEN q.cognitive_level IN ('application','scenario') THEN 0 ELSE 1 END,
		         q.id`, topicID, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]QuestionRow, 0)
	for rows.Next() {
		var q QuestionRow
		if err := rows.Scan(&q.ID, &q.Weight, &q.CognitiveLevel, &q.Prompt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, rows.Err()
}

// CreateAttempt membuat attempt beserta baris jawaban draf.
func (s *Store) CreateAttempt(ctx context.Context, userID, topicID uuid.UUID, locale string, questions []QuestionRow) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	hasApp := false
	for _, q := range questions {
		if q.CognitiveLevel == "application" || q.CognitiveLevel == "scenario" {
			hasApp = true
		}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO assessment_attempts
		(id, user_id, topic_id, locale, has_application_question) VALUES ($1,$2,$3,$4,$5)`,
		id, userID, topicID, locale, hasApp); err != nil {
		return uuid.Nil, err
	}
	for i, q := range questions {
		if _, err := tx.Exec(ctx, `INSERT INTO assessment_answers (attempt_id, question_id, position)
			VALUES ($1,$2,$3)`, id, q.ID, i+1); err != nil {
			return uuid.Nil, err
		}
	}
	return id, tx.Commit(ctx)
}

// AttemptRow adalah ringkasan satu attempt.
type AttemptRow struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	TopicID     uuid.UUID  `json:"topic_id"`
	Locale      string     `json:"locale"`
	Status      string     `json:"status"`
	ScorePct    *float64   `json:"score_pct"`
	HasApp      bool       `json:"has_application_question"`
	SubmittedAt *time.Time `json:"submitted_at"`
	ScoredAt    *time.Time `json:"scored_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func scanAttempt(row interface{ Scan(...any) error }) (AttemptRow, error) {
	var a AttemptRow
	var score sql.NullFloat64
	var submitted, scored, resolved sql.NullTime
	err := row.Scan(&a.ID, &a.UserID, &a.TopicID, &a.Locale, &a.Status, &score, &a.HasApp,
		&submitted, &scored, &resolved, &a.CreatedAt)
	if err != nil {
		return AttemptRow{}, err
	}
	if score.Valid {
		a.ScorePct = &score.Float64
	}
	if submitted.Valid {
		a.SubmittedAt = &submitted.Time
	}
	if scored.Valid {
		a.ScoredAt = &scored.Time
	}
	if resolved.Valid {
		a.ResolvedAt = &resolved.Time
	}
	return a, nil
}

const attemptColumns = `id, user_id, topic_id, locale, status, score_pct, has_application_question,
	submitted_at, scored_at, resolved_at, created_at`

func (s *Store) GetAttempt(ctx context.Context, id uuid.UUID) (AttemptRow, error) {
	a, err := scanAttempt(s.pool.QueryRow(ctx, `SELECT `+attemptColumns+` FROM assessment_attempts WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return AttemptRow{}, ErrNotFound
	}
	return a, err
}

// FindAttemptForUpdate mengunci attempt dalam transaksi.
func (s *Store) FindAttemptForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (AttemptRow, error) {
	a, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM assessment_attempts WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return AttemptRow{}, ErrNotFound
	}
	return a, err
}

// AnswerRow adalah satu jawaban assessment.
type AnswerRow struct {
	ID               uuid.UUID  `json:"id"`
	AttemptID        uuid.UUID  `json:"attempt_id"`
	QuestionID       uuid.UUID  `json:"question_id"`
	Position         int        `json:"position"`
	AnswerText       *string    `json:"answer_text"`
	IsBlank          bool       `json:"is_blank"`
	EvalStatus       *string    `json:"evaluation_status"`
	RetryCount       int        `json:"evaluation_retry_count"`
	FailedAt         *time.Time `json:"evaluation_failed_at"`
	PromptContractID *uuid.UUID `json:"evaluation_prompt_contract_id"`
}

const answerColumns = `id, attempt_id, question_id, position, answer_text, is_blank,
	evaluation_status, evaluation_retry_count, evaluation_failed_at, evaluation_prompt_contract_id`

func scanAnswer(row interface{ Scan(...any) error }) (AnswerRow, error) {
	var a AnswerRow
	var text, status, failedAt, contractID sql.NullString
	err := row.Scan(&a.ID, &a.AttemptID, &a.QuestionID, &a.Position, &text, &a.IsBlank,
		&status, &a.RetryCount, &failedAt, &contractID)
	if err != nil {
		return AnswerRow{}, err
	}
	if text.Valid {
		a.AnswerText = &text.String
	}
	if status.Valid {
		a.EvalStatus = &status.String
	}
	if failedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, failedAt.String)
		if err != nil {
			return AnswerRow{}, err
		}
		a.FailedAt = &t
	}
	if contractID.Valid {
		id, err := uuid.Parse(contractID.String)
		if err != nil {
			return AnswerRow{}, err
		}
		a.PromptContractID = &id
	}
	return a, nil
}

func (s *Store) ListAnswers(ctx context.Context, attemptID uuid.UUID) ([]AnswerRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+answerColumns+` FROM assessment_answers WHERE attempt_id=$1 ORDER BY position`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AnswerRow, 0)
	for rows.Next() {
		a, err := scanAnswer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) FindAnswerForUpdate(ctx context.Context, tx pgx.Tx, attemptID, questionID uuid.UUID) (AnswerRow, error) {
	a, err := scanAnswer(tx.QueryRow(ctx, `SELECT `+answerColumns+` FROM assessment_answers
		WHERE attempt_id=$1 AND question_id=$2 FOR UPDATE`, attemptID, questionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return AnswerRow{}, ErrNotFound
	}
	return a, err
}

// SaveAnswer menyimpan isi jawaban draf.
func (s *Store) SaveAnswer(ctx context.Context, attemptID, questionID uuid.UUID, text string, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE assessment_answers SET answer_text=$3, answered_at=$4
		WHERE attempt_id=$1 AND question_id=$2 AND evaluation_status IS NULL`, attemptID, questionID, text, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateAnswerStatus menerapkan hasil ApplyEvaluationEvent ke baris jawaban.
func (s *Store) UpdateAnswerStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string,
	retryCount int, failedAt *time.Time, contractID *uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE assessment_answers SET evaluation_status=$2,
		evaluation_retry_count=$3, evaluation_failed_at=$4, evaluation_prompt_contract_id=COALESCE($5, evaluation_prompt_contract_id)
		WHERE id=$1`, id, status, retryCount, failedAt, contractID)
	return err
}

// SetSubmitted mengubah status attempt menjadi submitted.
func (s *Store) SetSubmitted(ctx context.Context, tx pgx.Tx, id uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE assessment_attempts SET status='submitted', submitted_at=$2 WHERE id=$1`, id, now)
	return err
}

// FinalizeAttempt menulis hasil akhir attempt.
func (s *Store) FinalizeAttempt(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string,
	score *float64, hasApp bool, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE assessment_attempts SET status=$2, score_pct=$3,
		has_application_question=$4, scored_at=$5, resolved_at=$5 WHERE id=$1`,
		id, status, score, hasApp, now)
	return err
}

// ListAttempts mengembalikan attempt milik user untuk cursor pagination.
func (s *Store) ListAttempts(ctx context.Context, userID uuid.UUID, after *uuid.UUID, limit int) ([]AttemptRow, error) {
	q := `SELECT ` + attemptColumns + ` FROM assessment_attempts WHERE user_id=$1`
	args := []any{userID}
	if after != nil {
		q += ` AND id > $2`
		args = append(args, *after)
	}
	q += ` ORDER BY id LIMIT $` + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AttemptRow, 0)
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
