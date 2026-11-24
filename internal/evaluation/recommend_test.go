package evaluation

import (
	"fmt"
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

// TestPropW19_RekomendasiDeterministikDanTerurut memeriksa Property 19:
// hasil deterministik, tidak memuat tema MASTERED, tiap tema paling banyak
// sekali, terurut berdasarkan prioritas, dipotong sesuai limit, dan setiap
// entri memiliki rule.code, rule.priority, rule.params, serta aksi yang valid.
func TestPropW19_RekomendasiDeterministikDanTerurut(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		topics, prereq, mastery, roadmap, threshold, limit := drawRekomendasiInput(t)

		got := Recommend(topics, prereq, mastery, roadmap, threshold, limit)
		lagi := Recommend(topics, prereq, mastery, roadmap, threshold, limit)
		if !reflect.DeepEqual(got, lagi) {
			t.Fatalf("hasil tidak deterministik: %+v vs %+v", got, lagi)
		}
		if len(got) > limit {
			t.Fatalf("hasil %d melebihi limit %d", len(got), limit)
		}

		valid := map[string]bool{}
		for _, topic := range topics {
			valid[topic.ID] = true
		}
		terlihat := map[string]bool{}
		sebelumnya := 0
		for i, rec := range got {
			if !valid[rec.TopicID] {
				t.Fatalf("tema %q tidak ada dalam masukan", rec.TopicID)
			}
			if terlihat[rec.TopicID] {
				t.Fatalf("tema %q muncul lebih dari sekali", rec.TopicID)
			}
			terlihat[rec.TopicID] = true
			if rekSudahDikuasai(rec.TopicID, mastery) {
				t.Fatalf("tema MASTERED ikut direkomendasikan: %q", rec.TopicID)
			}

			kode, ok := rec.Rule["code"].(string)
			if !ok || !rekKodeValid(kode) {
				t.Fatalf("rule.code tidak valid: %v", rec.Rule["code"])
			}
			prioritas, ok := rec.Rule["priority"].(int)
			if !ok || prioritas < 1 || prioritas > 4 {
				t.Fatalf("rule.priority tidak valid: %v", rec.Rule["priority"])
			}
			params, ok := rec.Rule["params"].(map[string]any)
			if !ok || params == nil {
				t.Fatalf("rule.params tidak valid: %v", rec.Rule["params"])
			}
			if !rekAksiValid(rec.Action) {
				t.Fatalf("aksi tidak valid: %q", rec.Action)
			}
			if i > 0 && prioritas < sebelumnya {
				t.Fatalf("prioritas menurun: %d menjadi %d", sebelumnya, prioritas)
			}
			sebelumnya = prioritas
		}
	})
}

// TestRekomendasiPrioritasDanAksi mengunci urutan aturan dan aksi untuk
// kasus yang memuat prasyarat, penguasaan rendah, dan peta jalan.
func TestRekomendasiPrioritasDanAksi(t *testing.T) {
	topics := []RecommendTopic{
		{ID: "T0", Position: 0},
		{ID: "T1", Position: 1},
		{ID: "T2", Position: 2},
		{ID: "T3", Position: 3},
		{ID: "T4", Position: 4},
	}
	prereq := map[string][]string{"T2": {"T0"}, "T3": {"T2"}}
	avg := 30.0
	mastery := map[string]MasteryInfo{
		"T0": {Status: "mastered"},
		"T1": {Status: "MASTERED"},
		"T2": {Status: "needs_review"},
		"T3": {Status: "in_progress", AvgLast3: &avg},
	}
	got := Recommend(topics, prereq, mastery, [][]string{{"T4", "T1"}}, 60, 10)
	if len(got) != 3 {
		t.Fatalf("jumlah rekomendasi %d, ingin 3: %+v", len(got), got)
	}
	ingin := []struct {
		id, kode, aksi string
		prioritas      int
	}{
		{"T3", rekAturanPrasyarat, "take_assessment", 1},
		{"T2", rekAturanPenguasaan, "review", 2},
		{"T4", rekAturanPetaJalan, "study_material", 3},
	}
	for i, want := range ingin {
		rec := got[i]
		if rec.TopicID != want.id || rec.Action != want.aksi ||
			rec.Rule["code"] != want.kode || rec.Rule["priority"] != want.prioritas {
			t.Fatalf("rekomendasi ke-%d = %+v, ingin %+v", i, rec, want)
		}
		if _, ok := rec.Rule["params"].(map[string]any); !ok {
			t.Fatalf("rekomendasi ke-%d tanpa params: %+v", i, rec)
		}
	}
}

// TestRekomendasiUrutanKurikulum memastikan tema tanpa peta jalan mendapat
// aturan CURRICULUM_ORDER dan pengurut memakai posisi terkecil saat limit
// memotong hasil.
func TestRekomendasiUrutanKurikulum(t *testing.T) {
	topics := []RecommendTopic{{ID: "A", Position: 5}, {ID: "B", Position: 2}}
	got := Recommend(topics, nil, nil, nil, 60, 1)
	if len(got) != 1 || got[0].TopicID != "B" ||
		got[0].Rule["code"] != rekAturanKurikulum || got[0].Rule["priority"] != 4 ||
		got[0].Action != "study_material" {
		t.Fatalf("rekomendasi kurikulum tidak sesuai: %+v", got)
	}
}

func rekKodeValid(kode string) bool {
	switch kode {
	case rekAturanPrasyarat, rekAturanPenguasaan, rekAturanPetaJalan, rekAturanKurikulum:
		return true
	}
	return false
}

func rekAksiValid(aksi string) bool {
	switch aksi {
	case "study_material", "take_assessment", "review":
		return true
	}
	return false
}

func rekPtrFloat(x float64) *float64 { return &x }

func drawRekomendasiInput(t *rapid.T) ([]RecommendTopic, map[string][]string, map[string]MasteryInfo, [][]string, float64, int) {
	n := rapid.IntRange(1, 8).Draw(t, "w19_jumlah_tema")
	topics := make([]RecommendTopic, 0, n)
	ids := make([]string, 0, n+1)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("T%d", i)
		ids = append(ids, id)
		topics = append(topics, RecommendTopic{
			ID:       id,
			Slug:     "tema-" + id,
			Position: rapid.IntRange(0, 50).Draw(t, "w19_posisi_"+id),
		})
	}
	ids = append(ids, "TX") // tema di luar daftar yang boleh muncul sebagai prasyarat

	prereq := map[string][]string{}
	for _, topic := range topics {
		for i, jumlah := 0, rapid.IntRange(0, 3).Draw(t, "w19_jumlah_prasyarat_"+topic.ID); i < jumlah; i++ {
			prereq[topic.ID] = append(prereq[topic.ID],
				ids[rapid.IntRange(0, len(ids)-1).Draw(t, fmt.Sprintf("w19_prasyarat_%s_%d", topic.ID, i))])
		}
	}

	mastery := map[string]MasteryInfo{}
	statuses := []string{"mastered", "needs_review", "in_progress", "practicing", "not_started"}
	for _, topic := range topics {
		if !rapid.Bool().Draw(t, "w19_ada_penguasaan_"+topic.ID) {
			continue // tidak ada entri = NOT_STARTED
		}
		info := MasteryInfo{Status: statuses[rapid.IntRange(0, len(statuses)-1).Draw(t, "w19_status_"+topic.ID)]}
		if rapid.Bool().Draw(t, "w19_ada_rata_"+topic.ID) {
			info.AvgLast3 = rekPtrFloat(rapid.Float64Range(0, 100).Draw(t, "w19_rata_"+topic.ID))
		}
		mastery[topic.ID] = info
	}

	roadmap := make([][]string, 0)
	for i, jumlahPeta := 0, rapid.IntRange(0, 3).Draw(t, "w19_jumlah_peta"); i < jumlahPeta; i++ {
		order := make([]string, 0)
		for j, panjang := 0, rapid.IntRange(0, n+1).Draw(t, fmt.Sprintf("w19_panjang_peta_%d", i)); j < panjang; j++ {
			order = append(order, ids[rapid.IntRange(0, len(ids)-1).Draw(t, fmt.Sprintf("w19_peta_%d_%d", i, j))])
		}
		roadmap = append(roadmap, order)
	}

	threshold := rapid.Float64Range(0, 100).Draw(t, "w19_ambang")
	limit := rapid.IntRange(0, n+2).Draw(t, "w19_limit")
	return topics, prereq, mastery, roadmap, threshold, limit
}
