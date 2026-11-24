package evaluation

import (
	"math"
	"sort"
)

// ConceptRef adalah satu konsep pada rubrik. Weight harus positif dan
// dipakai sebagai pembobot rubric; IsRequired tidak mengubah rumus skor.
type ConceptRef struct {
	Code       string
	Weight     float64 // > 0
	IsRequired bool
}

// ConceptCoverage adalah hasil evaluasi cakupan satu konsep. Coverage salah
// satu dari "found", "partial", atau "missed".
type ConceptCoverage struct {
	ConceptCode string
	Coverage    string // found | partial | missed
	Evidence    string
}

// EvaluatorSignal adalah keluaran evaluator mentah. ResolvedMisconceptionCodes
// dan Feedback dibawa untuk konsumen lain; perhitungan penalti hanya
// memakai MisconceptionCodes.
type EvaluatorSignal struct {
	ConceptResults             []ConceptCoverage // satu entri per konsep masukan
	MisconceptionCodes         []string
	ResolvedMisconceptionCodes []string
	Relevance                  string // on_topic | off_topic | empty
	ScoreHint                  float64
	Confidence                 float64
	Feedback                   string
}

// ScoringParams memuat seluruh konstanta perhitungan skor (desain 4.2).
type ScoringParams struct {
	PartialCredit           float64
	MinorPenalty            float64
	MinorPenaltyCap         float64
	CriticalPenalty         float64
	CriticalScoreCap        float64
	ClampMin                float64
	ClampMax                float64
	HintDivergenceThreshold float64
	LowConfidenceThreshold  float64
}

// Penalties merangkum kode miskonsepsi yang dihukum dan total poin penalti.
type Penalties struct {
	Minor    []string
	Critical []string
	Points   float64
	Capped   bool
}

// AnswerScore adalah hasil akhir penilaian satu jawaban.
type AnswerScore struct {
	ScorePct    float64
	RubricScore float64
	Penalties   Penalties
	NeedsReview bool
}

// ScoreAnswer menghitung skor jawaban dari rubrik dan sinyal evaluator
// (desain 4.2). Seluruh perhitungan murni dan deterministik; ScoreHint tidak
// pernah memengaruhi ScorePct, hanya status NeedsReview.
func ScoreAnswer(concepts []ConceptRef, out EvaluatorSignal, severityOf map[string]string, P ScoringParams) AnswerScore {
	if out.Relevance == "off_topic" || out.Relevance == "empty" {
		return AnswerScore{
			ScorePct:    P.ClampMin,
			RubricScore: 0,
			Penalties:   Penalties{},
			NeedsReview: out.Confidence < P.LowConfidenceThreshold,
		}
	}

	var totalBobot, bobotTercapai float64
	for _, c := range concepts {
		if c.Weight <= 0 {
			continue
		}
		totalBobot += c.Weight
		bobotTercapai += c.Weight * kreditCakupan(c.Code, out.ConceptResults, P.PartialCredit)
	}
	rubric := 0.0
	if totalBobot > 0 {
		rubric = 100 * bobotTercapai / totalBobot
	}

	minor := map[string]bool{}
	critical := map[string]bool{}
	for _, kode := range out.MisconceptionCodes {
		switch severityOf[kode] {
		case "minor":
			minor[kode] = true
		case "critical":
			critical[kode] = true
		}
	}
	daftarMinor := kodeTerurut(minor)
	daftarKritis := kodeTerurut(critical)

	poinMinor := math.Min(P.MinorPenalty*float64(len(daftarMinor)), P.MinorPenaltyCap)
	poin := poinMinor + P.CriticalPenalty*float64(len(daftarKritis))
	skor := rubric - poin

	capped := false
	if len(daftarKritis) > 0 && skor > P.CriticalScoreCap {
		skor = P.CriticalScoreCap
		capped = true
	}
	skor = batasi(round2(skor), P.ClampMin, P.ClampMax)
	// Pembulatan dapat mengangkat skor sedikit di atas batas kritikal;
	// kembalikan ke batas tersebut agar invariant cap tetap terpenuhi.
	if len(daftarKritis) > 0 && skor > P.CriticalScoreCap {
		skor = batasi(P.CriticalScoreCap, P.ClampMin, P.ClampMax)
	}
	hint := batasi(out.ScoreHint, 0, 100)
	butuhTinjauan := math.Abs(hint-skor) > P.HintDivergenceThreshold ||
		out.Confidence < P.LowConfidenceThreshold

	return AnswerScore{
		ScorePct:    skor,
		RubricScore: rubric,
		Penalties: Penalties{
			Minor:    daftarMinor,
			Critical: daftarKritis,
			Points:   poin,
			Capped:   capped,
		},
		NeedsReview: butuhTinjauan,
	}
}

// kreditCakupan menerjemahkan cakupan menjadi kredit rubrik. Konsep tanpa
// entri dianggap missed (kredit nol), tetapi bobotnya tetap dihitung.
func kreditCakupan(kode string, hasil []ConceptCoverage, partial float64) float64 {
	for _, h := range hasil {
		if h.ConceptCode != kode {
			continue
		}
		switch h.Coverage {
		case "found":
			return 1
		case "partial":
			return partial
		default:
			return 0
		}
	}
	return 0
}

// kodeTerurut mengembalikan kunci map sebagai irisan string yang sudah
// diurutkan. Kode yang berulang dihitung sekali agar tidak dikenai penalti
// ganda.
func kodeTerurut(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// round2 membulatkan ke dua angka desimal dengan aturan setengah ke atas
// menggunakan aritmetika titik tetap.
func round2(x float64) float64 {
	if x >= 0 {
		return math.Floor(x*100+0.5) / 100
	}
	return math.Ceil(x*100-0.5) / 100
}

func batasi(x, batasBawah, batasAtas float64) float64 {
	if x < batasBawah {
		return batasBawah
	}
	if x > batasAtas {
		return batasAtas
	}
	return x
}
