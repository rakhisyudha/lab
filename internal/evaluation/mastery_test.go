package evaluation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Struktur untuk membaca evidence hasil ComputeMastery dalam pengujian.
type flagBuktiUji struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Status   string `json:"status"`
}

type parameterBuktiUji struct {
	MasteredThreshold        float64 `json:"mastered_threshold"`
	MinAttempts              int     `json:"min_attempts"`
	WindowSize               int     `json:"window_size"`
	NeedsReviewFloor         float64 `json:"needs_review_floor"`
	ApplicationEvidenceScope string  `json:"application_evidence_scope"`
	IsInitialValue           bool    `json:"is_initial_value"`
}

type buktiUji struct {
	AttemptIDs            []string          `json:"attempt_ids"`
	ScoreWindow           []float64         `json:"score_window"`
	AvgLast3              *float64          `json:"avg_last3"`
	ApplicationAttemptIDs []string          `json:"application_attempt_ids"`
	MisconceptionFlags    []flagBuktiUji    `json:"misconception_flags"`
	BlockingReasons       []string          `json:"blocking_reasons"`
	EverMastered          bool              `json:"ever_mastered"`
	Parameters            parameterBuktiUji `json:"parameters"`
}

func drawMasteryInput(t *rapid.T) ([]Attempt, []MisconceptionFlag, bool, MasteryParams) {
	P := MasteryParams{
		MasteredThreshold:        rapid.Float64Range(0, 100).Draw(t, "ambang_mastered"),
		MinAttempts:              rapid.IntRange(1, 6).Draw(t, "min_attempts"),
		WindowSize:               rapid.IntRange(1, 6).Draw(t, "window_size"),
		NeedsReviewFloor:         rapid.Float64Range(0, 100).Draw(t, "needs_review_floor"),
		ApplicationEvidenceScope: []string{"window", "all"}[rapid.IntRange(0, 1).Draw(t, "scope_aplikasi")],
		AllInitial:               rapid.Bool().Draw(t, "semua_initial"),
	}

	dasar := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	n := rapid.IntRange(0, 8).Draw(t, "jumlah_attempt")
	attempts := make([]Attempt, 0, n)
	for i := 0; i < n; i++ {
		attempts = append(attempts, Attempt{
			ID:                     fmt.Sprintf("a-%02d", i),
			ScorePct:               round2(rapid.Float64Range(0, 100).Draw(t, fmt.Sprintf("skor_%d", i))),
			HasApplicationQuestion: rapid.Bool().Draw(t, fmt.Sprintf("aplikatif_%d", i)),
			ScoredAt:               dasar.Add(time.Duration(rapid.IntRange(0, 20).Draw(t, fmt.Sprintf("waktu_%d", i))) * time.Hour),
		})
	}

	banyakFlag := rapid.IntRange(0, 5).Draw(t, "jumlah_flag")
	flags := make([]MisconceptionFlag, 0, banyakFlag)
	for i := 0; i < banyakFlag; i++ {
		flags = append(flags, MisconceptionFlag{
			Code:     fmt.Sprintf("M%d", rapid.IntRange(0, 9).Draw(t, fmt.Sprintf("kode_flag_%d", i))),
			Severity: []string{"minor", "critical", "major"}[rapid.IntRange(0, 2).Draw(t, fmt.Sprintf("severity_flag_%d", i))],
			Status:   []string{"detected", "resolved"}[rapid.IntRange(0, 1).Draw(t, fmt.Sprintf("status_flag_%d", i))],
		})
	}

	return attempts, flags, rapid.Bool().Draw(t, "pernah_mastered"), P
}

func urutkanAttempt(attempts []Attempt) []Attempt {
	urut := append([]Attempt(nil), attempts...)
	sort.SliceStable(urut, func(i, j int) bool {
		if urut[i].ScoredAt.Equal(urut[j].ScoredAt) {
			return urut[i].ID < urut[j].ID
		}
		return urut[i].ScoredAt.Before(urut[j].ScoredAt)
	})
	return urut
}

// oracleMastery adalah implementasi independen dari aturan desain 4.4 yang
// dipakai sebagai oracle pada properti 15 dan 16.
func oracleMastery(attempts []Attempt, flags []MisconceptionFlag, ever bool, P MasteryParams) (status string, blocking []string, window []Attempt, avg float64, adaAplikasi, kritis bool) {
	urut := urutkanAttempt(attempts)
	n := len(urut)
	mulai := 0
	if P.WindowSize < n {
		mulai = n - P.WindowSize
	}
	if mulai < 0 {
		mulai = 0
	}
	window = urut[mulai:]

	// Tanpa Attempt, aturan NOT_STARTED lebih dulu dan hanya satu alasan.
	if n == 0 {
		return "NOT_STARTED", []string{"attempts_below_min"}, window, 0, false, false
	}

	if len(window) > 0 {
		total := 0.0
		for _, a := range window {
			total += a.ScorePct
		}
		avg = round2(total / float64(len(window)))
	}
	for _, m := range flags {
		if m.Severity == "critical" && m.Status == "detected" {
			kritis = true
			break
		}
	}

	cakupan := urut
	if P.ApplicationEvidenceScope == "window" {
		cakupan = window
	}
	for _, a := range cakupan {
		if a.HasApplicationQuestion {
			adaAplikasi = true
			break
		}
	}

	blocking = []string{}
	if n < P.MinAttempts {
		blocking = append(blocking, "attempts_below_min")
	} else if avg < P.MasteredThreshold {
		blocking = append(blocking, "avg_below_threshold")
	}
	if kritis {
		blocking = append(blocking, "unresolved_critical_misconception")
	}
	if !adaAplikasi {
		blocking = append(blocking, "no_application_assessment")
	}

	switch {
	case len(blocking) == 0:
		status = "MASTERED"
	case kritis || ever || (n >= P.MinAttempts && avg < P.NeedsReviewFloor):
		status = "NEEDS_REVIEW"
	case n >= P.MinAttempts:
		status = "PRACTICING"
	default:
		status = "IN_PROGRESS"
	}
	return
}

func parseBuktiUji(t interface{ Fatalf(string, ...interface{}) }, raw json.RawMessage) buktiUji {
	var b buktiUji
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("evidence bukan JSON yang valid: %v (%s)", err, raw)
	}
	return b
}

// TestPropW15_MasteredIffSyaratTerpenuhi adalah Property 15: status MASTERED
// jika dan hanya jika empat syarat terpenuhi, dan blocking_reasons memuat
// tepat syarat yang tidak terpenuhi (desain 4.4).
func TestPropW15_MasteredIffSyaratTerpenuhi(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		attempts, flags, ever, P := drawMasteryInput(t)
		got := ComputeMastery(attempts, flags, ever, P)

		statusOracle, blockingOracle, window, avg, adaAplikasi, kritis := oracleMastery(attempts, flags, ever, P)
		harusMastered := len(attempts) >= P.MinAttempts &&
			avg >= P.MasteredThreshold && !kritis && adaAplikasi

		if (got.Status == "MASTERED") != harusMastered {
			t.Fatalf("MASTERED=%v, ingin %v; status=%q n=%d avg=%v ambang=%v kritis=%v aplikasi=%v",
				got.Status == "MASTERED", harusMastered, got.Status, len(attempts), avg,
				P.MasteredThreshold, kritis, adaAplikasi)
		}
		if got.Status != statusOracle {
			t.Fatalf("status=%q, ingin oracle %q", got.Status, statusOracle)
		}
		if kritis && got.Status == "MASTERED" {
			t.Fatal("miskonsepsi kritikal yang belum terselesaikan tidak boleh MASTERED")
		}
		if len(attempts) == 0 {
			if got.AvgLast3 != nil {
				t.Fatalf("avg_last3 harus nil tanpa attempt, got %v", *got.AvgLast3)
			}
		} else if got.AvgLast3 == nil || *got.AvgLast3 != avg {
			t.Fatalf("avg_last3=%v, ingin %v", got.AvgLast3, avg)
		}

		bukti := parseBuktiUji(t, got.Evidence)
		if !reflect.DeepEqual(bukti.BlockingReasons, blockingOracle) {
			t.Fatalf("blocking_reasons=%v, ingin %v", bukti.BlockingReasons, blockingOracle)
		}
		if bukti.EverMastered != (ever || got.Status == "MASTERED") || got.EverMastered != bukti.EverMastered {
			t.Fatalf("ever_mastered: hasil=%v evidence=%v, ingin %v",
				got.EverMastered, bukti.EverMastered, ever || got.Status == "MASTERED")
		}

		idsWindow := make([]string, 0, len(window))
		skorWindow := make([]float64, 0, len(window))
		for _, a := range window {
			idsWindow = append(idsWindow, a.ID)
			skorWindow = append(skorWindow, a.ScorePct)
		}
		if !reflect.DeepEqual(bukti.AttemptIDs, idsWindow) || !reflect.DeepEqual(bukti.ScoreWindow, skorWindow) {
			t.Fatalf("jendela: ids=%v skor=%v, ingin ids=%v skor=%v",
				bukti.AttemptIDs, bukti.ScoreWindow, idsWindow, skorWindow)
		}

		terbaik := 0.0
		for i, a := range urutkanAttempt(attempts) {
			if i == 0 || a.ScorePct > terbaik {
				terbaik = a.ScorePct
			}
		}
		if len(attempts) == 0 {
			if got.BestScore != nil {
				t.Fatalf("best_score harus nol (nil) tanpa attempt, got %v", *got.BestScore)
			}
		} else if got.BestScore == nil || *got.BestScore != terbaik {
			t.Fatalf("best_score=%v, ingin %v", got.BestScore, terbaik)
		}
	})
}

// TestPropW16_DeterministikIdempotenDanDiurutkan adalah Property 16: hasil
// identik untuk input sama, idempoten terhadap ever_mastered, monoton, dan
// tidak bergantung pada urutan Attempt maupun flag di input.
func TestPropW16_DeterministikIdempotenDanDiurutkan(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		attempts, flags, ever, P := drawMasteryInput(t)
		pertama := ComputeMastery(attempts, flags, ever, P)
		kedua := ComputeMastery(attempts, flags, ever, P)
		if !reflect.DeepEqual(pertama, kedua) {
			t.Fatalf("hasil tidak deterministik: %+v vs %+v", pertama, kedua)
		}

		ulang := ComputeMastery(attempts, flags, pertama.EverMastered, P)
		if !reflect.DeepEqual(pertama, ulang) {
			t.Fatalf("tidak idempoten: %+v vs %+v", pertama, ulang)
		}
		if pertama.EverMastered && !ulang.EverMastered {
			t.Fatal("ever_mastered tidak monoton")
		}
		if ever && !pertama.EverMastered {
			t.Fatal("ever_mastered masukan true harus tetap true")
		}
		if pertama.Status == "MASTERED" && !pertama.EverMastered {
			t.Fatal("status MASTERED harus menandai ever_mastered")
		}

		// Urutan input Attempt tidak penting (diubah dengan Fisher-Yates
		// memakai generator rapid).
		diacak := append([]Attempt(nil), attempts...)
		for i := len(diacak) - 1; i > 0; i-- {
			j := rapid.IntRange(0, i).Draw(t, fmt.Sprintf("tukar_%d", i))
			diacak[i], diacak[j] = diacak[j], diacak[i]
		}
		if !reflect.DeepEqual(pertama, ComputeMastery(diacak, flags, ever, P)) {
			t.Fatal("urutan Attempt masukan memengaruhi hasil")
		}

		// Urutan flag juga tidak penting; evidence diurutkan menurut kode.
		flagAcak := append([]MisconceptionFlag(nil), flags...)
		for i := len(flagAcak) - 1; i > 0; i-- {
			j := rapid.IntRange(0, i).Draw(t, fmt.Sprintf("tukar_flag_%d", i))
			flagAcak[i], flagAcak[j] = flagAcak[j], flagAcak[i]
		}
		if !reflect.DeepEqual(pertama, ComputeMastery(attempts, flagAcak, ever, P)) {
			t.Fatal("urutan flag masukan memengaruhi hasil")
		}

		bukti := parseBuktiUji(t, pertama.Evidence)
		inginParameter := parameterBuktiUji{
			MasteredThreshold:        P.MasteredThreshold,
			MinAttempts:              P.MinAttempts,
			WindowSize:               P.WindowSize,
			NeedsReviewFloor:         P.NeedsReviewFloor,
			ApplicationEvidenceScope: P.ApplicationEvidenceScope,
			IsInitialValue:           P.AllInitial,
		}
		if bukti.Parameters != inginParameter {
			t.Fatalf("parameters=%+v, ingin %+v", bukti.Parameters, inginParameter)
		}
		flagDiurut := append([]flagBuktiUji(nil), bukti.MisconceptionFlags...)
		sort.SliceStable(flagDiurut, func(i, j int) bool { return flagDiurut[i].Code < flagDiurut[j].Code })
		if len(flagDiurut) != len(bukti.MisconceptionFlags) {
			t.Fatalf("jumlah misconception_flags berubah: %v", bukti.MisconceptionFlags)
		}
		for i := range flagDiurut {
			if flagDiurut[i] != bukti.MisconceptionFlags[i] {
				t.Fatalf("misconception_flags tidak terurut menurut kode: %v", bukti.MisconceptionFlags)
			}
		}
	})
}

// TestPropW16_TabelKeputusan mengunci tabel keputusan D-6 untuk kasus tanpa
// Attempt, Attempt di bawah minimum, miskonsepsi kritikal, ever_mastered, dan
// rata-rata di bawah needs_review_floor.
func TestPropW16_TabelKeputusan(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		P := MasteryParams{
			MasteredThreshold:        80,
			MinAttempts:              3,
			WindowSize:               3,
			NeedsReviewFloor:         50,
			ApplicationEvidenceScope: "window",
			AllInitial:               true,
		}
		dasar := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
		buat := func(skor ...float64) []Attempt {
			out := make([]Attempt, 0, len(skor))
			for i, s := range skor {
				out = append(out, Attempt{
					ID:                     fmt.Sprintf("t-%02d", i),
					ScorePct:               s,
					HasApplicationQuestion: true,
					ScoredAt:               dasar.Add(time.Duration(i) * time.Hour),
				})
			}
			return out
		}
		kritis := []MisconceptionFlag{{Code: "M-KRITIS", Severity: "critical", Status: "detected"}}

		kasus := []struct {
			nama     string
			attempts []Attempt
			flags    []MisconceptionFlag
			ever     bool
			ingin    string
		}{
			{"tanpa attempt", nil, nil, false, "NOT_STARTED"},
			{"dua attempt", buat(90, 95), nil, false, "IN_PROGRESS"},
			{"dua attempt dengan kritikal", buat(90, 95), kritis, false, "NEEDS_REVIEW"},
			{"tiga attempt master", buat(85, 82, 90), nil, false, "MASTERED"},
			{"tiga attempt tanpa aplikasi", func() []Attempt {
				a := buat(85, 82, 90)
				for i := range a {
					a[i].HasApplicationQuestion = false
				}
				return a
			}(), nil, false, "PRACTICING"},
			{"tiga attempt dengan kritikal", buat(85, 82, 90), kritis, false, "NEEDS_REVIEW"},
			{"pernah mastered dengan skor tinggi", buat(85, 82, 90), nil, true, "MASTERED"},
			{"pernah mastered lalu turun", buat(90, 70, 72), nil, true, "NEEDS_REVIEW"},
			{"rata-rata 68", buat(60, 70, 75), nil, false, "PRACTICING"},
			{"rata-rata 45", buat(40, 45, 50), nil, false, "NEEDS_REVIEW"},
		}

		for _, c := range kasus {
			got := ComputeMastery(c.attempts, c.flags, c.ever, P)
			if got.Status != c.ingin {
				t.Fatalf("kasus %s: status = %q, ingin %q; avg=%v", c.nama, got.Status, c.ingin, got.AvgLast3)
			}
		}
	})
}
