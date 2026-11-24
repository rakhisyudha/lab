// Package evaluator merakit input Evaluator, memanggil Gateway, lalu menghitung
// skor final di server (Requirement 14.1, 14.2, 14.9).
package evaluator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/ai/gateway"
	"github.com/rakhisyudha/lab/internal/ai/prompt"
	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

// Payload adalah isi AITask answer_evaluation.
type Payload struct {
	QuestionID         uuid.UUID  `json:"question_id"`
	UserID             uuid.UUID  `json:"user_id"`
	TopicID            uuid.UUID  `json:"topic_id"`
	AnswerText         string     `json:"answer_text"`
	Locale             string     `json:"locale"`
	AssessmentAnswerID *uuid.UUID `json:"assessment_answer_id,omitempty"`
	InterviewMessageID *uuid.UUID `json:"interview_message_id,omitempty"`
}

// Result adalah keluaran Evaluator yang sudah dinilai server.
type Result struct {
	Gateway   gateway.Result
	Score     evaluation.AnswerScore
	Severity  map[string]string
	RawOutput json.RawMessage
}

type SettingsReader interface {
	Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
}

// Service menjalankan pipeline Evaluator untuk satu AITask.
type Service struct {
	pool      *pgxpool.Pool
	gateway   *gateway.Gateway
	contracts map[string]prompt.Contract
	settings  SettingsReader
	clock     clock.Clock
}

func NewService(pool *pgxpool.Pool, gw *gateway.Gateway, contracts map[string]prompt.Contract,
	s SettingsReader, c clock.Clock) *Service {
	if c == nil {
		c = clock.System{}
	}
	return &Service{pool: pool, gateway: gw, contracts: contracts, settings: s, clock: c}
}

var scoringKeys = []string{
	"scoring.partial_credit", "scoring.minor_penalty", "scoring.minor_penalty_cap",
	"scoring.critical_penalty", "scoring.critical_score_cap", "scoring.clamp_min",
	"scoring.clamp_max", "scoring.hint_divergence_threshold", "scoring.low_confidence_threshold",
}

// Evaluate menjalankan Evaluator dan menghitung skor final. Severity selalu
// diambil dari database, bukan dari output model (Requirement 14.8).
func (s *Service) Evaluate(ctx context.Context, task taskqueue.Task) (Result, error) {
	var p Payload
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		return Result{}, fmt.Errorf("evaluator: payload: %w", err)
	}
	contract, ok := s.contracts["answer_evaluation"]
	if !ok {
		return Result{}, fmt.Errorf("evaluator: kontrak answer_evaluation tidak ditemukan")
	}

	concepts, err := s.concepts(ctx, p.QuestionID)
	if err != nil {
		return Result{}, err
	}
	catalog, severity, err := s.catalog(ctx, p.TopicID)
	if err != nil {
		return Result{}, err
	}
	detected, err := s.detectedCodes(ctx, p.UserID, p.TopicID)
	if err != nil {
		return Result{}, err
	}
	conceptCodes := make([]string, 0, len(concepts))
	dataConcepts := make([]map[string]any, 0, len(concepts))
	for _, c := range concepts {
		conceptCodes = append(conceptCodes, c.Code)
		dataConcepts = append(dataConcepts, map[string]any{
			"code": c.Code, "weight": c.Weight, "is_required": c.IsRequired,
		})
	}
	data := map[string]any{
		"Question":   p.QuestionID.String(),
		"Concepts":   dataConcepts,
		"Locale":     p.Locale,
		"Catalog":    catalog,
		"Detected":   detected,
		"AnswerText": p.AnswerText,
	}

	gr, err := s.gateway.Run(ctx, gateway.Input{
		Task: task, Contract: contract, Data: data, LearnerData: p.AnswerText,
		AllowedConcepts: conceptCodes, CatalogCodes: catalog, DetectedCodes: detected,
	})
	if err != nil {
		return Result{}, err
	}

	snap, err := s.settings.Snapshot(ctx, scoringKeys...)
	if err != nil {
		return Result{}, err
	}
	params := evaluation.ScoringParams{
		PartialCredit:           snap.Float("scoring.partial_credit"),
		MinorPenalty:            snap.Float("scoring.minor_penalty"),
		MinorPenaltyCap:         snap.Float("scoring.minor_penalty_cap"),
		CriticalPenalty:         snap.Float("scoring.critical_penalty"),
		CriticalScoreCap:        snap.Float("scoring.critical_score_cap"),
		ClampMin:                snap.Float("scoring.clamp_min"),
		ClampMax:                snap.Float("scoring.clamp_max"),
		HintDivergenceThreshold: snap.Float("scoring.hint_divergence_threshold"),
		LowConfidenceThreshold:  snap.Float("scoring.low_confidence_threshold"),
	}
	score := evaluation.ScoreAnswer(concepts, gr.Signal, severity, params)
	return Result{Gateway: gr, Score: score, Severity: severity, RawOutput: gr.Raw}, nil
}

type conceptRow struct {
	Code        string
	Description string
	Weight      float64
	IsRequired  bool
}

func (s *Service) concepts(ctx context.Context, questionID uuid.UUID) ([]evaluation.ConceptRef, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.code, COALESCE(ct.description, ''), qc.weight, qc.is_required
		FROM question_concepts qc JOIN concepts c ON c.id = qc.concept_id
		LEFT JOIN concept_translations ct ON ct.concept_id = c.id AND ct.status = 'published'
		WHERE qc.question_id = $1 ORDER BY c.code`, questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]evaluation.ConceptRef, 0)
	for rows.Next() {
		var c conceptRow
		if err := rows.Scan(&c.Code, &c.Description, &c.Weight, &c.IsRequired); err != nil {
			return nil, err
		}
		out = append(out, evaluation.ConceptRef{Code: c.Code, Weight: c.Weight, IsRequired: c.IsRequired})
	}
	return out, rows.Err()
}

func (s *Service) catalog(ctx context.Context, topicID uuid.UUID) ([]string, map[string]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT code, severity FROM misconceptions WHERE topic_id = $1 AND is_active ORDER BY code`, topicID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	codes := make([]string, 0)
	severity := map[string]string{}
	for rows.Next() {
		var code, sev string
		if err := rows.Scan(&code, &sev); err != nil {
			return nil, nil, err
		}
		codes = append(codes, code)
		severity[code] = sev // Dipakai server, tidak dikirim ke prompt.
	}
	return codes, severity, rows.Err()
}

func (s *Service) detectedCodes(ctx context.Context, userID, topicID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT m.code FROM user_misconceptions um
		JOIN misconceptions m ON m.id = um.misconception_id
		WHERE um.user_id = $1 AND m.topic_id = $2 AND um.status = 'detected' ORDER BY m.code`, userID, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

// PromptCatalog mengembalikan katalog tanpa severity untuk disisipkan ke prompt.
func PromptCatalog(codes []string) []map[string]any {
	out := make([]map[string]any, 0, len(codes))
	for _, c := range codes {
		out = append(out, map[string]any{"code": c})
	}
	return out
}
