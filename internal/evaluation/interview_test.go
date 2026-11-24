package evaluation

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

// Feature: backend-lab-technical-specs, Property 17: Alokasi follow-up tidak melanggar batas interview dan tidak menghabiskan pertanyaan utama
// Kategori: Wajib
func TestPropW17_AlokasiFollowUpTidakMelanggarBatas(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := wawancaraAmbilConfig(t)
		P := wawancaraParamsFollowUp()
		state := FollowUpState{M: 0, T: 0, FollowupsByQuestion: map[string]int{}}
		pertanyaan := "Q0"
		kedalaman := 0

		for langkah := 0; langkah < 2*cfg.C+20 && state.M < cfg.N; langkah++ {
			wawancaraCekBatas(t, state, cfg)
			if kedalaman > cfg.D {
				t.Fatalf("kedalaman %d melebihi D=%d", kedalaman, cfg.D)
			}

			eval := wawancaraAmbilEval(t, fmt.Sprintf("w17_eval_%d", langkah))
			selesaiDiminta := rapid.Bool().Draw(t, fmt.Sprintf("w17_selesai_%d", langkah))
			if eval.Status != "scored" {
				selesaiDiminta = false // evaluasi gagal tidak boleh memicu follow-up
			}

			keputusan := AllocateFollowUp(cfg, state,
				AnsweredRef{QuestionID: pertanyaan, Depth: kedalaman}, eval, selesaiDiminta, P)

			if keputusan.AnsweredQuestionID != pertanyaan {
				t.Fatalf("AnsweredQuestionID = %q, ingin %q", keputusan.AnsweredQuestionID, pertanyaan)
			}
			if eval.Status != "scored" && keputusan.Ask {
				t.Fatalf("follow-up diajukan untuk evaluasi %q", eval.Status)
			}
			if keputusan.Ask {
				if state.T+1 > cfg.C {
					t.Fatalf("follow-up melewati total_cap: T=%d C=%d", state.T, cfg.C)
				}
				if state.T+1+(cfg.N-state.M) > cfg.C {
					t.Fatalf("follow-up menghabiskan slot pertanyaan utama: T=%d M=%d N=%d C=%d", state.T, state.M, cfg.N, cfg.C)
				}
				if state.FollowupsByQuestion[pertanyaan] >= cfg.F {
					t.Fatalf("follow-up melebihi F=%d untuk %q", cfg.F, pertanyaan)
				}
				if kedalaman >= cfg.D {
					t.Fatalf("follow-up melebihi D=%d", cfg.D)
				}
				if eval.Relevance == "off_topic" || eval.Relevance == "empty" {
					t.Fatalf("follow-up untuk jawaban %q", eval.Relevance)
				}
				if len(keputusan.FocusConcepts) == 0 && len(keputusan.FocusMisconceptions) == 0 {
					t.Fatalf("follow-up tanpa fokus: %+v", keputusan)
				}
			}

			sebelum := wawancaraSalinState(state)
			stateBaru, aksi := NextStep(state, cfg, keputusan)
			if !reflect.DeepEqual(state, sebelum) {
				t.Fatalf("NextStep mengubah state masukan: %+v menjadi %+v", sebelum, state)
			}
			wawancaraCekBatas(t, stateBaru, cfg)

			switch aksi {
			case "ask_followup":
				if stateBaru.T != state.T+1 || stateBaru.M != state.M {
					t.Fatalf("ask_followup: state %+v -> %+v", state, stateBaru)
				}
				if stateBaru.FollowupsByQuestion[pertanyaan] != state.FollowupsByQuestion[pertanyaan]+1 {
					t.Fatalf("hitung follow-up per pertanyaan tidak bertambah: %+v", stateBaru)
				}
				kedalaman++
				pertanyaan = fmt.Sprintf("%s-f%d", pertanyaan, kedalaman)
			case "next_main":
				if stateBaru.M != state.M+1 || stateBaru.T != state.T+1 {
					t.Fatalf("next_main: state %+v -> %+v", state, stateBaru)
				}
				kedalaman = 0
				pertanyaan = fmt.Sprintf("Q%d", stateBaru.M)
			case "finalize":
				t.Fatalf("finalize sebelum semua pertanyaan utama diajukan: M=%d N=%d", state.M, cfg.N)
			default:
				t.Fatalf("aksi tidak dikenal %q", aksi)
			}
			state = stateBaru
		}

		if state.M != cfg.N {
			t.Fatalf("tidak semua pertanyaan utama tercapai: M=%d N=%d", state.M, cfg.N)
		}

		// Finalisasi hanya mungkin setelah M >= N.
		terakhir := AllocateFollowUp(cfg, state,
			AnsweredRef{QuestionID: pertanyaan, Depth: kedalaman}, wawancaraEvalAman(), true, P)
		if terakhir.Ask {
			t.Fatalf("masih mengajukan follow-up setelah selesai: %+v", terakhir)
		}
		stateFinal, aksiFinal := NextStep(state, cfg, terakhir)
		if aksiFinal != "finalize" {
			t.Fatalf("aksi akhir %q, ingin finalize", aksiFinal)
		}
		if !reflect.DeepEqual(stateFinal, state) {
			t.Fatalf("finalize mengubah state: %+v menjadi %+v", state, stateFinal)
		}

		// Dengan finish sejak awal, seluruh pertanyaan utama tetap dapat
		// diajukan dan tidak ada slot yang dipakai follow-up.
		keadaan := FollowUpState{M: 0, T: 0, FollowupsByQuestion: map[string]int{}}
		for keadaan.M < cfg.N {
			keputusan := AllocateFollowUp(cfg, keadaan,
				AnsweredRef{QuestionID: "Q0", Depth: 0}, wawancaraEvalAman(), true, P)
			if keputusan.Ask {
				t.Fatalf("finish_requested menghasilkan follow-up: %+v", keputusan)
			}
			berikut, aksi := NextStep(keadaan, cfg, keputusan)
			if aksi != "next_main" {
				t.Fatalf("aksi %q, ingin next_main", aksi)
			}
			wawancaraCekBatas(t, berikut, cfg)
			keadaan = berikut
		}
		if keadaan.T != cfg.N {
			t.Fatalf("T=%d, ingin sebanyak pertanyaan utama N=%d", keadaan.T, cfg.N)
		}
		if _, aksi := NextStep(keadaan, cfg, FollowUpDecision{Reason: "finish_requested"}); aksi != "finalize" {
			t.Fatalf("aksi akhir %q, ingin finalize", aksi)
		}
	})
}

// Feature: backend-lab-technical-specs, Property 18: Skor interview adalah rata-rata berbobot yang di-clamp
// Kategori: Wajib
func TestPropW18_SkorInterviewRataRataBerbobotTerclamp(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		weights := make(map[string]float64, len(kunciDimensiInterview))
		for _, kunci := range kunciDimensiInterview {
			weights[kunci] = rapid.Float64Range(1, 40).Draw(t, "w18_bobot_"+kunci)
		}

		out := InterviewEvaluationOutput{
			DimensionScores: make(map[string]DimensionScore, len(kunciDimensiInterview)),
			Confidence:      rapid.Float64Range(0, 1).Draw(t, "w18_keyakinan"),
		}
		clamped := make(map[string]int, len(kunciDimensiInterview))
		minSkor, maxSkor := math.Inf(1), math.Inf(-1)
		for _, kunci := range kunciDimensiInterview {
			mentah := rapid.IntRange(-40, 140).Draw(t, "w18_skor_"+kunci)
			nilai := clampSkorInterview(mentah)
			out.DimensionScores[kunci] = DimensionScore{Score: mentah, Rationale: "alasan " + kunci}
			clamped[kunci] = nilai
			minSkor = math.Min(minSkor, float64(nilai))
			maxSkor = math.Max(maxSkor, float64(nilai))
		}
		jawaban := wawancaraAmbilSkorJawaban(t, "w18_jawaban")
		ambangRendah := rapid.Float64Range(0, 1).Draw(t, "w18_ambang_rendah")
		ambangSelisih := rapid.Float64Range(0, 60).Draw(t, "w18_ambang_selisih")

		got, err := ScoreInterview(out, weights, jawaban, ambangRendah, ambangSelisih)
		if err != nil {
			t.Fatalf("ScoreInterview mengembalikan error: %v", err)
		}

		pembilang, penyebut := 0.0, 0.0
		for _, kunci := range kunciDimensiInterview {
			pembilang += float64(clamped[kunci]) * weights[kunci]
			penyebut += weights[kunci]
		}
		ingin := round2(pembilang / penyebut)
		if math.Abs(got.OverallScore-ingin) > 1e-9 {
			t.Fatalf("OverallScore = %v, ingin rata-rata berbobot %v", got.OverallScore, ingin)
		}
		if got.OverallScore < minSkor-1e-9 || got.OverallScore > maxSkor+1e-9 {
			t.Fatalf("OverallScore %v di luar rentang skor ter-clamp [%v,%v]", got.OverallScore, minSkor, maxSkor)
		}
		if got.OverallScore < 0 || got.OverallScore > 100 {
			t.Fatalf("OverallScore %v di luar [0,100]", got.OverallScore)
		}
		if len(got.DimensionScores) != len(kunciDimensiInterview) {
			t.Fatalf("jumlah dimensi hasil %d, ingin %d", len(got.DimensionScores), len(kunciDimensiInterview))
		}
		for _, kunci := range kunciDimensiInterview {
			detail, ok := got.DimensionScores[kunci].(map[string]any)
			if !ok {
				t.Fatalf("detail dimensi %s bukan peta: %T", kunci, got.DimensionScores[kunci])
			}
			if detail["score"] != clamped[kunci] {
				t.Fatalf("dimensi %s: score %v, ingin %v", kunci, detail["score"], clamped[kunci])
			}
			if detail["weight"] != weights[kunci] {
				t.Fatalf("dimensi %s: weight %v, ingin %v", kunci, detail["weight"], weights[kunci])
			}
			if detail["rationale"] != "alasan "+kunci {
				t.Fatalf("dimensi %s: rationale %v", kunci, detail["rationale"])
			}
		}

		inginTinjauan := out.Confidence < ambangRendah
		if len(jawaban) > 0 {
			jumlah := 0.0
			for _, skor := range jawaban {
				jumlah += skor
			}
			rata := jumlah / float64(len(jawaban))
			if math.Abs(float64(clamped["technical_accuracy"])-rata) > ambangSelisih {
				inginTinjauan = true
			}
		}
		if got.NeedsReview != inginTinjauan {
			t.Fatalf("NeedsReview = %v, ingin %v", got.NeedsReview, inginTinjauan)
		}

		// Dimensi hilang harus menghasilkan error.
		tanpaDimensi := InterviewEvaluationOutput{
			DimensionScores: make(map[string]DimensionScore, len(out.DimensionScores)),
			Confidence:      out.Confidence,
		}
		for kunci, nilai := range out.DimensionScores {
			tanpaDimensi.DimensionScores[kunci] = nilai
		}
		hapusDimensi := kunciDimensiInterview[rapid.IntRange(0, 4).Draw(t, "w18_hapus_dimensi")]
		delete(tanpaDimensi.DimensionScores, hapusDimensi)
		if _, err := ScoreInterview(tanpaDimensi, weights, jawaban, ambangRendah, ambangSelisih); err == nil {
			t.Fatalf("dimensi %s hilang tidak menghasilkan error", hapusDimensi)
		}

		// Bobot hilang harus menghasilkan error.
		tanpaBobot := make(map[string]float64, len(weights))
		for kunci, nilai := range weights {
			tanpaBobot[kunci] = nilai
		}
		hapusBobot := kunciDimensiInterview[rapid.IntRange(0, 4).Draw(t, "w18_hapus_bobot")]
		delete(tanpaBobot, hapusBobot)
		if _, err := ScoreInterview(out, tanpaBobot, jawaban, ambangRendah, ambangSelisih); err == nil {
			t.Fatalf("bobot %s hilang tidak menghasilkan error", hapusBobot)
		}

		// Total bobot nol juga merupakan error.
		bobotNol := make(map[string]float64, len(kunciDimensiInterview))
		for _, kunci := range kunciDimensiInterview {
			bobotNol[kunci] = 0
		}
		if _, err := ScoreInterview(out, bobotNol, jawaban, ambangRendah, ambangSelisih); err == nil {
			t.Fatal("total bobot nol tidak menghasilkan error")
		}
	})
}

// Feature: backend-lab-technical-specs, Property 13: Agregasi Attempt berada di antara skor terkecil dan terbesar yang disertakan
// Kategori: Wajib
func TestPropW13Interview_AgregasiBeradaDiAntaraSkorRantai(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rantai := wawancaraAmbilRantai(t, "w13")
		awal := wawancaraSalinRantai(rantai)
		got := AggregateInterviewTopic(rantai, "A")
		ingin := wawancaraReferensi(rantai, "A")
		if !wawancaraSamaHasil(got, ingin) {
			t.Fatalf("agregasi = %s, ingin %s", wawancaraTulisHasil(got), wawancaraTulisHasil(ingin))
		}

		minSkor, maxSkor := math.Inf(1), math.Inf(-1)
		adaRantai := false
		for _, ch := range rantai {
			if ch.TopicID != "A" {
				continue
			}
			jumlah, banyak := 0.0, 0
			for _, jawaban := range ch.Answers {
				if jawaban.EvalStatus == "scored" && jawaban.ScorePct != nil {
					jumlah += *jawaban.ScorePct
					banyak++
				}
			}
			if banyak == 0 {
				continue
			}
			nilaiRantai := jumlah / float64(banyak)
			adaRantai = true
			minSkor = math.Min(minSkor, nilaiRantai)
			maxSkor = math.Max(maxSkor, nilaiRantai)
		}
		if !adaRantai {
			if got != nil {
				t.Fatalf("semua rantai tidak berskor namun hasilnya %s", wawancaraTulisHasil(got))
			}
		} else {
			if got == nil || got.Status != "scored" || got.ScorePct == nil {
				t.Fatalf("hasil bukan scored: %s", wawancaraTulisHasil(got))
			}
			if *got.ScorePct < minSkor-0.005 || *got.ScorePct > maxSkor+0.005 {
				t.Fatalf("skor %v di luar rentang rantai [%v,%v]", *got.ScorePct, minSkor, maxSkor)
			}
			if *got.ScorePct < 0 || *got.ScorePct > 100 {
				t.Fatalf("skor %v di luar [0,100]", *got.ScorePct)
			}
		}

		// Seluruh jawaban tidak berskor pada Topic -> hasil nil.
		semuaUnscored := wawancaraSalinRantai(rantai)
		for i := range semuaUnscored {
			for j := range semuaUnscored[i].Answers {
				semuaUnscored[i].Answers[j].EvalStatus = "unscored"
				semuaUnscored[i].Answers[j].ScorePct = nil
			}
		}
		if hasil := AggregateInterviewTopic(semuaUnscored, "A"); hasil != nil {
			t.Fatalf("semua unscored menghasilkan %s, ingin nil", wawancaraTulisHasil(hasil))
		}
		if hasil := AggregateInterviewTopic(nil, "A"); hasil != nil {
			t.Fatalf("rantai kosong menghasilkan %s, ingin nil", wawancaraTulisHasil(hasil))
		}

		// Jawaban gagal/unscored dengan skor ekstrem tidak mengubah hasil
		// rantai yang sudah memiliki jawaban berskor.
		ditambah := wawancaraSalinRantai(rantai)
		for i := range ditambah {
			ekstrem := float64(-1000 + i)
			ditambah[i].Answers = append(ditambah[i].Answers,
				ChainAnswer{EvalStatus: "failed", ScorePct: &ekstrem, Relevance: "on_topic"},
				ChainAnswer{EvalStatus: "unscored", ScorePct: &ekstrem, Relevance: "on_topic"},
			)
		}
		if hasil := AggregateInterviewTopic(ditambah, "A"); !wawancaraSamaHasil(hasil, ingin) {
			t.Fatalf("jawaban gagal mengubah hasil: %s menjadi %s", wawancaraTulisHasil(ingin), wawancaraTulisHasil(hasil))
		}
		if !reflect.DeepEqual(rantai, awal) {
			t.Fatal("AggregateInterviewTopic mengubah rantai masukan")
		}
	})
}

// Feature: backend-lab-technical-specs, Property 28: Skor Attempt hanya dari jawaban scored dan unscored tidak berpengaruh
// Kategori: Wajib
func TestPropW28Interview_KebalTerhadapJawabanUnscored(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rantai := wawancaraAmbilRantai(t, "w28")
		// Jamin ada satu rantai berskor pada Topic A sebagai dasar perbandingan.
		indeksA := -1
		adaBerskor := false
		for i := range rantai {
			if rantai[i].TopicID != "A" {
				continue
			}
			if indeksA < 0 {
				indeksA = i
			}
			for _, jawaban := range rantai[i].Answers {
				if jawaban.EvalStatus == "scored" && jawaban.ScorePct != nil {
					adaBerskor = true
					break
				}
			}
			if adaBerskor {
				break
			}
		}
		if indeksA < 0 {
			rantai[0].TopicID = "A"
			indeksA = 0
		}
		if !adaBerskor {
			nilai := rapid.Float64Range(0, 100).Draw(t, "w28_skor_paksa")
			rantai[indeksA].Answers[0] = ChainAnswer{EvalStatus: "scored", ScorePct: &nilai, Relevance: "on_topic"}
		}
		awal := wawancaraSalinRantai(rantai)
		dasar := AggregateInterviewTopic(rantai, "A")
		ingin := wawancaraReferensi(rantai, "A")
		if dasar == nil || !wawancaraSamaHasil(dasar, ingin) {
			t.Fatalf("dasar = %s, ingin %s", wawancaraTulisHasil(dasar), wawancaraTulisHasil(ingin))
		}

		// Membuang jawaban tidak berskor (indeks > 0) tidak mengubah hasil.
		tanpa := wawancaraSalinRantai(rantai)
		for i := range tanpa {
			bersih := make([]ChainAnswer, 0, len(tanpa[i].Answers))
			for j, jawaban := range tanpa[i].Answers {
				if j == 0 || (jawaban.EvalStatus == "scored" && jawaban.ScorePct != nil) {
					bersih = append(bersih, jawaban)
				}
			}
			tanpa[i].Answers = bersih
		}
		if hasil := AggregateInterviewTopic(tanpa, "A"); !wawancaraSamaHasil(hasil, dasar) {
			t.Fatalf("membuang jawaban unscored mengubah hasil: %s menjadi %s", wawancaraTulisHasil(dasar), wawancaraTulisHasil(hasil))
		}

		// Menambah jawaban unscored/failed dengan skor ekstrem tidak mengubah hasil.
		ditambah := wawancaraSalinRantai(rantai)
		for i := range ditambah {
			ekstrem := float64(1000 + i)
			ditambah[i].Answers = append(ditambah[i].Answers,
				ChainAnswer{EvalStatus: "unscored", ScorePct: &ekstrem, Relevance: "empty"},
				ChainAnswer{EvalStatus: "failed", ScorePct: &ekstrem, Relevance: "on_topic"},
			)
		}
		if hasil := AggregateInterviewTopic(ditambah, "A"); !wawancaraSamaHasil(hasil, dasar) {
			t.Fatalf("menambah jawaban unscored mengubah hasil: %s menjadi %s", wawancaraTulisHasil(dasar), wawancaraTulisHasil(hasil))
		}

		// Filter Topic: rantai topik lain tidak boleh memengaruhi topik A,
		// tetapi harus ikut dihitung saat topik tersebut yang diminta.
		bercampur := wawancaraSalinRantai(rantai)
		for i, nilai := range []float64{7, 93} {
			skor := nilai
			bercampur = append(bercampur, InterviewChain{
				QuestionID:     fmt.Sprintf("asing-%d", i),
				TopicID:        "B",
				Weight:         100,
				CognitiveLevel: "scenario",
				Answers: []ChainAnswer{
					{EvalStatus: "scored", ScorePct: &skor, Relevance: "on_topic"},
				},
			})
		}
		if hasil := AggregateInterviewTopic(bercampur, "A"); !wawancaraSamaHasil(hasil, dasar) {
			t.Fatalf("rantai topik B mengubah hasil topik A: %s menjadi %s", wawancaraTulisHasil(dasar), wawancaraTulisHasil(hasil))
		}
		hasilB := AggregateInterviewTopic(bercampur, "B")
		inginB := wawancaraReferensi(bercampur, "B")
		if hasilB == nil || !wawancaraSamaHasil(hasilB, inginB) {
			t.Fatalf("agregasi topik B = %s, ingin %s", wawancaraTulisHasil(hasilB), wawancaraTulisHasil(inginB))
		}
		if !reflect.DeepEqual(rantai, awal) {
			t.Fatal("AggregateInterviewTopic mengubah rantai masukan")
		}
	})
}

// wawancaraAmbilConfig menghasilkan profil valid dengan C >= N dan batas F, D
// yang mungkin nol (menguji konfigurasi paling ketat).
func wawancaraAmbilConfig(t *rapid.T) FollowUpConfig {
	n := rapid.IntRange(1, 6).Draw(t, "w17_N")
	return FollowUpConfig{
		N: n,
		C: n + rapid.IntRange(0, 6).Draw(t, "w17_tambahan_kapasitas"),
		F: rapid.IntRange(0, 3).Draw(t, "w17_F"),
		D: rapid.IntRange(0, 3).Draw(t, "w17_D"),
	}
}

// wawancaraParamsFollowUp memakai konstanta seed desain (30, 40, 20, trigger 70).
func wawancaraParamsFollowUp() FollowUpParams {
	return FollowUpParams{TriggerScore: 70, BaseCutoff: 30, ScarcityWeight: 40, CriticalBonus: 20}
}

// wawancaraEvalAman adalah evaluasi berskor, relevan, dan cukup tinggi sehingga
// tidak memicu follow-up.
func wawancaraEvalAman() EvalSignalInterview {
	return EvalSignalInterview{Status: "scored", ScorePct: 100, Relevance: "on_topic"}
}

// wawancaraCekBatas memeriksa invarian anggaran setelah setiap langkah.
func wawancaraCekBatas(t *rapid.T, state FollowUpState, cfg FollowUpConfig) {
	if state.T > cfg.C {
		t.Fatalf("T=%d melebihi C=%d", state.T, cfg.C)
	}
	if state.T+(cfg.N-state.M) > cfg.C {
		t.Fatalf("invarian anggaran dilanggar: T=%d M=%d N=%d C=%d", state.T, state.M, cfg.N, cfg.C)
	}
	if state.M > cfg.N || state.M < 0 {
		t.Fatalf("M=%d di luar rentang [0,%d]", state.M, cfg.N)
	}
	for kode, jumlah := range state.FollowupsByQuestion {
		if jumlah > cfg.F || jumlah < 0 {
			t.Fatalf("follow-up %q = %d, ingin dalam [0,%d]", kode, jumlah, cfg.F)
		}
	}
}

// wawancaraSalinState menyalin state beserta peta follow-upnya.
func wawancaraSalinState(state FollowUpState) FollowUpState {
	salin := FollowUpState{
		M:                   state.M,
		T:                   state.T,
		FollowupsByQuestion: make(map[string]int, len(state.FollowupsByQuestion)),
	}
	for kode, jumlah := range state.FollowupsByQuestion {
		salin.FollowupsByQuestion[kode] = jumlah
	}
	return salin
}

// wawancaraAmbilEval menghasilkan sinyal evaluasi dengan status, relevansi,
// cakupan konsep, dan miskonsepsi acak (termasuk kegagalan evaluasi).
func wawancaraAmbilEval(t *rapid.T, label string) EvalSignalInterview {
	status := []string{"scored", "scored", "scored", "failed", "unscored", "pending"}[rapid.IntRange(0, 5).Draw(t, label+"_status")]
	skor := rapid.Float64Range(0, 100).Draw(t, label+"_skor")
	relevansi := []string{"on_topic", "on_topic", "off_topic", "empty"}[rapid.IntRange(0, 3).Draw(t, label+"_relevansi")]

	n := rapid.IntRange(0, 4).Draw(t, label+"_konsep")
	konsep := make([]ConceptSignal, 0, n)
	for i := 0; i < n; i++ {
		awalan := fmt.Sprintf("%s_konsep_%d_", label, i)
		konsep = append(konsep, ConceptSignal{
			Code:       fmt.Sprintf("K%d", i),
			Coverage:   []string{"found", "partial", "missed"}[rapid.IntRange(0, 2).Draw(t, awalan+"cakupan")],
			IsRequired: rapid.Bool().Draw(t, awalan+"wajib"),
			Weight:     rapid.Float64Range(0.1, 5).Draw(t, awalan+"bobot"),
		})
	}

	m := rapid.IntRange(0, 3).Draw(t, label+"_miskonsepsi")
	miskonsepsi := make([]MisconceptionSignal, 0, m)
	for i := 0; i < m; i++ {
		miskonsepsi = append(miskonsepsi, MisconceptionSignal{
			Code:     fmt.Sprintf("M%d", i),
			Severity: []string{"minor", "major", "critical"}[rapid.IntRange(0, 2).Draw(t, fmt.Sprintf("%s_severity_%d", label, i))],
		})
	}

	return EvalSignalInterview{
		Status:                 status,
		ScorePct:               skor,
		Relevance:              relevansi,
		ConceptResults:         konsep,
		DetectedMisconceptions: miskonsepsi,
	}
}

// wawancaraAmbilSkorJawaban menghasilkan daftar skor jawaban, kadang kosong.
func wawancaraAmbilSkorJawaban(t *rapid.T, label string) []float64 {
	n := rapid.IntRange(0, 4).Draw(t, label+"_jumlah")
	out := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, rapid.Float64Range(0, 100).Draw(t, fmt.Sprintf("%s_%d", label, i)))
	}
	return out
}

// wawancaraAmbilRantai menghasilkan rantai untuk dua Topic ("A" dan "B").
func wawancaraAmbilRantai(t *rapid.T, label string) []InterviewChain {
	n := rapid.IntRange(1, 5).Draw(t, label+"_jumlah_rantai")
	rantai := make([]InterviewChain, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s_rantai_%d", label, i)
		topic := "A"
		if rapid.Bool().Draw(t, id+"_topik") {
			topic = "B"
		}
		jumlahJawaban := rapid.IntRange(1, 4).Draw(t, id+"_jumlah_jawaban")
		jawaban := make([]ChainAnswer, 0, jumlahJawaban)
		for j := 0; j < jumlahJawaban; j++ {
			awalan := fmt.Sprintf("%s_jawaban_%d_", id, j)
			status := []string{"scored", "scored", "unscored", "failed", "pending"}[rapid.IntRange(0, 4).Draw(t, awalan+"status")]
			var nilai *float64
			if status == "scored" {
				skor := rapid.Float64Range(0, 100).Draw(t, awalan+"skor")
				nilai = &skor
			}
			jawaban = append(jawaban, ChainAnswer{
				EvalStatus: status,
				ScorePct:   nilai,
				Relevance:  []string{"on_topic", "on_topic", "off_topic", "empty"}[rapid.IntRange(0, 3).Draw(t, awalan+"relevansi")],
			})
		}
		rantai = append(rantai, InterviewChain{
			QuestionID:     id,
			TopicID:        topic,
			Weight:         rapid.Float64Range(0.1, 5).Draw(t, id+"_bobot"),
			CognitiveLevel: []string{"application", "scenario", "remember", "understand"}[rapid.IntRange(0, 3).Draw(t, id+"_kognitif")],
			Answers:        jawaban,
		})
	}
	return rantai
}

// wawancaraSalinRantai menyalin rantai beserta irisan jawabannya.
func wawancaraSalinRantai(rantai []InterviewChain) []InterviewChain {
	salin := make([]InterviewChain, len(rantai))
	copy(salin, rantai)
	for i := range salin {
		salin[i].Answers = append([]ChainAnswer(nil), rantai[i].Answers...)
	}
	return salin
}

// wawancaraReferensi menghitung hasil agregasi yang diharapkan secara mandiri.
func wawancaraReferensi(rantai []InterviewChain, topicID string) *AggregateResult {
	skorRantai := make([]float64, 0, len(rantai))
	bobotRantai := make([]float64, 0, len(rantai))
	punyaAplikasi := false

	for _, ch := range rantai {
		if ch.TopicID != topicID {
			continue
		}
		jumlah, banyak := 0.0, 0
		for _, jawaban := range ch.Answers {
			if jawaban.EvalStatus == "scored" && jawaban.ScorePct != nil {
				jumlah += *jawaban.ScorePct
				banyak++
			}
		}
		if banyak == 0 {
			continue
		}
		skorRantai = append(skorRantai, jumlah/float64(banyak))
		bobotRantai = append(bobotRantai, ch.Weight)
		if len(ch.Answers) > 0 && ch.Answers[0].EvalStatus == "scored" && ch.Answers[0].Relevance != "empty" &&
			(ch.CognitiveLevel == "application" || ch.CognitiveLevel == "scenario") {
			punyaAplikasi = true
		}
	}
	if len(skorRantai) == 0 {
		return nil
	}

	pembilang, penyebut := 0.0, 0.0
	for i := range skorRantai {
		pembilang += bobotRantai[i] * skorRantai[i]
		penyebut += bobotRantai[i]
	}
	nilai := 0.0
	if penyebut != 0 {
		nilai = round2(pembilang / penyebut)
	}
	return &AggregateResult{
		Status:                 "scored",
		ScorePct:               &nilai,
		HasApplicationQuestion: punyaAplikasi,
	}
}

// wawancaraSamaHasil membandingkan dua hasil agregasi interview.
func wawancaraSamaHasil(a, b *AggregateResult) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Status != b.Status || a.HasApplicationQuestion != b.HasApplicationQuestion {
		return false
	}
	if (a.ScorePct == nil) != (b.ScorePct == nil) {
		return false
	}
	if a.ScorePct != nil && math.Abs(*a.ScorePct-*b.ScorePct) > 1e-12 {
		return false
	}
	return reflect.DeepEqual(a.UnscoredQuestionIDs, b.UnscoredQuestionIDs)
}

// wawancaraTulisHasil merupkan representasi teks hasil agregasi untuk pesan error.
func wawancaraTulisHasil(hasil *AggregateResult) string {
	if hasil == nil {
		return "nil"
	}
	skor := "nil"
	if hasil.ScorePct != nil {
		skor = fmt.Sprint(*hasil.ScorePct)
	}
	return fmt.Sprintf("{status:%s skor:%s aplikasi:%v}", hasil.Status, skor, hasil.HasApplicationQuestion)
}
