package evaluation

// AnswerAggregate adalah ringkasan hasil penilaian satu jawaban yang dipakai
// saat mengagregasi skor asesmen (desain 4.2). EvalStatus salah satu dari
// "pending", "scored", "failed", atau "unscored".
type AnswerAggregate struct {
	QuestionID string
	EvalStatus string
	IsBlank    bool
	ScorePct   *float64
	Relevance  string // on_topic | off_topic | empty
}

// QuestionAggregate adalah metadata pertanyaan yang menentukan bobot dan
// apakah pertanyaan termasuk ranah aplikasi (application/scenario).
type QuestionAggregate struct {
	ID             string
	Weight         float64
	CognitiveLevel string
}

// AggregateResult adalah keputusan akhir agregasi. Status salah satu dari
// "deferred", "unscored", atau "scored".
type AggregateResult struct {
	Status                 string
	ScorePct               *float64
	HasApplicationQuestion bool
	UnscoredQuestionIDs    []string
}

// AggregateAssessment mengagregasi seluruh jawaban menjadi satu keputusan.
// Fungsi ini murni dan tidak mengubah answers maupun questions. Jawaban dengan
// status pending/failed menangguhkan hasil; jawaban kosong tidak ikut
// menentukan status "unscored"; skor akhir hanya dihitung dari jawahan
// berskor dengan bobot positif (desain 4.2).
func AggregateAssessment(answers []AnswerAggregate, questions map[string]QuestionAggregate) AggregateResult {
	unscoredIDs := make([]string, 0)
	nonblank, nonblankUnscored := 0, 0
	deferred := false

	for _, ans := range answers {
		switch ans.EvalStatus {
		case "pending", "failed":
			deferred = true
		case "unscored":
			unscoredIDs = append(unscoredIDs, ans.QuestionID)
		}
		if !ans.IsBlank {
			nonblank++
			if ans.EvalStatus == "unscored" {
				nonblankUnscored++
			}
		}
	}
	if deferred {
		return AggregateResult{Status: "deferred"}
	}
	if nonblank > 0 && nonblank == nonblankUnscored {
		return AggregateResult{Status: "unscored", UnscoredQuestionIDs: unscoredIDs}
	}

	// Kehadiran pertanyaan aplikasi dinilai terlepas dari bobot atau
	// ketersediaan skor, selama jawabannya berskor, tidak kosong, dan relevan.
	hasApp := false
	for _, ans := range answers {
		if ans.EvalStatus != "scored" || ans.IsBlank || ans.Relevance == "empty" {
			continue
		}
		level := questions[ans.QuestionID].CognitiveLevel
		if level == "application" || level == "scenario" {
			hasApp = true
			break
		}
	}

	pembilang, penyebut := 0.0, 0.0
	for _, ans := range answers {
		if ans.EvalStatus != "scored" || ans.ScorePct == nil {
			continue
		}
		bobot := questions[ans.QuestionID].Weight
		if bobot <= 0 {
			continue
		}
		pembilang += bobot * *ans.ScorePct
		penyebut += bobot
	}

	hasil := AggregateResult{
		Status:                 "scored",
		HasApplicationQuestion: hasApp,
		UnscoredQuestionIDs:    unscoredIDs,
	}
	if penyebut > 0 {
		skor := round2(pembilang / penyebut)
		hasil.ScorePct = &skor
	}
	return hasil
}
