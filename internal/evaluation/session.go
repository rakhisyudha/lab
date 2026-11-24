package evaluation

import "time"

// SessionState adalah keadaan satu Interview_Session (desain 4.6).
// Status: CREATED | IN_PROGRESS | PAUSED | COMPLETED | ABANDONED.
// Phase: awaiting_start | awaiting_answer | evaluating | generating_followup |
// finalizing | done. COMPLETED dan ABANDONED bersifat terminal.
type SessionState struct {
	Status      string
	StartedAt   *time.Time
	PausedAt    *time.Time
	ExpiresAt   *time.Time
	AbandonedAt *time.Time
	Phase       string
}

// SessionAction adalah aksi yang dapat diterapkan pada SessionState:
// start | pause | resume | finish | abandon.
type SessionAction string

// SessionParams memuat lama jeda (pause) dalam hari. Nilai nol atau negatif
// memakai bawaan 7 hari.
type SessionParams struct {
	PauseTTLDays int
}

// TransitionResult adalah hasil Transition. ErrorCode kosong berarti transisi
// diterima; "invalid_session_transition" berarti sesi tidak diubah, sedangkan
// "session_expired" berarti sesi sudah diabadikan (perubahan harus
// disimpan oleh pemanggil).
type TransitionResult struct {
	Session   SessionState
	ErrorCode string // "" | invalid_session_transition | session_expired
}

// sesiSalinWaktu membuat salinan mandiri dari penunjuk waktu agar hasil
// fungsi tidak pernah berbagi alamat dengan argumen.
func sesiSalinWaktu(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	salin := *t
	return &salin
}

// sesiSalinState menyalin seluruh field, termasuk penunjuk waktu.
func sesiSalinState(s SessionState) SessionState {
	return SessionState{
		Status:      s.Status,
		StartedAt:   sesiSalinWaktu(s.StartedAt),
		PausedAt:    sesiSalinWaktu(s.PausedAt),
		ExpiresAt:   sesiSalinWaktu(s.ExpiresAt),
		AbandonedAt: sesiSalinWaktu(s.AbandonedAt),
		Phase:       s.Phase,
	}
}

// sesiTTL mengubah jumlah hari jeda menjadi Duration, dengan bawaan 7 hari.
func sesiTTL(P SessionParams) time.Duration {
	hari := P.PauseTTLDays
	if hari <= 0 {
		hari = 7
	}
	return time.Duration(hari) * 24 * time.Hour
}

// Transition menerapkan satu aksi pada sesi secara murni (desain 4.6).
// Pasangan yang tidak tercantum pada tabel transisi ditolak dengan
// "invalid_session_transition" dan keadaan dikembalikan apa adanya.
// Fungsi ini tidak pernah memutasi argumen.
func Transition(s SessionState, action SessionAction, now time.Time, P SessionParams) TransitionResult {
	hasil := TransitionResult{Session: sesiSalinState(s)}

	switch {
	case s.Status == "CREATED" && action == "start":
		hasil.Session.Status = "IN_PROGRESS"
		hasil.Session.StartedAt = sesiSalinWaktu(&now)
		hasil.Session.Phase = "awaiting_answer"

	case s.Status == "IN_PROGRESS" && action == "pause":
		hasil.Session.Status = "PAUSED"
		hasil.Session.PausedAt = sesiSalinWaktu(&now)
		kedaluwarsa := now.Add(sesiTTL(P))
		hasil.Session.ExpiresAt = &kedaluwarsa
		// Status, phase, dan pertanyaan berjalan tidak berubah.

	case s.Status == "PAUSED" && action == "resume":
		if s.ExpiresAt != nil && !s.ExpiresAt.After(now) {
			// Lewat batas jeda: sesi menjadi ABANDONED dan perubahan ini
			// tetap harus disimpan pemanggil meskipun ada kode galat.
			hasil.Session.Status = "ABANDONED"
			hasil.Session.AbandonedAt = sesiSalinWaktu(&now)
			hasil.ErrorCode = "session_expired"
		} else {
			hasil.Session.Status = "IN_PROGRESS"
			hasil.Session.PausedAt = nil
			hasil.Session.ExpiresAt = nil
		}

	case s.Status == "IN_PROGRESS" && action == "finish":
		hasil.Session.Phase = "finalizing"

	case (s.Status == "CREATED" || s.Status == "IN_PROGRESS" || s.Status == "PAUSED") && action == "abandon":
		hasil.Session.Status = "ABANDONED"
		hasil.Session.AbandonedAt = sesiSalinWaktu(&now)

	default:
		hasil.ErrorCode = "invalid_session_transition"
	}

	return hasil
}

// AbandonExpiredSessions mengabadikan setiap sesi PAUSED yang sudah melewati
// batas jeda (ExpiresAt <= now). Fungsi murni: input tidak diubah, keluaran
// adalah salinan baru, dan pemanggilan ulang bersifat idempoten.
func AbandonExpiredSessions(sessions []SessionState, now time.Time) []SessionState {
	if sessions == nil {
		return nil
	}

	hasil := make([]SessionState, len(sessions))
	for i, s := range sessions {
		hasil[i] = sesiSalinState(s)
		if hasil[i].Status == "PAUSED" && hasil[i].ExpiresAt != nil && !hasil[i].ExpiresAt.After(now) {
			hasil[i].Status = "ABANDONED"
			waktu := sesiSalinWaktu(&now)
			hasil[i].AbandonedAt = waktu
		}
	}
	return hasil
}
