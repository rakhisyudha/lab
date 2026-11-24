package evaluation

import "time"

// MisconceptionRecord adalah keadaan satu kode miskonsepsi pada pemelajar.
// Status salah satu dari "detected" atau "resolved".
type MisconceptionRecord struct {
	Code                    string
	Status                  string
	Occurrences             int
	ConsecutiveResolveCount int
	DetectedAt              time.Time
	ResolvedAt              *time.Time
	SourceEvaluationID      string
	ResolvedByEvaluationID  string
}

// EvalSignalEvent adalah sinyal dari satu evaluasi. Kode pada DetectedCodes
// diproses lebih dulu daripada kode pada ResolvedCodes.
type EvalSignalEvent struct {
	ID            string
	DetectedCodes []string
	ResolvedCodes []string
	CreatedAt     time.Time
}

// ApplyMisconceptionSignals menerapkan deretan sinyal secara murni dan
// mengembalikan peta keadaan yang baru; peta masukan tidak diubah. Deteksi
// membuka kembali rekaman yang sudah terselesaikan dan mengulang hitungan
// resolusi berturut-turut. Resolusi hanya mengubah rekaman yang sedang
// terdeteksi, setelah jumlah laporan berturut-turut mencapai required.
func ApplyMisconceptionSignals(state map[string]MisconceptionRecord, events []EvalSignalEvent, required int) map[string]MisconceptionRecord {
	next := make(map[string]MisconceptionRecord, len(state)+len(events))
	for kode, rec := range state {
		next[kode] = rec
	}

	for _, ev := range events {
		for _, kode := range ev.DetectedCodes {
			rec, ada := next[kode]
			if !ada {
				next[kode] = MisconceptionRecord{
					Code:                    kode,
					Status:                  "detected",
					Occurrences:             1,
					ConsecutiveResolveCount: 0,
					DetectedAt:              ev.CreatedAt,
					SourceEvaluationID:      ev.ID,
				}
				continue
			}
			rec.Status = "detected"
			rec.ResolvedAt = nil
			rec.ResolvedByEvaluationID = ""
			rec.Occurrences++
			rec.ConsecutiveResolveCount = 0
			rec.SourceEvaluationID = ev.ID
			// DetectedAt sengaja dipertahankan: Occurrences menghitung
			// kemunculan, bukan mengganti waktu kemunculan pertama.
			next[kode] = rec
		}

		for _, kode := range ev.ResolvedCodes {
			rec, ada := next[kode]
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
			next[kode] = rec
		}
	}
	return next
}
