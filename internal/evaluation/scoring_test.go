package evaluation

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

// TestPropW11_SkorDalamBatasDanDeterministik memeriksa Property 11: skor
// selalu berada di [ClampMin, ClampMax], deterministik untuk input yang sama,
// dan tidak berubah hanya karena ScoreHint diubah.
func TestPropW11_SkorDalamBatasDanDeterministik(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		P, concepts, out, severity := drawScoringInput(t)

		got := ScoreAnswer(concepts, out, severity, P)
		if got.ScorePct < P.ClampMin-1e-9 || got.ScorePct > P.ClampMax+1e-9 {
			t.Fatalf("skor %v di luar [%v,%v]", got.ScorePct, P.ClampMin, P.ClampMax)
		}
		if lagi := ScoreAnswer(concepts, out, severity, P); !reflect.DeepEqual(got, lagi) {
			t.Fatalf("hasil tidak deterministik: %+v vs %+v", got, lagi)
		}

		hintBaru := rapid.Float64Range(-50, 150).Draw(t, "hint_baru")
		outLain := out
		outLain.ScoreHint = hintBaru
		if lain := ScoreAnswer(concepts, outLain, severity, P); lain.ScorePct != got.ScorePct {
			t.Fatalf("skor berubah hanya karena hint: %v menjadi %v", got.ScorePct, lain.ScorePct)
		}
	})
}

// TestPropW11_KasusKhusus memeriksa bagian Property 11 untuk relevansi
// off_topic/empty, batas skor kritikal, dan batas penalti minor.
func TestPropW11_KasusKhusus(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		P, concepts, out, severity := drawScoringInput(t)

		for _, relevansi := range []string{"off_topic", "empty"} {
			o := out
			o.Relevance = relevansi
			got := ScoreAnswer(concepts, o, severity, P)
			if got.ScorePct != P.ClampMin || got.RubricScore != 0 {
				t.Fatalf("relevansi %s: skor=%v rubrik=%v", relevansi, got.ScorePct, got.RubricScore)
			}
			if len(got.Penalties.Minor) != 0 || len(got.Penalties.Critical) != 0 ||
				got.Penalties.Points != 0 || got.Penalties.Capped {
				t.Fatalf("relevansi %s memiliki penalti: %+v", relevansi, got.Penalties)
			}
			if ingin := o.Confidence < P.LowConfidenceThreshold; got.NeedsReview != ingin {
				t.Fatalf("relevansi %s: needs_review=%v, ingin %v", relevansi, got.NeedsReview, ingin)
			}
		}

		// Kehadiran miskonsepsi kritikal membatasi skor akhir.
		severityKritis := salinSeverity(severity)
		severityKritis["M-KRITIKAL"] = "critical"
		oKritis := out
		oKritis.MisconceptionCodes = append(append([]string(nil), out.MisconceptionCodes...), "M-KRITIKAL")
		kritis := ScoreAnswer(concepts, oKritis, severityKritis, P)
		if kritis.ScorePct > P.CriticalScoreCap+1e-9 {
			t.Fatalf("skor kritikal %v melebihi batas %v", kritis.ScorePct, P.CriticalScoreCap)
		}

		// Tanpa kritikal, total penalti tidak boleh melebihi kap minor.
		oMinor := out
		oMinor.MisconceptionCodes = []string{"M-MINOR-1", "M-MINOR-1", "M-MINOR-2"}
		minor := ScoreAnswer(concepts, oMinor, map[string]string{"M-MINOR-1": "minor", "M-MINOR-2": "minor"}, P)
		if minor.Penalties.Points > P.MinorPenaltyCap+1e-9 {
			t.Fatalf("penalti minor %v melebihi kap %v", minor.Penalties.Points, P.MinorPenaltyCap)
		}
	})
}

// TestPropW12_MonotonTerhadapCoverage memeriksa Property 12: menaikkan
// cakupan salah satu konsep (missed -> partial -> found) tidak pernah
// menurunkan skor akhir, dengan bobot positif dan hasil lengkap.
func TestPropW12_MonotonTerhadapCoverage(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		P, concepts, out, severity := drawScoringInput(t)
		urutan := map[string]int{"missed": 0, "partial": 1, "found": 2}

		for i := range concepts {
			for _, target := range []string{"partial", "found"} {
				if urutan[target] <= urutan[out.ConceptResults[i].Coverage] {
					continue
				}
				hasil := append([]ConceptCoverage(nil), out.ConceptResults...)
				hasil[i].Coverage = target
				o := out
				o.ConceptResults = hasil

				awal := ScoreAnswer(concepts, out, severity, P).ScorePct
				setelah := ScoreAnswer(concepts, o, severity, P).ScorePct
				if setelah < awal-1e-9 {
					t.Fatalf("konsep %d: menaikkan ke %s menurunkan skor %v -> %v",
						i, target, awal, setelah)
				}
			}
		}
	})
}

// drawScoringInput menghasilkan parameter yang valid (0 <= ClampMin <
// ClampMax <= 100), konsep berbobot positif, cakupan lengkap, dan severity
// miskonsepsi yang acak.
func drawScoringInput(t *rapid.T) (ScoringParams, []ConceptRef, EvaluatorSignal, map[string]string) {
	bawah := rapid.Float64Range(0, 40).Draw(t, "clamp_min")
	atas := rapid.Float64Range(bawah+1, 100).Draw(t, "clamp_max")
	P := ScoringParams{
		PartialCredit:           rapid.Float64Range(0, 1).Draw(t, "partial"),
		MinorPenalty:            rapid.Float64Range(0, 12).Draw(t, "minor_penalty"),
		MinorPenaltyCap:         rapid.Float64Range(0, 25).Draw(t, "minor_cap"),
		CriticalPenalty:         rapid.Float64Range(0, 20).Draw(t, "critical_penalty"),
		CriticalScoreCap:        rapid.Float64Range(bawah, atas).Draw(t, "critical_cap"),
		ClampMin:                bawah,
		ClampMax:                atas,
		HintDivergenceThreshold: rapid.Float64Range(0, 60).Draw(t, "hint_divergensi"),
		LowConfidenceThreshold:  rapid.Float64Range(0, 1).Draw(t, "ambang_keyakinan"),
	}

	n := rapid.IntRange(1, 5).Draw(t, "jumlah_konsep")
	concepts := make([]ConceptRef, 0, n)
	results := make([]ConceptCoverage, 0, n)
	for i := 0; i < n; i++ {
		kode := fmt.Sprintf("K%d", i)
		concepts = append(concepts, ConceptRef{
			Code:       kode,
			Weight:     rapid.Float64Range(0.1, 5).Draw(t, "bobot_"+kode),
			IsRequired: rapid.Bool().Draw(t, "wajib_"+kode),
		})
		results = append(results, ConceptCoverage{
			ConceptCode: kode,
			Coverage:    drawCoverage(t, "cakupan_"+kode),
		})
	}

	severity := map[string]string{}
	misconceptions := []string{}
	for i, m := 0, rapid.IntRange(0, 4).Draw(t, "jumlah_miskonsepsi"); i < m; i++ {
		kode := fmt.Sprintf("M%d", i)
		misconceptions = append(misconceptions, kode)
		severity[kode] = []string{"minor", "critical", "major"}[rapid.IntRange(0, 2).Draw(t, "severity_"+kode)]
	}

	out := EvaluatorSignal{
		ConceptResults:     results,
		MisconceptionCodes: misconceptions,
		Relevance:          "on_topic",
		ScoreHint:          rapid.Float64Range(-20, 120).Draw(t, "score_hint"),
		Confidence:         rapid.Float64Range(0, 1).Draw(t, "keyakinan"),
	}
	return P, concepts, out, severity
}

func drawCoverage(t *rapid.T, label string) string {
	return []string{"found", "partial", "missed"}[rapid.IntRange(0, 2).Draw(t, label)]
}

func salinSeverity(src map[string]string) map[string]string {
	out := make(map[string]string, len(src)+1)
	for k, v := range src {
		out[k] = v
	}
	return out
}

// TestScoreAnswerPembulatanSetengahKeAtas mengunci aturan pembulatan yang
// dipakai sebelum dibatasi ke rentang skor.
func TestScoreAnswerPembulatanSetengahKeAtas(t *testing.T) {
	if got := round2(1.125); math.Abs(got-1.13) > 1e-9 {
		t.Fatalf("round2(1.125) = %v, ingin 1.13", got)
	}
	if got := round2(-1.125); math.Abs(got+1.13) > 1e-9 {
		t.Fatalf("round2(-1.125) = %v, ingin -1.13", got)
	}
}
