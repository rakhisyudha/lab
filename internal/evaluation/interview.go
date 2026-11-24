package evaluation

import (
	"fmt"
	"math"
	"sort"
)

// FollowUpConfig memuat batas alokasi follow-up interview (desain 4.5): N
// adalah jumlah pertanyaan utama, C total kapasitas seluruh pertanyaan, F
// maksimum follow-up per pertanyaan, dan D kedalaman maksimum rantai.
type FollowUpConfig struct {
	N int
	C int
	F int
	D int
}

// FollowUpState adalah keadaan anggaran interview. M menghitung pertanyaan
// utama yang sudah diajukan, T menghitung seluruh pertanyaan yang sudah
// diajukan, dan FollowupsByQuestion menghitung follow-up per pertanyaan.
type FollowUpState struct {
	M                   int
	T                   int
	FollowupsByQuestion map[string]int
}

// AnsweredRef menandai pertanyaan terakhir yang dijawab beserta kedalamannya
// (0 berarti jawaban pertanyaan utama).
type AnsweredRef struct {
	QuestionID string
	Depth      int
}

// ConceptSignal adalah sinyal cakupan satu konsep dari evaluator. Coverage
// salah satu dari "found", "partial", atau "missed".
type ConceptSignal struct {
	Code       string
	Coverage   string
	IsRequired bool
	Weight     float64
}

// MisconceptionSignal adalah satu miskonsepsi yang terdeteksi pada jawaban.
type MisconceptionSignal struct {
	Code     string
	Severity string
}

// EvalSignalInterview adalah sinyal evaluasi satu jawaban interview. Status
// salah satu dari "scored", "failed", "unscored", atau "pending".
type EvalSignalInterview struct {
	Status                 string
	ScorePct               float64
	Relevance              string
	ConceptResults         []ConceptSignal
	DetectedMisconceptions []MisconceptionSignal
}

// FollowUpParams memuat konstanta pengambilan keputusan follow-up (desain 4.5).
type FollowUpParams struct {
	TriggerScore   float64
	BaseCutoff     float64
	ScarcityWeight float64
	CriticalBonus  float64
}

// FollowUpDecision adalah hasil alokasi follow-up. Reason menjelaskan alasan
// keputusan, sedangkan AnsweredQuestionID dipakai NextStep untuk menghitung
// follow-up pada pertanyaan yang baru saja dijawab.
type FollowUpDecision struct {
	Ask                 bool
	Reason              string
	FocusConcepts       []string
	FocusMisconceptions []string
	AnsweredQuestionID  string
}

// AllocateFollowUp menentukan apakah jawaban terakhir perlu diikuti
// pertanyaan lanjutan (desain 4.5). Fungsi ini murni: keputusan hanya bergantung
// pada angka evaluasi dan state, bukan pada teks yang dihasilkan AI, dan tidak
// mengubah state maupun peta masukan. Setiap keputusan "ya" hanya mungkin bila
// masih ada slot follow-up yang tidak mengganggu pertanyaan utama tersisa.
func AllocateFollowUp(cfg FollowUpConfig, state FollowUpState, answered AnsweredRef, eval EvalSignalInterview, finishRequested bool, P FollowUpParams) FollowUpDecision {
	tolak := func(alasan string) FollowUpDecision {
		return FollowUpDecision{Reason: alasan, AnsweredQuestionID: answered.QuestionID}
	}

	remaining := cfg.N - state.M
	available := cfg.C - state.T - remaining
	f := state.FollowupsByQuestion[answered.QuestionID]

	// Urutan pemeriksaan penting: alasan yang lebih awal mengalahkan alasan
	// berikutnya, sehingga diagnostik keputusan tetap deterministik.
	if finishRequested {
		return tolak("finish_requested")
	}
	if eval.Status != "scored" {
		return tolak("evaluation_not_scored")
	}
	if eval.Relevance == "off_topic" || eval.Relevance == "empty" {
		return tolak("not_probeable")
	}
	if f >= cfg.F {
		return tolak("per_question_limit")
	}
	if answered.Depth >= cfg.D {
		return tolak("max_depth")
	}
	if available <= 0 {
		return tolak("total_cap")
	}

	adaKonsepWajibTerlewat := false
	for _, c := range eval.ConceptResults {
		if c.IsRequired && c.Coverage == "missed" {
			adaKonsepWajibTerlewat = true
			break
		}
	}
	adaKritis := false
	for _, m := range eval.DetectedMisconceptions {
		if m.Severity == "critical" {
			adaKritis = true
			break
		}
	}
	if !adaKonsepWajibTerlewat && !adaKritis && eval.ScorePct >= P.TriggerScore {
		return tolak("sufficient")
	}

	need := 100 - eval.ScorePct
	if adaKritis {
		need += P.CriticalBonus
	}
	need = math.Min(100, need)
	scarcity := 1 - math.Min(1, float64(available)/math.Max(1, float64(remaining)))
	cutoff := P.BaseCutoff + P.ScarcityWeight*scarcity
	if need < cutoff {
		return tolak("below_cutoff")
	}

	// Konsep yang perlu digali: yang belum-found, didahulukan yang wajib,
	// lalu bobot terbesar, lalu kode terkecil agar urutan stabil.
	type konsepFokus struct {
		kode  string
		wajib bool
		bobot float64
	}
	konsep := make([]konsepFokus, 0, len(eval.ConceptResults))
	for _, c := range eval.ConceptResults {
		if c.Coverage == "missed" || c.Coverage == "partial" {
			konsep = append(konsep, konsepFokus{kode: c.Code, wajib: c.IsRequired, bobot: c.Weight})
		}
	}
	sort.Slice(konsep, func(i, j int) bool {
		if konsep[i].wajib != konsep[j].wajib {
			return konsep[i].wajib
		}
		if konsep[i].bobot != konsep[j].bobot {
			return konsep[i].bobot > konsep[j].bobot
		}
		return konsep[i].kode < konsep[j].kode
	})
	focusKonsep := make([]string, 0, batasTerkecilInterview(3, len(konsep)))
	for i := 0; i < len(konsep) && i < 3; i++ {
		focusKonsep = append(focusKonsep, konsep[i].kode)
	}

	// Miskonsepsi kritis didahulukan, sisanya diurutkan menurut kode.
	type miskonsepsiFokus struct {
		kode   string
		kritis bool
	}
	mis := make([]miskonsepsiFokus, 0, len(eval.DetectedMisconceptions))
	for _, m := range eval.DetectedMisconceptions {
		mis = append(mis, miskonsepsiFokus{kode: m.Code, kritis: m.Severity == "critical"})
	}
	sort.Slice(mis, func(i, j int) bool {
		if mis[i].kritis != mis[j].kritis {
			return mis[i].kritis
		}
		return mis[i].kode < mis[j].kode
	})
	focusMiskonsepsi := make([]string, 0, batasTerkecilInterview(2, len(mis)))
	for i := 0; i < len(mis) && i < 2; i++ {
		focusMiskonsepsi = append(focusMiskonsepsi, mis[i].kode)
	}

	if len(focusKonsep) == 0 && len(focusMiskonsepsi) == 0 {
		return tolak("no_focus")
	}

	return FollowUpDecision{
		Ask:                 true,
		Reason:              "followup_needed",
		FocusConcepts:       focusKonsep,
		FocusMisconceptions: focusMiskonsepsi,
		AnsweredQuestionID:  answered.QuestionID,
	}
}

// NextStep menerapkan keputusan alokasi dengan menghasilkan state baru. Peta
// follow-up selalu disalin sehingga state masukan tidak pernah diubah.
func NextStep(state FollowUpState, cfg FollowUpConfig, decision FollowUpDecision) (FollowUpState, string) {
	next := FollowUpState{
		M:                   state.M,
		T:                   state.T,
		FollowupsByQuestion: make(map[string]int, len(state.FollowupsByQuestion)+1),
	}
	for kode, jumlah := range state.FollowupsByQuestion {
		next.FollowupsByQuestion[kode] = jumlah
	}

	if decision.Ask {
		next.T++
		next.FollowupsByQuestion[decision.AnsweredQuestionID]++
		return next, "ask_followup"
	}
	if state.M < cfg.N {
		next.M++
		next.T++
		return next, "next_main"
	}
	return next, "finalize"
}

// batasTerkecilInterview mengembalikan nilai terkecil dari dua bilangan bulat.
func batasTerkecilInterview(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// DimensionScore adalah skor satu dimensi evaluasi interview beserta alasannya.
type DimensionScore struct {
	Score     int
	Rationale string
}

// InterviewEvaluationOutput adalah output evaluator interview tervalidasi.
type InterviewEvaluationOutput struct {
	DimensionScores map[string]DimensionScore
	Confidence      float64
}

// ScoreInterviewResult memuat skor tiap dimensi (score, weight, rationale),
// skor keseluruhan, dan penanda perlunya tinjauan manusia.
type ScoreInterviewResult struct {
	DimensionScores map[string]any
	OverallScore    float64
	NeedsReview     bool
}

// kunciDimensiInterview adalah lima dimensi wajib hasil evaluasi interview.
var kunciDimensiInterview = []string{
	"technical_accuracy",
	"conceptual_understanding",
	"reasoning",
	"practical_application",
	"communication",
}

// ScoreInterview menghitung skor keseluruhan interview sebagai rata-rata
// berbobot dari lima dimensi yang sudah di-clamp ke [0,100]. Fungsi ini murni
// dan mengembalikan error bila ada dimensi atau bobot yang hilang, atau bila
// total bobot nol. Skor keseluruhan sengaja terpisah dari skor Attempt.
func ScoreInterview(out InterviewEvaluationOutput, weights map[string]float64, answerScores []float64,
	lowConfidenceThreshold, hintDivergenceThreshold float64) (ScoreInterviewResult, error) {

	clamped := make(map[string]int, len(kunciDimensiInterview))
	perDimensi := make(map[string]any, len(kunciDimensiInterview))
	pembilang, penyebut := 0.0, 0.0

	for _, kunci := range kunciDimensiInterview {
		nilai, ada := out.DimensionScores[kunci]
		if !ada {
			return ScoreInterviewResult{}, fmt.Errorf("dimensi %s tidak ditemukan", kunci)
		}
		bobot, ada := weights[kunci]
		if !ada {
			return ScoreInterviewResult{}, fmt.Errorf("bobot dimensi %s tidak ditemukan", kunci)
		}
		skor := clampSkorInterview(nilai.Score)
		clamped[kunci] = skor
		pembilang += float64(skor) * bobot
		penyebut += bobot
		perDimensi[kunci] = map[string]any{
			"score":     skor,
			"weight":    bobot,
			"rationale": nilai.Rationale,
		}
	}
	if penyebut == 0 {
		return ScoreInterviewResult{}, fmt.Errorf("total bobot dimensi interview nol")
	}

	butuhTinjauan := out.Confidence < lowConfidenceThreshold
	if len(answerScores) > 0 {
		jumlah := 0.0
		for _, skor := range answerScores {
			jumlah += skor
		}
		rataJawaban := jumlah / float64(len(answerScores))
		if math.Abs(float64(clamped["technical_accuracy"])-rataJawaban) > hintDivergenceThreshold {
			butuhTinjauan = true
		}
	}

	return ScoreInterviewResult{
		DimensionScores: perDimensi,
		OverallScore:    round2(pembilang / penyebut),
		NeedsReview:     butuhTinjauan,
	}, nil
}

// clampSkorInterview membatasi skor Integer ke rentang 0 sampai 100.
func clampSkorInterview(skor int) int {
	if skor < 0 {
		return 0
	}
	if skor > 100 {
		return 100
	}
	return skor
}

// ChainAnswer adalah satu jawaban dalam rantai satu pertanyaan interview.
type ChainAnswer struct {
	EvalStatus string
	ScorePct   *float64
	Relevance  string
}

// InterviewChain merangkum satu pertanyaan utama beserta seluruh jawaban
// follow-upnya untuk keperluan agregasi per Topic.
type InterviewChain struct {
	QuestionID     string
	TopicID        string
	Weight         float64
	CognitiveLevel string
	Answers        []ChainAnswer
}

// AggregateInterviewTopic mengagregasi skor per Topic interview dari rantai
// jawaban yang berskor saja. Mengembalikan nil bila tidak ada rantai Topic
// tersebut yang memiliki jawaban berskor. Fungsi ini murni dan tidak mengubah
// rantai masukan.
func AggregateInterviewTopic(chains []InterviewChain, topicID string) *AggregateResult {
	type baris struct {
		bobot    float64
		skor     float64
		utamaOK  bool
		kognitif string
	}
	barisan := make([]baris, 0, len(chains))

	for _, rantai := range chains {
		if rantai.TopicID != topicID {
			continue
		}
		jumlah, banyak := 0.0, 0
		for _, jawaban := range rantai.Answers {
			if jawaban.EvalStatus != "scored" || jawaban.ScorePct == nil {
				continue
			}
			jumlah += *jawaban.ScorePct
			banyak++
		}
		if banyak == 0 {
			continue
		}

		utamaOK := false
		if len(rantai.Answers) > 0 {
			utama := rantai.Answers[0]
			utamaOK = utama.EvalStatus == "scored" && utama.Relevance != "empty"
		}
		barisan = append(barisan, baris{
			bobot:    rantai.Weight,
			skor:     jumlah / float64(banyak),
			utamaOK:  utamaOK,
			kognitif: rantai.CognitiveLevel,
		})
	}

	if len(barisan) == 0 {
		return nil
	}

	pembilang, penyebut := 0.0, 0.0
	punyaAplikasi := false
	for _, bar := range barisan {
		pembilang += bar.bobot * bar.skor
		penyebut += bar.bobot
		if bar.utamaOK && (bar.kognitif == "application" || bar.kognitif == "scenario") {
			punyaAplikasi = true
		}
	}

	skor := 0.0
	if penyebut != 0 {
		skor = round2(pembilang / penyebut)
	}
	return &AggregateResult{
		Status:                 "scored",
		ScorePct:               &skor,
		HasApplicationQuestion: punyaAplikasi,
	}
}
