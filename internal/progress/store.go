// Package progress menyajikan MasteryRecord, Attempt, dan catatan Misconception
// milik Learner (Requirement 12, 10.4).
package progress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store mengakses tabel mastery_records, topic_attempts, dan user_misconceptions.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ErrNotFound menandakan data progress tidak ditemukan.
var ErrNotFound = errors.New("progress: tidak ditemukan")

// MasteryView adalah DTO progress satu Topic. interview_ready sengaja nullable.
type MasteryView struct {
	TopicID        string          `json:"topic_id"`
	Status         string          `json:"status"`
	AttemptCount   int             `json:"attempt_count"`
	AvgLast3       any             `json:"avg_last3"`
	BestScore      any             `json:"best_score"`
	LastAttemptID  *string         `json:"last_attempt_id"`
	MasteredAt     any             `json:"mastered_at"`
	InterviewReady *bool           `json:"interview_ready"`
	Evidence       json.RawMessage `json:"evidence"`
	RuleVersion    string          `json:"rule_version"`
}

func scanMastery(rows pgx.Rows) (MasteryView, error) {
	var v MasteryView
	var topicID, lastID uuid.UUID
	var lastIDNull, interviewReadyValid bool
	var interviewReady bool
	var evidence []byte
	err := rows.Scan(&topicID, &v.Status, &v.AttemptCount, &v.AvgLast3, &v.BestScore,
		&lastID, &lastIDNull, &v.MasteredAt, &interviewReady, &interviewReadyValid, &evidence)
	if err != nil {
		return MasteryView{}, err
	}
	v.TopicID = topicID.String()
	if !lastIDNull {
		s := lastID.String()
		v.LastAttemptID = &s
	}
	if interviewReadyValid {
		v.InterviewReady = &interviewReady
	}
	if len(evidence) == 0 {
		evidence = []byte("{}")
	}
	v.Evidence = json.RawMessage(evidence)
	return v, nil
}

const masterySelect = `topic_id, status, attempt_count, avg_last3, best_score,
	last_attempt_id, last_attempt_id IS NOT NULL, mastered_at,
	interview_ready, interview_ready IS NOT NULL, evidence`

// ListMastery mengembalikan seluruh MasteryRecord milik user.
func (s *Store) ListMastery(ctx context.Context, userID uuid.UUID) ([]MasteryView, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+masterySelect+` FROM mastery_records
		WHERE user_id = $1 ORDER BY topic_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]MasteryView, 0)
	for rows.Next() {
		v, err := scanMastery(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// GetMastery mengembalikan MasteryRecord satu Topic. Bila belum ada, dikembalikan
// status NOT_STARTED tanpa Attempt (Requirement 10.4).
func (s *Store) GetMastery(ctx context.Context, userID, topicID uuid.UUID) (MasteryView, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+masterySelect+` FROM mastery_records
		WHERE user_id = $1 AND topic_id = $2`, userID, topicID)
	if err != nil {
		return MasteryView{}, err
	}
	defer rows.Close()
	if rows.Next() {
		return scanMastery(rows)
	}
	return MasteryView{
		TopicID: topicID.String(), Status: "NOT_STARTED", AttemptCount: 0,
		Evidence: json.RawMessage("{}"), RuleVersion: "mastery_v1",
	}, nil
}

// MisconceptionView adalah DTO catatan Misconception Learner.
type MisconceptionView struct {
	ID              string `json:"id"`
	MisconceptionID string `json:"misconception_id"`
	Code            string `json:"code"`
	Severity        string `json:"severity"`
	Status          string `json:"status"`
	DetectedAt      any    `json:"detected_at"`
	ResolvedAt      any    `json:"resolved_at"`
	Occurrences     int    `json:"occurrences"`
}

// ListMisconceptions mengembalikan catatan Misconception milik user, opsional
// difilter berdasarkan status (Requirement 10.4).
func (s *Store) ListMisconceptions(ctx context.Context, userID uuid.UUID, status string) ([]MisconceptionView, error) {
	q := `SELECT um.id, um.misconception_id, m.code, m.severity, um.status,
		um.detected_at, um.resolved_at, um.occurrences
		FROM user_misconceptions um JOIN misconceptions m ON m.id = um.misconception_id
		WHERE um.user_id = $1`
	args := []any{userID}
	if status == "detected" || status == "resolved" {
		q += ` AND um.status = $2`
		args = append(args, status)
	}
	q += ` ORDER BY um.detected_at DESC, um.id DESC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]MisconceptionView, 0)
	for rows.Next() {
		var v MisconceptionView
		var id, mid uuid.UUID
		if err := rows.Scan(&id, &mid, &v.Code, &v.Severity, &v.Status,
			&v.DetectedAt, &v.ResolvedAt, &v.Occurrences); err != nil {
			return nil, err
		}
		v.ID = id.String()
		v.MisconceptionID = mid.String()
		items = append(items, v)
	}
	return items, rows.Err()
}

// TopicExists memeriksa keberadaan Topic untuk membedakan not_found dari
// default NOT_STARTED.
func (s *Store) TopicExists(ctx context.Context, topicID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM topics WHERE id = $1 AND deleted_at IS NULL)`, topicID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("progress: cek topic: %w", err)
	}
	return exists, nil
}
