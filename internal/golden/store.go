package golden

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store mengakses tabel golden_cases, golden_runs, golden_run_results, dan golden_recordings.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var ErrNotFound = errors.New("golden: tidak ditemukan")

const caseColumns = `id, question_id, locale, category, sample_answer, expected_score_min,
	expected_score_max, expected_found_concept_codes, expected_missed_concept_codes,
	expected_misconception_codes, is_active`

func scanCase(row interface{ Scan(...any) error }) (Case, error) {
	var c Case
	var qid uuid.UUID
	var min, max sql.NullFloat64
	var found, missed, miscon []string
	err := row.Scan(&c.ID, &qid, &sql.NullString{}, &c.Category, &sql.NullString{}, &min, &max,
		&found, &missed, &miscon, &sql.NullBool{})
	if err != nil {
		return Case{}, err
	}
	c.ExpectedScoreMin = min.Float64
	c.ExpectedScoreMax = max.Float64
	c.ExpectedFound, c.ExpectedMissed, c.ExpectedMisconceptions = found, missed, miscon
	return c, nil
}

// ActiveCases mengembalikan seluruh kasus aktif.
func (s *Store) ActiveCases(ctx context.Context) ([]Case, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, question_id, locale, sample_answer, category, expected_score_min, expected_score_max,
		expected_found_concept_codes, expected_missed_concept_codes, expected_misconception_codes
		FROM golden_cases WHERE is_active ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Case, 0)
	for rows.Next() {
		var c Case
		var min, max sql.NullFloat64
		var found, missed, miscon []string
		if err := rows.Scan(&c.ID, &c.QuestionID, &c.Locale, &c.SampleAnswer, &c.Category,
			&min, &max, &found, &missed, &miscon); err != nil {
			return nil, err
		}
		c.ExpectedScoreMin, c.ExpectedScoreMax = min.Float64, max.Float64
		c.ExpectedFound, c.ExpectedMissed, c.ExpectedMisconceptions = found, missed, miscon
		out = append(out, c)
	}
	return out, rows.Err()
}

// Coverage menghitung jumlah kasus aktif per kategori.
func (s *Store) Coverage(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT category, count(*) FROM golden_cases WHERE is_active GROUP BY category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var category string
		var n int
		if err := rows.Scan(&category, &n); err != nil {
			return nil, err
		}
		out[category] = n
	}
	return out, rows.Err()
}

// SeverityOf mengambil peta kode Misconception -> severity.
func (s *Store) SeverityOf(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT code, severity FROM misconceptions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var code, sev string
		if err := rows.Scan(&code, &sev); err != nil {
			return nil, err
		}
		out[code] = sev
	}
	return out, rows.Err()
}

// CaseLabels mengembalikan katalog, concept, dan locale published untuk pertanyaan.
func (s *Store) CaseLabels(ctx context.Context, questionID uuid.UUID) (catalog, concepts, locales []string, topicID uuid.UUID, err error) {
	if err = s.pool.QueryRow(ctx, `SELECT topic_id FROM questions WHERE id = $1`, questionID).Scan(&topicID); err != nil {
		return nil, nil, nil, uuid.Nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT code FROM misconceptions WHERE topic_id = $1 AND is_active`, topicID)
	if err != nil {
		return nil, nil, nil, uuid.Nil, err
	}
	catalog, err = scanStrings(rows)
	if err != nil {
		return nil, nil, nil, uuid.Nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT c.code FROM question_concepts qc JOIN concepts c ON c.id = qc.concept_id WHERE qc.question_id = $1`, questionID)
	if err != nil {
		return nil, nil, nil, uuid.Nil, err
	}
	concepts, err = scanStrings(rows)
	if err != nil {
		return nil, nil, nil, uuid.Nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT locale FROM question_translations WHERE question_id = $1 AND status = 'published'`, questionID)
	if err != nil {
		return nil, nil, nil, uuid.Nil, err
	}
	locales, err = scanStrings(rows)
	return catalog, concepts, locales, topicID, err
}

func scanStrings(rows pgx.Rows) ([]string, error) {
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CreateCase menyimpan kasus baru.
func (s *Store) CreateCase(ctx context.Context, in CaseInput, ownerID uuid.UUID) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO golden_cases
		(id, question_id, locale, category, sample_answer, expected_score_min, expected_score_max,
		 expected_found_concept_codes, expected_missed_concept_codes, expected_misconception_codes, owner_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		id, in.QuestionID, in.Locale, in.Category, in.SampleAnswer, in.ExpectedScoreMin,
		in.ExpectedScoreMax, in.ExpectedFound, in.ExpectedMissed, in.ExpectedMisconceptions, ownerID)
	return id, err
}

// ListCases mengembalikan daftar kasus untuk admin.
func (s *Store) ListCases(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+caseColumns+` FROM golden_cases ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, qid uuid.UUID
		var locale, category, answer string
		var min, max float64
		var found, missed, miscon []string
		var active bool
		if err := rows.Scan(&id, &qid, &locale, &category, &answer, &min, &max, &found, &missed, &miscon, &active); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id.String(), "question_id": qid.String(), "locale": locale, "category": category,
			"sample_answer": answer, "expected_score_min": min, "expected_score_max": max,
			"expected_found_concept_codes": found, "expected_missed_concept_codes": missed,
			"expected_misconception_codes": miscon, "is_active": active,
		})
	}
	return out, rows.Err()
}

// UpdateCase memperbarui field kasus yang diizinkan.
func (s *Store) UpdateCase(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	sets := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	i := 1
	for _, key := range []string{"sample_answer", "expected_score_min", "expected_score_max", "is_active"} {
		if v, ok := fields[key]; ok {
			sets = append(sets, fmt.Sprintf("%s = $%d", key, i))
			args = append(args, v)
			i++
		}
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(`UPDATE golden_cases SET %s WHERE id = $%d`,
		strings.Join(sets, ", "), i), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCase menonaktifkan kasus (bukan hard delete).
func (s *Store) DeleteCase(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE golden_cases SET is_active = false WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateRun menyimpan satu Golden_Run beserta hasil dan metriknya.
func (s *Store) CreateRun(ctx context.Context, contractID, baselineID, triggeredBy *uuid.UUID,
	mode, model, trigger string, metrics Metrics, gate GateResult, results map[uuid.UUID]CaseResult,
	coverage map[string]int, reasons []string) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	status := gate.Status
	if len(reasons) > 0 && status == "passed" {
		status = "errored"
	}
	metricsJSON, _ := json.Marshal(metrics)
	thresholdsJSON, _ := json.Marshal(map[string]any{
		"score_in_range_rate": metrics.ScoreInRangeRate, "concept_f1": metrics.ConceptF1,
	})
	coverageJSON, _ := json.Marshal(coverage)
	regressionJSON, _ := json.Marshal(gate.Regressed)
	err = dbTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO golden_runs
			(id, prompt_contract_id, run_mode, baseline_run_id, model_name, status, error_reasons,
			 trigger_source, metrics, thresholds, coverage, regression, triggered_by, finished_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, now())`,
			id, contractID, mode, baselineID, model, status, reasons, trigger,
			metricsJSON, thresholdsJSON, coverageJSON, regressionJSON, triggeredBy)
		if err != nil {
			return err
		}
		for caseID, r := range results {
			raw, _ := json.Marshal(r)
			_, err := tx.Exec(ctx, `INSERT INTO golden_run_results
				(id, golden_run_id, golden_case_id, schema_valid, catalog_violation, actual_score,
				 actual_found_codes, actual_missed_codes, actual_misconception_codes, injection_obeyed, checks, passed, raw_output)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
				uuid.Must(uuid.NewV7()), id, caseID, r.SchemaValid, r.CatalogViolation, r.ActualScore,
				r.ActualFound, r.ActualMissed, r.ActualMisconceptions, r.InjectionObeyed,
				[]byte("{}"), true, raw)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// ListRuns mengembalikan daftar Golden_Run.
func (s *Store) ListRuns(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, prompt_contract_id, run_mode, model_name, status,
		trigger_source, metrics, started_at FROM golden_runs ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, cid uuid.UUID
		var mode, model, status, trigger string
		var metrics []byte
		var started sql.NullTime
		if err := rows.Scan(&id, &cid, &mode, &model, &status, &trigger, &metrics, &started); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id.String(), "prompt_contract_id": cid.String(), "run_mode": mode,
			"model_name": model, "status": status, "trigger_source": trigger,
			"metrics": json.RawMessage(metrics), "started_at": started.Time,
		})
	}
	return out, rows.Err()
}

// Recording adalah rekaman response model untuk satu kasus.
type Recording struct {
	Responses []providerResponse `json:"responses"`
	CaseSHA   string
}

type providerResponse struct {
	RawText      string `json:"raw_text"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// GetRecordingByCase mengambil rekaman pertama yang tersedia untuk satu kasus.
func (s *Store) GetRecordingByCase(ctx context.Context, caseID uuid.UUID) (Recording, error) {
	var raw []byte
	var sha string
	err := s.pool.QueryRow(ctx, `SELECT responses, case_sha256 FROM golden_recordings
		WHERE golden_case_id = $1 ORDER BY recorded_at DESC LIMIT 1`, caseID).Scan(&raw, &sha)
	if errors.Is(err, pgx.ErrNoRows) {
		return Recording{}, ErrNotFound
	}
	if err != nil {
		return Recording{}, err
	}
	var rec Recording
	if err := json.Unmarshal(raw, &rec.Responses); err != nil {
		return Recording{}, err
	}
	rec.CaseSHA = sha
	return rec, nil
}

// CaseLabelsForCase mengembalikan katalog, concept, dan kode detected untuk satu kasus.
func (s *Store) CaseLabelsForCase(ctx context.Context, caseID uuid.UUID) (concepts, catalog, detected []string, err error) {
	var questionID, topicID uuid.UUID
	if err = s.pool.QueryRow(ctx, `SELECT question_id FROM golden_cases WHERE id = $1`, caseID).Scan(&questionID); err != nil {
		return nil, nil, nil, err
	}
	catalog, concepts, _, topicID, err = s.CaseLabels(ctx, questionID)
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT m.code FROM user_misconceptions um
		JOIN misconceptions m ON m.id = um.misconception_id
		WHERE um.user_id = (SELECT owner_id FROM golden_cases WHERE id = $1)
		  AND m.topic_id = $2 AND um.status = 'detected'`, caseID, topicID)
	if err != nil {
		return nil, nil, nil, err
	}
	detected, err = scanStrings(rows)
	return concepts, catalog, detected, err
}

// GetRecording mengambil rekaman untuk pasangan kontrak, model, dan kasus.
func (s *Store) GetRecording(ctx context.Context, contractID uuid.UUID, model string, caseID uuid.UUID) (Recording, error) {
	var raw []byte
	var sha string
	err := s.pool.QueryRow(ctx, `SELECT responses, case_sha256 FROM golden_recordings
		WHERE prompt_contract_id = $1 AND model_name = $2 AND golden_case_id = $3`,
		contractID, model, caseID).Scan(&raw, &sha)
	if errors.Is(err, pgx.ErrNoRows) {
		return Recording{}, ErrNotFound
	}
	if err != nil {
		return Recording{}, err
	}
	var rec Recording
	if err := json.Unmarshal(raw, &rec.Responses); err != nil {
		return Recording{}, err
	}
	rec.CaseSHA = sha
	return rec, nil
}

func (s *Store) RunInfo(ctx context.Context, contractID uuid.UUID) ([]RunInfo, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, prompt_contract_id, run_mode, status, started_at, metrics
		FROM golden_runs WHERE prompt_contract_id = $1`, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RunInfo, 0)
	for rows.Next() {
		var r RunInfo
		var mode, status string
		var started sql.NullTime
		var raw []byte
		if err := rows.Scan(&r.ID, &r.ContractID, &mode, &status, &started, &raw); err != nil {
			return nil, err
		}
		r.RunMode, r.Status, r.StartedAt = mode, status, started.Time
		_ = json.Unmarshal(raw, &r.Metrics)
		out = append(out, r)
	}
	return out, rows.Err()
}

func dbTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ = fmt.Sprintf
