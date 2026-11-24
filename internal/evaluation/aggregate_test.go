package evaluation

import (
	"fmt"
	"math"
	"testing"

	"pgregory.net/rapid"
)

// TestPropW13_AgregatBerbobotDanTerbatas memeriksa Property 13: dengan bobot
// positif, tanpa status pending/failed, dan skor berskor di [0,100], hasil
// "scored" berada di antara skor minimum dan maksimum serta sama dengan
// rata-rata berbobot dari jawaban berskor saja. Status "deferred" hanya
// mungkin bila ada jawaban pending/failed.
func TestPropW13_AgregatBerbobotDanTerbatas(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		answers, questions := drawAgregatInput(t)
		got := AggregateAssessment(answers, questions)

		if got.Status == "deferred" {
			t.Fatalf("deferred tanpa jawaban pending/failed: %+v", answers)
		}

		bobot, pembilang := 0.0, 0.0
		minSkor, maxSkor := math.Inf(1), math.Inf(-1)
		adaBerskorNonblank := false
		for _, ans := range answers {
			if ans.EvalStatus != "scored" || ans.ScorePct == nil {
				continue
			}
			if !ans.IsBlank {
				adaBerskorNonblank = true
			}
			b := questions[ans.QuestionID].Weight
			if b <= 0 {
				continue
			}
			bobot += b
			pembilang += b * *ans.ScorePct
			minSkor = math.Min(minSkor, *ans.ScorePct)
			maxSkor = math.Max(maxSkor, *ans.ScorePct)
		}

		// Jawaban berskor yang tidak kosong menghalangi status "unscored".
		if adaBerskorNonblank && got.Status != "scored" {
			t.Fatalf("status %q padahal ada jawaban berskor tidak kosong", got.Status)
		}
		if got.Status == "scored" && bobot > 0 {
			if got.ScorePct == nil {
				t.Fatal("skor nil padahal bobot positif")
			}
			const toleransi = 0.005 + 1e-9 // pembulatan setengah ke atas
			ingin := pembilang / bobot
			if math.Abs(*got.ScorePct-ingin) > toleransi {
				t.Fatalf("skor %v, ingin rata-rata berbobot %v", *got.ScorePct, ingin)
			}
			if *got.ScorePct < minSkor-toleransi || *got.ScorePct > maxSkor+toleransi {
				t.Fatalf("skor %v di luar rentang [%v,%v]", *got.ScorePct, minSkor, maxSkor)
			}
			if *got.ScorePct < 0 || *got.ScorePct > 100 {
				t.Fatalf("skor %v di luar [0,100]", *got.ScorePct)
			}
		} else if got.Status == "unscored" && got.ScorePct != nil {
			t.Fatalf("status unscored tidak boleh membawa skor: %v", *got.ScorePct)
		}

		// Satu status pending/failed saja sudah cukup untuk menangguhkan.
		for _, status := range []string{"pending", "failed"} {
			diubah := append([]AnswerAggregate(nil), answers...)
			diubah[0].EvalStatus = status
			if hasil := AggregateAssessment(diubah, questions); hasil.Status != "deferred" {
				t.Fatalf("status %s menghasilkan %q, ingin deferred", status, hasil.Status)
			}
		}
	})
}

// TestPropW28_AgregatKebalTerhadapUnscored memeriksa Property 28:
// menambah/membuang jawaban unscored tidak mengubah ScorePct maupun
// HasApplicationQuestion dari hasil "scored"; semua jawaban tidak kosong
// yang unscored menghasilkan "unscored"; dan status pending/failed
// menghasilkan "deferred".
func TestPropW28_AgregatKebalTerhadapUnscored(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		answers, questions := drawAgregatInput(t)
		// Pastikan ada jawaban berskor yang tidak kosong.
		utama := &answers[0]
		utama.EvalStatus = "scored"
		utama.IsBlank = false
		skor := rapid.Float64Range(0, 100).Draw(t, "w28_skor_utama")
		utama.ScorePct = &skor

		dasar := AggregateAssessment(answers, questions)
		if dasar.Status != "scored" {
			t.Fatalf("status dasar %q, ingin scored", dasar.Status)
		}

		tanpa := make([]AnswerAggregate, 0, len(answers))
		for _, ans := range answers {
			if ans.EvalStatus != "unscored" {
				tanpa = append(tanpa, ans)
			}
		}
		cekSetara := func(hasil AggregateResult, konteks string) {
			if hasil.Status != "scored" {
				t.Fatalf("%s: status %q, ingin scored", konteks, hasil.Status)
			}
			if !agregatSamaSkor(dasar.ScorePct, hasil.ScorePct) {
				t.Fatalf("%s: skor berubah: %v menjadi %v", konteks, agregatTulisSkor(dasar.ScorePct), agregatTulisSkor(hasil.ScorePct))
			}
			if hasil.HasApplicationQuestion != dasar.HasApplicationQuestion {
				t.Fatalf("%s: HasApplicationQuestion berubah", konteks)
			}
		}
		cekSetara(AggregateAssessment(tanpa, questions), "tanpa unscored")

		ditambah := append([]AnswerAggregate(nil), answers...)
		for i := 0; i < 2; i++ {
			ditambah = append(ditambah, AnswerAggregate{
				QuestionID: fmt.Sprintf("U%d", i),
				EvalStatus: "unscored",
				IsBlank:    rapid.Bool().Draw(t, fmt.Sprintf("w28_kosong_%d", i)),
				Relevance:  "empty",
			})
		}
		cekSetara(AggregateAssessment(ditambah, questions), "dengan unscored tambahan")

		// Semua jawaban tidak kosong kini unscored.
		semuaUnscored := make([]AnswerAggregate, 0, len(answers))
		for _, ans := range answers {
			ans.EvalStatus = "unscored"
			ans.IsBlank = false
			ans.ScorePct = nil
			semuaUnscored = append(semuaUnscored, ans)
		}
		if hasil := AggregateAssessment(semuaUnscored, questions); hasil.Status != "unscored" {
			t.Fatalf("status %q, ingin unscored", hasil.Status)
		}

		for _, status := range []string{"pending", "failed"} {
			diubah := append([]AnswerAggregate(nil), answers...)
			diubah = append(diubah, AnswerAggregate{QuestionID: "X", EvalStatus: status})
			if hasil := AggregateAssessment(diubah, questions); hasil.Status != "deferred" {
				t.Fatalf("status %s menghasilkan %q, ingin deferred", status, hasil.Status)
			}
		}
	})
}

// drawAgregatInput menghasilkan soal berbobot positif dan jawaban dengan
// status scored/unscored (tanpa pending/failed).
func drawAgregatInput(t *rapid.T) ([]AnswerAggregate, map[string]QuestionAggregate) {
	n := rapid.IntRange(1, 6).Draw(t, "agregat_jumlah_soal")
	questions := make(map[string]QuestionAggregate, n)
	answers := make([]AnswerAggregate, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("Q%d", i)
		questions[id] = QuestionAggregate{
			ID:             id,
			Weight:         rapid.Float64Range(0.1, 5).Draw(t, "agregat_bobot_"+id),
			CognitiveLevel: []string{"application", "scenario", "remember", "understand"}[rapid.IntRange(0, 3).Draw(t, "agregat_kognitif_"+id)],
		}
		status := []string{"scored", "unscored"}[rapid.IntRange(0, 1).Draw(t, "agregat_status_"+id)]
		ans := AnswerAggregate{
			QuestionID: id,
			EvalStatus: status,
			IsBlank:    rapid.Bool().Draw(t, "agregat_kosong_"+id),
			Relevance:  []string{"on_topic", "off_topic", "empty"}[rapid.IntRange(0, 2).Draw(t, "agregat_relevansi_"+id)],
		}
		if status == "scored" {
			nilai := rapid.Float64Range(0, 100).Draw(t, "agregat_skor_"+id)
			ans.ScorePct = &nilai
		}
		answers = append(answers, ans)
	}
	return answers, questions
}

func agregatSamaSkor(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return math.Abs(*a-*b) < 1e-12
}

func agregatTulisSkor(p *float64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprint(*p)
}
