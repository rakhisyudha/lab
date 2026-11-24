package evaluation

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestPropW14_MiskonsepsiSesuaiModelAcuan memeriksa Property 14:
// ApplyMisconceptionSignals setara dengan model acuan sederhana, deteksi
// membuka kembali rekaman dengan occurrences+1 dan hitungan resolusi nol,
// resolusi baru terjadi setelah required laporan berturut-turut, dan peta
// masukan tidak berubah.
func TestPropW14_MiskonsepsiSesuaiModelAcuan(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		state, events, required := drawMiskonsepsiInput(t)
		sebelum := salinMiskonsepsi(state)

		got := ApplyMisconceptionSignals(state, events, required)
		ingin := referensiMiskonsepsi(state, events, required)
		if !reflect.DeepEqual(got, ingin) {
			t.Fatalf("hasil %+v berbeda dari model acuan %+v", got, ingin)
		}
		if !reflect.DeepEqual(state, sebelum) {
			t.Fatalf("peta masukan berubah: %+v menjadi %+v", sebelum, state)
		}

		for kode, rec := range got {
			if rec.Code != kode {
				t.Fatalf("kode rekaman %q tidak cocok dengan kunci %q", rec.Code, kode)
			}
			if rec.Occurrences < 1 || rec.ConsecutiveResolveCount < 0 {
				t.Fatalf("rekaman %q memiliki hitungan negatif: %+v", kode, rec)
			}
			if rec.Status == "detected" && (rec.ResolvedAt != nil || rec.ResolvedByEvaluationID != "") {
				t.Fatalf("rekaman terdeteksi masih memiliki data resolusi: %+v", rec)
			}
			if rec.Status == "resolved" &&
				(rec.ResolvedAt == nil || rec.ConsecutiveResolveCount < required || rec.ResolvedByEvaluationID == "") {
				t.Fatalf("rekaman terselesaikan tidak valid: %+v (required=%d)", rec, required)
			}
		}
	})
}

// TestPropW14_AmbangDanReopen mengunci perilaku ambang resolusi dan
// pembukaan kembali secara eksplisit.
func TestPropW14_AmbangDanReopen(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		required := rapid.IntRange(1, 4).Draw(t, "w14b_required")
		mulai := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		state := map[string]MisconceptionRecord{}

		resolusi := make([]EvalSignalEvent, 0, required)
		for i := 0; i < required; i++ {
			resolusi = append(resolusi, EvalSignalEvent{
				ID:            fmt.Sprintf("R%d", i),
				ResolvedCodes: []string{"MX"},
				CreatedAt:     mulai.Add(time.Duration(i) * time.Hour),
			})
		}

		// Tanpa deteksi, laporan resolusi diabaikan.
		if partial := ApplyMisconceptionSignals(state, resolusi[:required-1], required); len(partial) != 0 {
			t.Fatalf("resolusi tanpa rekaman justru membuat entri: %+v", partial)
		}

		// Deteksi lalu sebanyak required laporan resolusi menyelesaikan rekaman.
		urutan := append([]EvalSignalEvent{{
			ID:            "D0",
			DetectedCodes: []string{"MX"},
			CreatedAt:     mulai,
		}}, resolusi...)
		terselesaikan := ApplyMisconceptionSignals(state, urutan, required)
		rec, ada := terselesaikan["MX"]
		if !ada || rec.Status != "resolved" || rec.ResolvedAt == nil ||
			rec.ConsecutiveResolveCount != required || rec.ResolvedByEvaluationID != fmt.Sprintf("R%d", required-1) {
			t.Fatalf("resolusi setelah %d laporan tidak valid: %+v", required, rec)
		}

		sebelumReopen := salinMiskonsepsi(terselesaikan)
		dibuka := ApplyMisconceptionSignals(terselesaikan, []EvalSignalEvent{{
			ID:            "D1",
			DetectedCodes: []string{"MX"},
			CreatedAt:     mulai.Add(24 * time.Hour),
		}}, required)
		reopen := dibuka["MX"]
		if reopen.Status != "detected" || reopen.Occurrences != rec.Occurrences+1 ||
			reopen.ConsecutiveResolveCount != 0 || reopen.ResolvedAt != nil ||
			reopen.ResolvedByEvaluationID != "" || reopen.SourceEvaluationID != "D1" ||
			!reopen.DetectedAt.Equal(rec.DetectedAt) {
			t.Fatalf("pembukaan kembali tidak mengulang hitungan: %+v", reopen)
		}
		if !reflect.DeepEqual(terselesaikan, sebelumReopen) {
			t.Fatal("peta masukan berubah saat dibuka kembali")
		}
	})
}

// referensiMiskonsepsi adalah model acuan sederhana yang ditulis ulang
// secara langsung tanpa memakai fungsi produksi.
func referensiMiskonsepsi(state map[string]MisconceptionRecord, events []EvalSignalEvent, required int) map[string]MisconceptionRecord {
	out := salinMiskonsepsi(state)
	for _, ev := range events {
		for _, kode := range ev.DetectedCodes {
			rec, ada := out[kode]
			if !ada {
				out[kode] = MisconceptionRecord{
					Code:               kode,
					Status:             "detected",
					Occurrences:        1,
					DetectedAt:         ev.CreatedAt,
					SourceEvaluationID: ev.ID,
				}
				continue
			}
			rec.Status = "detected"
			rec.ResolvedAt = nil
			rec.ResolvedByEvaluationID = ""
			rec.Occurrences++
			rec.ConsecutiveResolveCount = 0
			rec.SourceEvaluationID = ev.ID
			out[kode] = rec
		}
		for _, kode := range ev.ResolvedCodes {
			rec, ada := out[kode]
			if !ada || rec.Status != "detected" {
				continue
			}
			rec.ConsecutiveResolveCount++
			if rec.ConsecutiveResolveCount >= required {
				waktu := ev.CreatedAt
				rec.Status = "resolved"
				rec.ResolvedAt = &waktu
				rec.ResolvedByEvaluationID = ev.ID
			}
			out[kode] = rec
		}
	}
	return out
}

// salinMiskonsepsi menyalin peta keadaan supaya referensi dan pemeriksaan
// kemurnian tidak berbagi struktur dengan masukan.
func salinMiskonsepsi(src map[string]MisconceptionRecord) map[string]MisconceptionRecord {
	out := make(map[string]MisconceptionRecord, len(src))
	for kode, rec := range src {
		out[kode] = rec
	}
	return out
}

func drawMiskonsepsiInput(t *rapid.T) (map[string]MisconceptionRecord, []EvalSignalEvent, int) {
	required := rapid.IntRange(1, 4).Draw(t, "w14_required")
	state := map[string]MisconceptionRecord{}
	for i, n := 0, rapid.IntRange(0, 4).Draw(t, "w14_jumlah_record"); i < n; i++ {
		kode := fmt.Sprintf("M%d", i)
		waktu := waktuMiskonsepsi(t, "w14_waktu_record_"+kode)
		rec := MisconceptionRecord{
			Code:                    kode,
			Status:                  []string{"detected", "resolved"}[rapid.IntRange(0, 1).Draw(t, "w14_status_"+kode)],
			Occurrences:             rapid.IntRange(1, 3).Draw(t, "w14_occ_"+kode),
			ConsecutiveResolveCount: rapid.IntRange(0, 2).Draw(t, "w14_count_"+kode),
			DetectedAt:              waktu.Add(-time.Hour),
			SourceEvaluationID:      "S-" + kode,
		}
		if rec.Status == "resolved" {
			selesai := waktu
			rec.ResolvedAt = &selesai
			rec.ResolvedByEvaluationID = "R-" + kode
			rec.ConsecutiveResolveCount = required + rapid.IntRange(0, 2).Draw(t, "w14_count_resolved_"+kode)
		}
		state[kode] = rec
	}

	events := make([]EvalSignalEvent, 0)
	for i, n := 0, rapid.IntRange(0, 6).Draw(t, "w14_jumlah_event"); i < n; i++ {
		label := fmt.Sprintf("%d", i)
		events = append(events, EvalSignalEvent{
			ID:            "E" + label,
			DetectedCodes: kodeMiskonsepsi(t, "w14_deteksi_"+label),
			ResolvedCodes: kodeMiskonsepsi(t, "w14_resolusi_"+label),
			CreatedAt:     waktuMiskonsepsi(t, "w14_waktu_event_"+label),
		})
	}
	return state, events, required
}

func kodeMiskonsepsi(t *rapid.T, label string) []string {
	pilihan := []string{"M0", "M1", "M2", "M3", "MX"}
	kode := make([]string, 0)
	for i, n := 0, rapid.IntRange(0, 3).Draw(t, label+"_jumlah"); i < n; i++ {
		kode = append(kode, pilihan[rapid.IntRange(0, len(pilihan)-1).Draw(t, fmt.Sprintf("%s_%d", label, i))])
	}
	return kode
}

func waktuMiskonsepsi(t *rapid.T, label string) time.Time {
	return time.Unix(int64(rapid.IntRange(0, 100000).Draw(t, label)), 0).UTC()
}
