package evaluation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestPropW29_SiklusStatusDanInvarian memeriksa Property 29: urutan kejadian
// acak dengan waktu yang tidak menurun hanya diterima pada transisi yang
// diizinkan, state terminal kebal terhadap kejadian apa pun, dan keempat
// invarian TaskState berlaku pada setiap langkah yang diterima.
func TestPropW29_SiklusStatusDanInvarian(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		maxAttempts := rapid.IntRange(1, 4).Draw(t, "maks_attempt")
		tugas := TaskState{
			Status:      statusQueued,
			Attempts:    0,
			MaxAttempts: maxAttempts,
			RunAfter:    now,
		}
		assertInvarianTugas(t, tugas)

		jenis := []string{eventDequeue, eventComplete, eventRequeue, eventFail, eventLockExpired, "invalid"}
		for langkah := 0; langkah < 40; langkah++ {
			dt := rapid.IntRange(0, 4).Draw(t, fmt.Sprintf("dt_%d", langkah))
			now = now.Add(time.Duration(dt) * time.Second)
			ev := TaskEvent{
				Type:         jenis[rapid.IntRange(0, len(jenis)-1).Draw(t, fmt.Sprintf("jenis_%d", langkah))],
				RunAfter:     now.Add(time.Duration(rapid.IntRange(0, 4).Draw(t, fmt.Sprintf("run_after_%d", langkah))) * time.Second),
				CountAttempt: rapid.Bool().Draw(t, fmt.Sprintf("hitung_%d", langkah)),
			}
			if rapid.Bool().Draw(t, fmt.Sprintf("ada_hasil_%d", langkah)) {
				ev.Result = json.RawMessage(fmt.Sprintf(`{"langkah":%d}`, langkah))
			}
			if rapid.Bool().Draw(t, fmt.Sprintf("ada_galat_%d", langkah)) {
				ev.ErrorCode = "galat_contoh"
			}

			// Model independen menentukan apakah transisi seharusnya diterima.
			harusDiterima := false
			switch tugas.Status {
			case statusQueued:
				harusDiterima = ev.Type == eventDequeue &&
					!tugas.RunAfter.After(now) && tugas.Attempts < tugas.MaxAttempts
			case statusProcessing:
				switch ev.Type {
				case eventComplete:
					harusDiterima = ev.Result != nil
				case eventRequeue, eventLockExpired:
					harusDiterima = true
				case eventFail:
					harusDiterima = ev.ErrorCode != ""
				}
			}

			got, err := NextTaskState(tugas, ev, now)
			if (err == nil) != harusDiterima {
				t.Fatalf("langkah %d status=%s event=%s: err=%v, ingin diterima=%v",
					langkah, tugas.Status, ev.Type, err, harusDiterima)
			}
			if err != nil {
				if err.Error() != errInvalidTaskTransition {
					t.Fatalf("pesan galat: %q, ingin %q", err.Error(), errInvalidTaskTransition)
				}
				if !reflect.DeepEqual(got, tugas) {
					t.Fatalf("keadaan berubah walaupun transisi ditolak: %+v", got)
				}
				continue
			}
			assertInvarianTugas(t, got)
			tugas = got
		}
	})
}

// TestNextTaskStateKadaluarsaDanTerminal menguji dua cabang lock_expired dan
// sifat kebal dari state terminal secara eksplisit.
func TestNextTaskStateKadaluarsaDanTerminal(t *testing.T) {
	now := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	locked := now.Add(-time.Minute)

	processing := TaskState{
		Status:      statusProcessing,
		Attempts:    1,
		MaxAttempts: 2,
		LockedAt:    &locked,
	}
	queued, err := NextTaskState(processing, TaskEvent{Type: eventLockExpired}, now)
	if err != nil || queued.Status != statusQueued || queued.LockedAt != nil || !queued.RunAfter.Equal(now) {
		t.Fatalf("lock_expired dengan sisa percobaan: %+v err=%v", queued, err)
	}

	processing.Attempts = 2
	failed, err := NextTaskState(processing, TaskEvent{Type: eventLockExpired}, now)
	if err != nil || failed.Status != statusFailed || failed.ErrorCode != errProviderUnavailable {
		t.Fatalf("lock_expired tanpa sisa percobaan: %+v err=%v", failed, err)
	}

	_, err = NextTaskState(failed, TaskEvent{Type: eventDequeue}, now)
	if err == nil {
		t.Fatal("state terminal harus menolak semua kejadian")
	}
	_, err = NextTaskState(TaskState{Status: statusCompleted, Result: json.RawMessage(`1`),
		MaxAttempts: 1, Attempts: 1}, TaskEvent{Type: eventRequeue}, now)
	if err == nil {
		t.Fatal("state completed harus menolak semua kejadian")
	}
}

func assertInvarianTugas(t *rapid.T, s TaskState) {
	if s.Attempts < 0 || s.Attempts > s.MaxAttempts {
		t.Fatalf("attempts %d di luar [0,%d]", s.Attempts, s.MaxAttempts)
	}
	if (s.Status == statusProcessing) != (s.LockedAt != nil) {
		t.Fatalf("status=%s tidak konsisten dengan locked_at=%v", s.Status, s.LockedAt)
	}
	if s.Status == statusCompleted && s.Result == nil {
		t.Fatal("completed harus memiliki result")
	}
	if s.Status == statusFailed && s.ErrorCode == "" {
		t.Fatal("failed harus memiliki error_code")
	}
}
