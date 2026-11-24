package evaluation

import (
	"encoding/json"
	"sort"
	"time"
)

// Attempt adalah satu catatan Attempt yang sudah selesai dan masuk ke
// perhitungan mastery (hanya berasal dari jawaban scored).
type Attempt struct {
	ID                     string
	ScorePct               float64
	HasApplicationQuestion bool
	ScoredAt               time.Time
}

// MisconceptionFlag adalah catatan miskonsepsi pengguna untuk satu topik.
// Severity: minor | critical; Status: detected | resolved.
type MisconceptionFlag struct {
	Code, Severity, Status string
}

// MasteryParams memuat semua ambang mastery yang dibaca dari App_Settings
// (desain 4.4).
type MasteryParams struct {
	MasteredThreshold        float64 // 80
	MinAttempts              int     // 3
	WindowSize               int     // 3
	NeedsReviewFloor         float64 // 50
	ApplicationEvidenceScope string  // "window" | "all"
	AllInitial               bool
}

// MasteryResult adalah hasil ComputeMastery. AvgLast3 dan BestScore nil
// bila tidak ada Attempt. Evidence selalu berupa JSON objek yang valid.
type MasteryResult struct {
	Status       string // NOT_STARTED | IN_PROGRESS | PRACTICING | NEEDS_REVIEW | MASTERED
	Evidence     json.RawMessage
	AvgLast3     *float64
	BestScore    *float64
	EverMastered bool
}

// Struktur evidence sesuai desain 4.4. Deklarasi struct (bukan map) dipakai
// agar urutan kunci JSON stabil sehingga hasil mudah dibandingkan.
type buktiMastery struct {
	AttemptIDs            []string              `json:"attempt_ids"`
	ScoreWindow           []float64             `json:"score_window"`
	AvgLast3              *float64              `json:"avg_last3"`
	ApplicationAttemptIDs []string              `json:"application_attempt_ids"`
	MisconceptionFlags    []flagMasteryBukti    `json:"misconception_flags"`
	BlockingReasons       []string              `json:"blocking_reasons"`
	EverMastered          bool                  `json:"ever_mastered"`
	Parameters            parameterMasteryBukti `json:"parameters"`
}

type flagMasteryBukti struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Status   string `json:"status"`
}

type parameterMasteryBukti struct {
	MasteredThreshold        float64 `json:"mastered_threshold"`
	MinAttempts              int     `json:"min_attempts"`
	WindowSize               int     `json:"window_size"`
	NeedsReviewFloor         float64 `json:"needs_review_floor"`
	ApplicationEvidenceScope string  `json:"application_evidence_scope"`
	IsInitialValue           bool    `json:"is_initial_value"`
}

// ComputeMastery menetapkan status mastery secara murni dan deterministik
// (desain 4.4). Attempt diurutkan menurut (ScoredAt ASC, ID ASC) pada salinan
// masukan, sehingga urutan penyimpanan tidak memengaruhi hasil dan slice
// pemanggil tidak diubah. everMastered bersifat monoton.
func ComputeMastery(attempts []Attempt, misconceptions []MisconceptionFlag, everMastered bool, P MasteryParams) MasteryResult {
	urutan := urutkanAttemptMastery(attempts)
	n := len(urutan)

	// Salinan flag diurutkan berdasarkan kode, lalu severity dan status agar
	// hasil tetap deterministik walaupun ada flag dengan kode yang sama.
	flags := make([]flagMasteryBukti, 0, len(misconceptions))
	for _, m := range misconceptions {
		flags = append(flags, flagMasteryBukti{Code: m.Code, Severity: m.Severity, Status: m.Status})
	}
	sort.Slice(flags, func(i, j int) bool {
		if flags[i].Code != flags[j].Code {
			return flags[i].Code < flags[j].Code
		}
		if flags[i].Severity != flags[j].Severity {
			return flags[i].Severity < flags[j].Severity
		}
		return flags[i].Status < flags[j].Status
	})

	parameter := parameterMasteryBukti{
		MasteredThreshold:        P.MasteredThreshold,
		MinAttempts:              P.MinAttempts,
		WindowSize:               P.WindowSize,
		NeedsReviewFloor:         P.NeedsReviewFloor,
		ApplicationEvidenceScope: P.ApplicationEvidenceScope,
		IsInitialValue:           P.AllInitial,
	}

	// Tanpa Attempt, status NOT_STARTED dan kedua skor bernilai nol (nil).
	if n == 0 {
		bukti := buktiMastery{
			AttemptIDs:            []string{},
			ScoreWindow:           []float64{},
			ApplicationAttemptIDs: []string{},
			MisconceptionFlags:    flags,
			BlockingReasons:       []string{"attempts_below_min"},
			EverMastered:          everMastered,
			Parameters:            parameter,
		}
		encoded, _ := json.Marshal(bukti)
		return MasteryResult{Status: "NOT_STARTED", Evidence: encoded, EverMastered: everMastered}
	}

	mulai := 0
	if P.WindowSize < n {
		mulai = n - P.WindowSize
	}
	if mulai < 0 {
		mulai = 0
	}
	jendela := urutan[mulai:]

	total := 0.0
	for _, a := range jendela {
		total += a.ScorePct
	}
	var avg3 *float64
	if len(jendela) > 0 {
		nilai := round2(total / float64(len(jendela)))
		avg3 = &nilai
	}
	terbaik := urutan[0].ScorePct
	for _, a := range urutan[1:] {
		if a.ScorePct > terbaik {
			terbaik = a.ScorePct
		}
	}
	best := &terbaik

	banyakKritis := 0
	for _, m := range misconceptions {
		if m.Severity == "critical" && m.Status == "detected" {
			banyakKritis++
		}
	}

	cakupanAplikasi := urutan
	if P.ApplicationEvidenceScope == "window" {
		cakupanAplikasi = jendela
	}

	adaAplikasi := false
	for _, a := range cakupanAplikasi {
		if a.HasApplicationQuestion {
			adaAplikasi = true
			break
		}
	}

	blocking := make([]string, 0, 4)
	if n < P.MinAttempts {
		blocking = append(blocking, "attempts_below_min")
	} else if avg3 != nil && *avg3 < P.MasteredThreshold {
		blocking = append(blocking, "avg_below_threshold")
	}
	if banyakKritis > 0 {
		blocking = append(blocking, "unresolved_critical_misconception")
	}
	if !adaAplikasi {
		blocking = append(blocking, "no_application_assessment")
	}

	status := ""
	switch {
	case len(blocking) == 0:
		status = "MASTERED"
	case banyakKritis > 0 || everMastered || (n >= P.MinAttempts && avg3 != nil && *avg3 < P.NeedsReviewFloor):
		status = "NEEDS_REVIEW"
	case n >= P.MinAttempts:
		status = "PRACTICING"
	default:
		status = "IN_PROGRESS"
	}

	idsJendela := make([]string, 0, len(jendela))
	skorJendela := make([]float64, 0, len(jendela))
	for _, a := range jendela {
		idsJendela = append(idsJendela, a.ID)
		skorJendela = append(skorJendela, a.ScorePct)
	}
	idsAplikasi := make([]string, 0, len(cakupanAplikasi))
	for _, a := range cakupanAplikasi {
		if a.HasApplicationQuestion {
			idsAplikasi = append(idsAplikasi, a.ID)
		}
	}

	everHasil := everMastered || status == "MASTERED"
	bukti := buktiMastery{
		AttemptIDs:            idsJendela,
		ScoreWindow:           skorJendela,
		AvgLast3:              avg3,
		ApplicationAttemptIDs: idsAplikasi,
		MisconceptionFlags:    flags,
		BlockingReasons:       blocking,
		EverMastered:          everHasil,
		Parameters:            parameter,
	}
	encoded, _ := json.Marshal(bukti)

	return MasteryResult{
		Status:       status,
		Evidence:     encoded,
		AvgLast3:     avg3,
		BestScore:    best,
		EverMastered: everHasil,
	}
}

// urutkanAttemptMastery mengurutkan Attempt berdasarkan (ScoredAt ASC,
// ID ASC) tanpa mengubah slice masukan.
func urutkanAttemptMastery(attempts []Attempt) []Attempt {
	urut := append([]Attempt(nil), attempts...)
	sort.SliceStable(urut, func(i, j int) bool {
		if urut[i].ScoredAt.Equal(urut[j].ScoredAt) {
			return urut[i].ID < urut[j].ID
		}
		return urut[i].ScoredAt.Before(urut[j].ScoredAt)
	})
	return urut
}
