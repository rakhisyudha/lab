// Package golden mengelola Golden_Set, metrik regresi, dan gerbang aktivasi.
package golden

import (
	"time"

	"github.com/google/uuid"
)

// Kategori kasus Golden_Set (Requirement 17.2).
const (
	CategoryDifferentWording      = "different_wording"
	CategoryPartiallyCorrect      = "partially_correct"
	CategoryCriticalMisconception = "critical_misconception"
	CategoryOffTopicOrEmpty       = "off_topic_or_empty"
	CategoryAdversarialInjection  = "adversarial_injection"
)

var Categories = []string{
	CategoryDifferentWording, CategoryPartiallyCorrect, CategoryCriticalMisconception,
	CategoryOffTopicOrEmpty, CategoryAdversarialInjection,
}

// Case adalah kasus aktif yang dijalankan Golden_Set_Runner.
type Case struct {
	ID                     uuid.UUID
	QuestionID             uuid.UUID
	Locale                 string
	SampleAnswer           string
	Category               string
	ExpectedScoreMin       float64
	ExpectedScoreMax       float64
	ExpectedFound          []string
	ExpectedMissed         []string
	ExpectedMisconceptions []string
}

// CaseResult adalah hasil satu kasus.
type CaseResult struct {
	CaseID               uuid.UUID
	SchemaValid          bool
	CatalogViolation     bool
	InjectionObeyed      bool
	ActualScore          float64
	ActualFound          []string
	ActualMissed         []string
	ActualMisconceptions []string
}

// Metrics adalah metrik agregat satu run (Requirement 17.7).
type Metrics struct {
	ScoreInRangeRate        float64
	ConceptF1               float64
	CriticalRecall          float64
	FalseCriticalRate       float64
	InjectionResilienceRate float64
	CatalogViolationCount   int
	SchemaViolationCount    int
}

// ComputeGoldenMetrics menghitung seluruh metrik menurut design 4.13.
func ComputeGoldenMetrics(cases []Case, results map[uuid.UUID]CaseResult, severityOf map[string]string) Metrics {
	n := len(cases)
	if n == 0 {
		return Metrics{ConceptF1: 1, CriticalRecall: 1, InjectionResilienceRate: 1}
	}
	m := Metrics{}
	inRange := 0
	tp, fp, fn := 0, 0, 0
	expCrit, foundCrit := 0, 0
	neg, falseCrit := 0, 0
	adv, resilient := 0, 0

	for _, c := range cases {
		res, ok := results[c.ID]
		valid := ok && res.SchemaValid && !res.CatalogViolation
		if valid && res.ActualScore >= c.ExpectedScoreMin && res.ActualScore <= c.ExpectedScoreMax {
			inRange++
		}
		if res.CatalogViolation {
			m.CatalogViolationCount++
		}
		if ok && !res.SchemaValid {
			m.SchemaViolationCount++
		}

		foundSet := toSet(res.ActualFound)
		if valid {
			for _, code := range c.ExpectedFound {
				if foundSet[code] {
					tp++
				} else {
					fn++
				}
			}
			for _, code := range c.ExpectedMissed {
				if foundSet[code] {
					fp++
				}
			}
		} else {
			fn += len(c.ExpectedFound)
		}

		hasExpectedCritical := false
		for _, code := range c.ExpectedMisconceptions {
			if severityOf[code] == "critical" {
				hasExpectedCritical = true
				expCrit++
				if valid && has(res.ActualMisconceptions, code) {
					foundCrit++
				}
			}
		}
		if !hasExpectedCritical {
			neg++
			if valid {
				for _, code := range res.ActualMisconceptions {
					if severityOf[code] == "critical" {
						falseCrit++
						break
					}
				}
			}
		}
		if c.Category == CategoryAdversarialInjection {
			adv++
			if ok && res.SchemaValid && !res.InjectionObeyed {
				resilient++
			}
		}
	}

	m.ScoreInRangeRate = float64(inRange) / float64(n)
	denom := 2*tp + fp + fn
	if denom == 0 {
		m.ConceptF1 = 1
	} else {
		m.ConceptF1 = float64(2*tp) / float64(denom)
	}
	if expCrit == 0 {
		m.CriticalRecall = 1
	} else {
		m.CriticalRecall = float64(foundCrit) / float64(expCrit)
	}
	if neg == 0 {
		m.FalseCriticalRate = 0
	} else {
		m.FalseCriticalRate = float64(falseCrit) / float64(neg)
	}
	if adv == 0 {
		m.InjectionResilienceRate = 1
	} else {
		m.InjectionResilienceRate = float64(resilient) / float64(adv)
	}
	return m
}

func toSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, v := range items {
		out[v] = true
	}
	return out
}

func has(items []string, want string) bool {
	for _, v := range items {
		if v == want {
			return true
		}
	}
	return false
}

// GateThresholds adalah ambang lulus dari App_Settings.
type GateThresholds struct {
	ScoreInRangeRate        float64
	ConceptF1               float64
	CriticalRecall          float64
	FalseCriticalRate       float64
	InjectionResilienceRate float64
}

// Baseline adalah ringkasan run live yang disetujui sebagai baseline regresi.
type Baseline struct {
	Status  string
	Metrics Metrics
}

// GateResult adalah hasil evaluasi gerbang.
type GateResult struct {
	Status    string
	Reasons   []string
	Regressed []string
}

// EvaluateGate menerapkan coverage, ambang, dan toleransi regresi (design 4.13).
func EvaluateGate(m Metrics, coverage map[string]int, t GateThresholds, minCases int, baseline *Baseline, tolerance float64) GateResult {
	for _, category := range Categories {
		if coverage[category] < minCases {
			return GateResult{Status: "insufficient_coverage", Reasons: []string{"coverage:" + category}}
		}
	}
	var reasons []string
	if m.ScoreInRangeRate < t.ScoreInRangeRate {
		reasons = append(reasons, "score_in_range_rate")
	}
	if m.ConceptF1 < t.ConceptF1 {
		reasons = append(reasons, "concept_f1")
	}
	if m.CriticalRecall < t.CriticalRecall {
		reasons = append(reasons, "critical_recall")
	}
	if m.FalseCriticalRate > t.FalseCriticalRate {
		reasons = append(reasons, "false_critical_rate")
	}
	if m.InjectionResilienceRate < t.InjectionResilienceRate {
		reasons = append(reasons, "injection_resilience_rate")
	}
	if m.CatalogViolationCount != 0 {
		reasons = append(reasons, "catalog_violation_count")
	}
	if m.SchemaViolationCount != 0 {
		reasons = append(reasons, "schema_violation_count")
	}

	var regressed []string
	if baseline != nil && baseline.Status == "passed" {
		b := baseline.Metrics
		const eps = 1e-9
		if m.ScoreInRangeRate < b.ScoreInRangeRate-tolerance-eps {
			regressed = append(regressed, "score_in_range_rate")
		}
		if m.ConceptF1 < b.ConceptF1-tolerance-eps {
			regressed = append(regressed, "concept_f1")
		}
		if m.CriticalRecall < b.CriticalRecall-tolerance-eps {
			regressed = append(regressed, "critical_recall")
		}
		if m.InjectionResilienceRate < b.InjectionResilienceRate-tolerance-eps {
			regressed = append(regressed, "injection_resilience_rate")
		}
		if m.FalseCriticalRate > b.FalseCriticalRate+tolerance+eps {
			regressed = append(regressed, "false_critical_rate")
		}
	}
	if len(reasons) > 0 {
		return GateResult{Status: "failed", Reasons: reasons, Regressed: regressed}
	}
	if len(regressed) > 0 {
		return GateResult{Status: "failed", Reasons: []string{"regression_detected"}, Regressed: regressed}
	}
	return GateResult{Status: "passed"}
}

// RunInfo adalah ringkasan minimal Golden_Run untuk pemilihan baseline.
type RunInfo struct {
	ID         uuid.UUID
	RunMode    string
	Status     string
	StartedAt  time.Time
	ContractID uuid.UUID
	Metrics    Metrics
}

// SelectBaseline memilih run live terbaru berstatus passed untuk kontrak given.
// Run recorded tidak pernah menjadi baseline (Requirement 17.15).
func SelectBaseline(runs []RunInfo, contractID uuid.UUID) *Baseline {
	var best *RunInfo
	for i := range runs {
		r := runs[i]
		if r.RunMode != "live" || r.Status != "passed" || r.ContractID != contractID {
			continue
		}
		if best == nil || r.StartedAt.After(best.StartedAt) {
			best = &runs[i]
		}
	}
	if best == nil {
		return nil
	}
	return &Baseline{Status: best.Status, Metrics: best.Metrics}
}
