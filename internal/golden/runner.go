package golden

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/ai/schema"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

type SettingsReader interface {
	Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
}

// EvaluatorLive menilai satu kasus dengan pipeline produksi (mode live).
type EvaluatorLive func(ctx context.Context, c Case) (schema.Output, evaluation.AnswerScore, error)

// Runner menjalankan Golden_Set dalam mode recorded atau live.
type Runner struct {
	store    *Store
	settings SettingsReader
	clock    clock.Clock
	live     EvaluatorLive
}

func NewRunner(store *Store, s SettingsReader, c clock.Clock, live EvaluatorLive) *Runner {
	if c == nil {
		c = clock.System{}
	}
	return &Runner{store: store, settings: s, clock: c, live: live}
}

var gateKeys = []string{
	"golden.threshold.score_in_range_rate", "golden.threshold.concept_f1",
	"golden.threshold.critical_recall", "golden.threshold.false_critical_rate",
	"golden.threshold.injection_resilience_rate", "golden.regression_tolerance",
	"golden.min_cases_per_category",
}

// Run menjalankan satu Golden_Run dan mengembalikan id-nya.
func (r *Runner) Run(ctx context.Context, contractID uuid.UUID, model, mode, trigger string) (uuid.UUID, error) {
	if mode != "recorded" && mode != "live" {
		return uuid.Nil, fmt.Errorf("golden: run_mode %q tidak dikenal", mode)
	}
	cases, err := r.store.ActiveCases(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	severity, err := r.store.SeverityOf(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	params, err := r.scoringParams(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	results := map[uuid.UUID]CaseResult{}
	var reasons []string
	for _, c := range cases {
		var out schema.Output
		var score evaluation.AnswerScore
		switch mode {
		case "recorded":
			out, score, err = r.recorded(ctx, c, severity, params)
		case "live":
			if r.live == nil {
				return uuid.Nil, fmt.Errorf("golden: evaluator live tidak tersedia")
			}
			out, score, err = r.live(ctx, c)
		}
		if err != nil {
			reasons = append(reasons, err.Error())
			continue
		}
		found := make([]string, 0, len(out.ConceptResults))
		for _, cr := range out.ConceptResults {
			if cr.Coverage == "found" {
				found = append(found, cr.ConceptCode)
			}
		}
		results[c.ID] = CaseResult{
			CaseID: c.ID, SchemaValid: true, CatalogViolation: false,
			InjectionObeyed: out.InjectionSuspected, ActualScore: score.ScorePct,
			ActualFound: found, ActualMissed: c.ExpectedMissed,
			ActualMisconceptions: intersect(out.MisconceptionCodes, c.ExpectedMisconceptions),
		}
	}
	if len(reasons) > 0 {
		metrics := ComputeGoldenMetrics(cases, results, severity)
		return r.store.CreateRun(ctx, &contractID, nil, nil, mode, model, trigger, metrics,
			GateResult{Status: "errored"}, results, map[string]int{}, reasons)
	}

	metrics := ComputeGoldenMetrics(cases, results, severity)
	coverage, err := r.store.Coverage(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	snap, err := r.settings.Snapshot(ctx, gateKeys...)
	if err != nil {
		return uuid.Nil, err
	}
	thresholds := GateThresholds{
		ScoreInRangeRate:        snap.Float("golden.threshold.score_in_range_rate"),
		ConceptF1:               snap.Float("golden.threshold.concept_f1"),
		CriticalRecall:          snap.Float("golden.threshold.critical_recall"),
		FalseCriticalRate:       snap.Float("golden.threshold.false_critical_rate"),
		InjectionResilienceRate: snap.Float("golden.threshold.injection_resilience_rate"),
	}
	minCases := int(snap.Int("golden.min_cases_per_category"))
	if minCases <= 0 {
		minCases = 5
	}
	tolerance := snap.Float("golden.regression_tolerance")
	runs, err := r.store.RunInfo(ctx, contractID)
	if err != nil {
		return uuid.Nil, err
	}
	var baselineRunID *uuid.UUID
	var baseline *Baseline
	if mode == "live" {
		if b := SelectBaseline(runs, contractID); b != nil {
			baseline = b
			// Cari id run baseline terbaru.
			for _, info := range runs {
				if info.Status == "passed" && info.RunMode == "live" && info.ContractID == contractID {
					id := info.ID
					baselineRunID = &id
					break
				}
			}
		}
	}
	gate := EvaluateGate(metrics, coverage, thresholds, minCases, baseline, tolerance)
	return r.store.CreateRun(ctx, &contractID, baselineRunID, nil, mode, model, trigger, metrics, gate, results, coverage, nil)
}

func (r *Runner) recorded(ctx context.Context, c Case, severity map[string]string, params evaluation.ScoringParams) (schema.Output, evaluation.AnswerScore, error) {
	// Model tidak disimpan pada run; gunakan rekaman apa adanya dari pasangan kontrak/model.
	rec, err := r.store.GetRecordingByCase(ctx, c.ID)
	if err != nil {
		return schema.Output{}, evaluation.AnswerScore{}, fmt.Errorf("recording_missing:%s", c.ID)
	}
	if len(rec.Responses) == 0 {
		return schema.Output{}, evaluation.AnswerScore{}, fmt.Errorf("recording_missing:%s", c.ID)
	}
	concepts, catalog, detected, err := r.store.CaseLabelsForCase(ctx, c.ID)
	if err != nil {
		return schema.Output{}, evaluation.AnswerScore{}, err
	}
	var lastErr error
	for _, resp := range rec.Responses {
		out, err := schema.ValidateEvaluatorOutput(json.RawMessage(resp.RawText), concepts, catalog, detected)
		if err != nil {
			lastErr = err
			continue
		}
		refs := make([]evaluation.ConceptRef, 0, len(concepts))
		for _, code := range concepts {
			refs = append(refs, evaluation.ConceptRef{Code: code, Weight: 1})
		}
		score := evaluation.ScoreAnswer(refs, toSignal(out), severity, params)
		return out, score, nil
	}
	if lastErr != nil {
		return schema.Output{}, evaluation.AnswerScore{}, fmt.Errorf("validation_failed:%s", c.ID)
	}
	return schema.Output{}, evaluation.AnswerScore{}, fmt.Errorf("recording_stale:%s", c.ID)
}

func (r *Runner) scoringParams(ctx context.Context) (evaluation.ScoringParams, error) {
	snap, err := r.settings.Snapshot(ctx,
		"scoring.partial_credit", "scoring.minor_penalty", "scoring.minor_penalty_cap",
		"scoring.critical_penalty", "scoring.critical_score_cap", "scoring.clamp_min",
		"scoring.clamp_max", "scoring.hint_divergence_threshold", "scoring.low_confidence_threshold")
	if err != nil {
		return evaluation.ScoringParams{}, err
	}
	return evaluation.ScoringParams{
		PartialCredit: snap.Float("scoring.partial_credit"), MinorPenalty: snap.Float("scoring.minor_penalty"),
		MinorPenaltyCap: snap.Float("scoring.minor_penalty_cap"), CriticalPenalty: snap.Float("scoring.critical_penalty"),
		CriticalScoreCap: snap.Float("scoring.critical_score_cap"), ClampMin: snap.Float("scoring.clamp_min"),
		ClampMax: snap.Float("scoring.clamp_max"), HintDivergenceThreshold: snap.Float("scoring.hint_divergence_threshold"),
		LowConfidenceThreshold: snap.Float("scoring.low_confidence_threshold"),
	}, nil
}

func toSignal(out schema.Output) evaluation.EvaluatorSignal {
	concepts := make([]evaluation.ConceptCoverage, 0, len(out.ConceptResults))
	for _, c := range out.ConceptResults {
		concepts = append(concepts, evaluation.ConceptCoverage{ConceptCode: c.ConceptCode, Coverage: c.Coverage, Evidence: c.Evidence})
	}
	return evaluation.EvaluatorSignal{
		ConceptResults: concepts, MisconceptionCodes: out.MisconceptionCodes,
		ResolvedMisconceptionCodes: out.ResolvedMisconceptionCodes, Relevance: out.Relevance,
		ScoreHint: out.ScoreHint, Confidence: out.Confidence, Feedback: out.Feedback,
	}
}

func intersect(a, b []string) []string {
	set := map[string]bool{}
	for _, v := range b {
		set[v] = true
	}
	out := make([]string, 0)
	for _, v := range a {
		if set[v] {
			out = append(out, v)
		}
	}
	return out
}
