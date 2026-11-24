package evaluation

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Pilihan nilai acak untuk property test sesi (desain 4.6).
var (
	sesiUjiAksi   = []SessionAction{"start", "pause", "resume", "finish", "abandon"}
	sesiUjiStatus = []string{"CREATED", "IN_PROGRESS", "PAUSED", "COMPLETED", "ABANDONED"}
	sesiUjiPhase  = []string{"awaiting_start", "awaiting_answer", "evaluating", "generating_followup", "finalizing", "done"}
)

// sesiUjiSalin menyalin state secara mandiri di dalam test, terpisah dari
// salinan produksi, supaya pemeriksaan kemurnian tidak bergantung pada
// implementasi yang diuji.
func sesiUjiSalin(s SessionState) SessionState {
	salin := s
	if s.StartedAt != nil {
		v := *s.StartedAt
		salin.StartedAt = &v
	}
	if s.PausedAt != nil {
		v := *s.PausedAt
		salin.PausedAt = &v
	}
	if s.ExpiresAt != nil {
		v := *s.ExpiresAt
		salin.ExpiresAt = &v
	}
	if s.AbandonedAt != nil {
		v := *s.AbandonedAt
		salin.AbandonedAt = &v
	}
	return salin
}

// sesiUjiTTL adalah oracle mandiri untuk lama jeda, termasuk bawaan 7 hari.
func sesiUjiTTL(P SessionParams) time.Duration {
	hari := P.PauseTTLDays
	if hari <= 0 {
		hari = 7
	}
	return time.Duration(hari) * 24 * time.Hour
}

// sesiUjiParam mengambil parameter acak, sesekali memakai nilai bawaan.
func sesiUjiParam(t *rapid.T, label string) SessionParams {
	return SessionParams{
		PauseTTLDays: []int{0, -2, 1, 2, 7, 14}[rapid.IntRange(0, 5).Draw(t, label)],
	}
}

// sesiUjiReferensi adalah model state machine mandiri sesuai desain 4.6.
func sesiUjiReferensi(s SessionState, a SessionAction, now time.Time, P SessionParams) TransitionResult {
	hasil := TransitionResult{Session: sesiUjiSalin(s)}

	switch {
	case s.Status == "CREATED" && a == "start":
		hasil.Session.Status = "IN_PROGRESS"
		waktu := now
		hasil.Session.StartedAt = &waktu
		hasil.Session.Phase = "awaiting_answer"

	case s.Status == "IN_PROGRESS" && a == "pause":
		hasil.Session.Status = "PAUSED"
		waktu := now
		hasil.Session.PausedAt = &waktu
		kedaluwarsa := now.Add(sesiUjiTTL(P))
		hasil.Session.ExpiresAt = &kedaluwarsa

	case s.Status == "PAUSED" && a == "resume":
		if s.ExpiresAt != nil && !s.ExpiresAt.After(now) {
			hasil.Session.Status = "ABANDONED"
			waktu := now
			hasil.Session.AbandonedAt = &waktu
			hasil.ErrorCode = "session_expired"
		} else {
			hasil.Session.Status = "IN_PROGRESS"
			hasil.Session.PausedAt = nil
			hasil.Session.ExpiresAt = nil
		}

	case s.Status == "IN_PROGRESS" && a == "finish":
		hasil.Session.Phase = "finalizing"

	case (s.Status == "CREATED" || s.Status == "IN_PROGRESS" || s.Status == "PAUSED") && a == "abandon":
		hasil.Session.Status = "ABANDONED"
		waktu := now
		hasil.Session.AbandonedAt = &waktu

	default:
		hasil.ErrorCode = "invalid_session_transition"
	}
	return hasil
}

// sesiUjiCekTransisi memeriksa invarian transisi secara eksplisit, tanpa
// bertumpu pada model referensi di atas.
func sesiUjiCekTransisi(t *rapid.T, sebelum SessionState, a SessionAction, now time.Time, P SessionParams, hasil TransitionResult) {
	valid := false
	switch {
	case sebelum.Status == "CREATED" && a == "start",
		sebelum.Status == "IN_PROGRESS" && a == "pause",
		sebelum.Status == "PAUSED" && a == "resume",
		sebelum.Status == "IN_PROGRESS" && a == "finish",
		(sebelum.Status == "CREATED" || sebelum.Status == "IN_PROGRESS" || sebelum.Status == "PAUSED") && a == "abandon":
		valid = true
	}
	// COMPLETED dan ABANDONED tidak pernah punya transisi keluar.
	if sebelum.Status == "COMPLETED" || sebelum.Status == "ABANDONED" {
		valid = false
	}

	if !valid {
		if hasil.ErrorCode != "invalid_session_transition" {
			t.Fatalf("aksi %q pada status %s: error_code=%q, ingin invalid_session_transition", a, sebelum.Status, hasil.ErrorCode)
		}
		if !reflect.DeepEqual(hasil.Session, sebelum) {
			t.Fatalf("transisi ditolak tetapi sesi berubah: %+v -> %+v", sebelum, hasil.Session)
		}
		return
	}
	if hasil.ErrorCode == "invalid_session_transition" {
		t.Fatalf("aksi %q pada status %s ditolak padahal valid", a, sebelum.Status)
	}

	switch a {
	case "start":
		if hasil.ErrorCode != "" || hasil.Session.Status != "IN_PROGRESS" || hasil.Session.Phase != "awaiting_answer" {
			t.Fatalf("start: hasil=%+v", hasil)
		}
		if hasil.Session.StartedAt == nil || !hasil.Session.StartedAt.Equal(now) {
			t.Fatalf("start: StartedAt=%v, ingin %v", hasil.Session.StartedAt, now)
		}

	case "pause":
		inginKedaluwarsa := now.Add(sesiUjiTTL(P))
		if hasil.ErrorCode != "" || hasil.Session.Status != "PAUSED" {
			t.Fatalf("pause: hasil=%+v", hasil)
		}
		if hasil.Session.PausedAt == nil || !hasil.Session.PausedAt.Equal(now) {
			t.Fatalf("pause: PausedAt=%v, ingin %v", hasil.Session.PausedAt, now)
		}
		if hasil.Session.ExpiresAt == nil || !hasil.Session.ExpiresAt.Equal(inginKedaluwarsa) {
			t.Fatalf("pause: ExpiresAt=%v, ingin %v", hasil.Session.ExpiresAt, inginKedaluwarsa)
		}

	case "resume":
		kedaluwarsa := sebelum.ExpiresAt != nil && !sebelum.ExpiresAt.After(now)
		if kedaluwarsa {
			if hasil.ErrorCode != "session_expired" || hasil.Session.Status != "ABANDONED" {
				t.Fatalf("resume lewat batas: hasil=%+v", hasil)
			}
			if hasil.Session.AbandonedAt == nil || !hasil.Session.AbandonedAt.Equal(now) {
				t.Fatalf("resume lewat batas: AbandonedAt=%v, ingin %v", hasil.Session.AbandonedAt, now)
			}
			if !reflect.DeepEqual(hasil.Session.PausedAt, sebelum.PausedAt) || !reflect.DeepEqual(hasil.Session.ExpiresAt, sebelum.ExpiresAt) {
				t.Fatalf("resume lewat batas mengubah PausedAt/ExpiresAt: %+v", hasil.Session)
			}
		} else {
			if hasil.ErrorCode != "" || hasil.Session.Status != "IN_PROGRESS" {
				t.Fatalf("resume sebelum batas: hasil=%+v", hasil)
			}
			if hasil.Session.PausedAt != nil || hasil.Session.ExpiresAt != nil {
				t.Fatalf("resume sebelum batas tidak mengosongkan PausedAt/ExpiresAt: %+v", hasil.Session)
			}
		}

	case "finish":
		if hasil.ErrorCode != "" || hasil.Session.Status != "IN_PROGRESS" || hasil.Session.Phase != "finalizing" {
			t.Fatalf("finish: hasil=%+v", hasil)
		}

	case "abandon":
		if hasil.ErrorCode != "" || hasil.Session.Status != "ABANDONED" {
			t.Fatalf("abandon: hasil=%+v", hasil)
		}
		if hasil.Session.AbandonedAt == nil || !hasil.Session.AbandonedAt.Equal(now) {
			t.Fatalf("abandon: AbandonedAt=%v, ingin %v", hasil.Session.AbandonedAt, now)
		}
	}

	// Field yang tidak boleh berubah untuk aksi apa pun.
	if a != "start" && !reflect.DeepEqual(hasil.Session.StartedAt, sebelum.StartedAt) {
		t.Fatalf("aksi %q mengubah StartedAt", a)
	}
	ubahAbandoned := a == "abandon" || (a == "resume" && hasil.ErrorCode == "session_expired")
	if !ubahAbandoned && !reflect.DeepEqual(hasil.Session.AbandonedAt, sebelum.AbandonedAt) {
		t.Fatalf("aksi %q mengubah AbandonedAt", a)
	}
	if a != "start" && a != "finish" && hasil.Session.Phase != sebelum.Phase {
		t.Fatalf("aksi %q mengubah Phase: %q -> %q", a, sebelum.Phase, hasil.Session.Phase)
	}
}

// Feature: backend-lab-technical-specs, Property 8: Transisi status Interview_Session hanya yang valid
// Kategori: Wajib
func TestPropW8_TransisiStatusInterviewSessionHanyaYangValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		P := sesiUjiParam(t, "w8_param")
		ttl := sesiUjiTTL(P)
		base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		now := base

		s := SessionState{
			Status: sesiUjiStatus[rapid.IntRange(0, len(sesiUjiStatus)-1).Draw(t, "w8_status_awal")],
			Phase:  sesiUjiPhase[rapid.IntRange(0, len(sesiUjiPhase)-1).Draw(t, "w8_phase_awal")],
		}
		if rapid.Bool().Draw(t, "w8_ada_started") {
			waktu := base.Add(-time.Duration(rapid.IntRange(1, 100).Draw(t, "w8_started_jam")) * time.Hour)
			s.StartedAt = &waktu
		}
		if s.Status == "PAUSED" {
			if rapid.Bool().Draw(t, "w8_ada_paused") {
				waktu := base.Add(-time.Hour)
				s.PausedAt = &waktu
			}
			if rapid.Bool().Draw(t, "w8_ada_expired") {
				waktu := base.Add(time.Duration(rapid.IntRange(-48, 48).Draw(t, "w8_expired_offset")) * time.Hour)
				s.ExpiresAt = &waktu
			}
		}

		jumlah := rapid.IntRange(1, 25).Draw(t, "w8_jumlah_aksi")
		for i := 0; i < jumlah; i++ {
			a := sesiUjiAksi[rapid.IntRange(0, len(sesiUjiAksi)-1).Draw(t, fmt.Sprintf("w8_aksi_%d", i))]

			// Waktu non-menurun; beberapa mode menyentuh tepat batas TTL.
			jam := 0
			switch rapid.IntRange(0, 4).Draw(t, fmt.Sprintf("w8_mode_waktu_%d", i)) {
			case 0:
				jam = 0
			case 1:
				jam = 1
			case 2:
				jam = int(ttl / time.Hour)
			case 3:
				jam = int(ttl/time.Hour) + 1
			default:
				jam = rapid.IntRange(0, 60).Draw(t, fmt.Sprintf("w8_jam_%d", i))
			}
			now = now.Add(time.Duration(jam) * time.Hour)

			sebelum := s
			snapshot := sesiUjiSalin(sebelum)
			hasil := Transition(s, a, now, P)
			ingin := sesiUjiReferensi(sebelum, a, now, P)
			if !reflect.DeepEqual(hasil, ingin) {
				t.Fatalf("langkah %d aksi %q status %s: hasil=%+v, ingin=%+v", i, a, sebelum.Status, hasil, ingin)
			}
			sesiUjiCekTransisi(t, sebelum, a, now, P, hasil)
			if !reflect.DeepEqual(sebelum, snapshot) {
				t.Fatalf("langkah %d: argumen Transition ikut termutasi", i)
			}
			s = hasil.Session
		}

		// Terminal: semua aksi pada COMPLETED/ABANDONED ditolak.
		for _, statusAkhir := range []string{"COMPLETED", "ABANDONED"} {
			terminal := SessionState{Status: statusAkhir, Phase: "done"}
			for _, a := range sesiUjiAksi {
				sebelum := terminal
				hasil := Transition(terminal, a, now, P)
				if hasil.ErrorCode != "invalid_session_transition" || !reflect.DeepEqual(hasil.Session, sebelum) {
					t.Fatalf("status terminal %s dengan aksi %q: hasil=%+v", statusAkhir, a, hasil)
				}
				sesiUjiCekTransisi(t, sebelum, a, now, P, hasil)
			}
		}
	})
}

// Feature: backend-lab-technical-specs, Property 9: Pause dan resume menjaga state dan batas 7 hari
// Kategori: Wajib
func TestPropW9_PauseDanResumeMenjagaStateDanBatas7Hari(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		P := sesiUjiParam(t, "w9_param")
		ttl := sesiUjiTTL(P)
		base := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)

		awal := SessionState{
			Status: "IN_PROGRESS",
			Phase:  sesiUjiPhase[rapid.IntRange(0, len(sesiUjiPhase)-1).Draw(t, "w9_phase_awal")],
		}
		waktuMulai := base.Add(-time.Duration(rapid.IntRange(1, 72).Draw(t, "w9_started_jam")) * time.Hour)
		awal.StartedAt = &waktuMulai
		snapshotAwal := sesiUjiSalin(awal)

		tPause := base.Add(time.Duration(rapid.IntRange(0, 48).Draw(t, "w9_jeda_pause")) * time.Hour)
		hasilPause := Transition(awal, "pause", tPause, P)
		if !reflect.DeepEqual(awal, snapshotAwal) {
			t.Fatal("pause mengubah argumen sesi")
		}
		if hasilPause.ErrorCode != "" || hasilPause.Session.Status != "PAUSED" {
			t.Fatalf("pause: hasil=%+v", hasilPause)
		}
		paused := hasilPause.Session
		if paused.PausedAt == nil || !paused.PausedAt.Equal(tPause) {
			t.Fatalf("pause: PausedAt=%v, ingin %v", paused.PausedAt, tPause)
		}
		expWant := tPause.Add(ttl)
		if paused.ExpiresAt == nil || !paused.ExpiresAt.Equal(expWant) {
			t.Fatalf("pause: ExpiresAt=%v, ingin %v", paused.ExpiresAt, expWant)
		}
		if paused.Phase != awal.Phase || !reflect.DeepEqual(paused.StartedAt, awal.StartedAt) {
			t.Fatalf("pause mengubah phase atau StartedAt: %+v", paused)
		}

		// Batas jeda: jauh sebelum, dekat sebelum, tepat 1 ns sebelum,
		// tepat pada, dan tepat 1 ns sesudah.
		waktuUji := []time.Time{
			tPause,
			expWant.Add(-time.Duration(rapid.IntRange(1, 200).Draw(t, "w9_jam_sebelum")) * time.Hour),
			expWant.Add(-time.Nanosecond),
			expWant,
			expWant.Add(time.Nanosecond),
		}
		for i, u := range waktuUji {
			sebelum := sesiUjiSalin(paused)
			hasil := Transition(paused, "resume", u, P)
			if !reflect.DeepEqual(paused, sebelum) {
				t.Fatalf("langkah %d: resume mengubah argumen sesi", i)
			}
			if u.Before(expWant) {
				if hasil.ErrorCode != "" || hasil.Session.Status != "IN_PROGRESS" {
					t.Fatalf("resume pada %v (< %v): hasil=%+v", u, expWant, hasil)
				}
				if hasil.Session.PausedAt != nil || hasil.Session.ExpiresAt != nil {
					t.Fatalf("resume sebelum batas tidak mengosongkan waktu jeda: %+v", hasil.Session)
				}
				if hasil.Session.Phase != paused.Phase || !reflect.DeepEqual(hasil.Session.StartedAt, paused.StartedAt) {
					t.Fatalf("resume mengubah phase atau StartedAt: %+v", hasil.Session)
				}
				if !reflect.DeepEqual(hasil.Session.AbandonedAt, paused.AbandonedAt) {
					t.Fatalf("resume mengubah AbandonedAt: %+v", hasil.Session)
				}
			} else {
				if hasil.ErrorCode != "session_expired" || hasil.Session.Status != "ABANDONED" {
					t.Fatalf("resume pada %v (>= %v): hasil=%+v", u, expWant, hasil)
				}
				if hasil.Session.AbandonedAt == nil || !hasil.Session.AbandonedAt.Equal(u) {
					t.Fatalf("resume kadaluwarsa: AbandonedAt=%v, ingin %v", hasil.Session.AbandonedAt, u)
				}
				if hasil.Session.Phase != paused.Phase || !reflect.DeepEqual(hasil.Session.StartedAt, paused.StartedAt) {
					t.Fatalf("resume kadaluwarsa mengubah phase atau StartedAt: %+v", hasil.Session)
				}
			}
		}

		// AbandonExpiredSessions hanya menyentuh PAUSED yang kadaluwarsa.
		nowJaga := tPause
		n := rapid.IntRange(1, 12).Draw(t, "w9_jumlah_sesi")
		daftar := make([]SessionState, 0, n)
		for i := 0; i < n; i++ {
			label := fmt.Sprintf("w9_sesi_%d_", i)
			sx := SessionState{
				Status: sesiUjiStatus[rapid.IntRange(0, len(sesiUjiStatus)-1).Draw(t, label+"status")],
				Phase:  sesiUjiPhase[rapid.IntRange(0, len(sesiUjiPhase)-1).Draw(t, label+"phase")],
			}
			switch rapid.IntRange(0, 3).Draw(t, label+"mode_expired") {
			case 0:
				// ExpiresAt kosong.
			case 1:
				waktu := nowJaga.Add(-time.Nanosecond)
				sx.ExpiresAt = &waktu
			case 2:
				waktu := nowJaga
				sx.ExpiresAt = &waktu
			default:
				waktu := nowJaga.Add(time.Nanosecond)
				sx.ExpiresAt = &waktu
			}
			daftar = append(daftar, sx)
		}

		snapshot := make([]SessionState, len(daftar))
		for i := range daftar {
			snapshot[i] = sesiUjiSalin(daftar[i])
		}
		hasilJaga := AbandonExpiredSessions(daftar, nowJaga)
		if len(hasilJaga) != len(daftar) {
			t.Fatalf("panjang hasil=%d, ingin %d", len(hasilJaga), len(daftar))
		}
		for i, asli := range snapshot {
			ingin := sesiUjiSalin(asli)
			if asli.Status == "PAUSED" && asli.ExpiresAt != nil && !asli.ExpiresAt.After(nowJaga) {
				ingin.Status = "ABANDONED"
				waktu := nowJaga
				ingin.AbandonedAt = &waktu
			}
			if !reflect.DeepEqual(hasilJaga[i], ingin) {
				t.Fatalf("sesi %d: hasil=%+v, ingin=%+v", i, hasilJaga[i], ingin)
			}
			if !reflect.DeepEqual(daftar[i], asli) {
				t.Fatalf("sesi %d: input ikut termutasi", i)
			}
		}
		if lagi := AbandonExpiredSessions(hasilJaga, nowJaga); !reflect.DeepEqual(lagi, hasilJaga) {
			t.Fatal("AbandonExpiredSessions tidak idempoten")
		}
		if AbandonExpiredSessions(nil, nowJaga) != nil {
			t.Fatal("slice nil harus menghasilkan nil")
		}
	})
}
