package evaluation

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// answerDenganTeks membungkus state dengan field teks buatan. Teks tidak
// pernah dikirim ke ApplyEvaluationEvent dan harus tetap sama setelah
// kejadian apa pun (Requirement 25.4).
type answerDenganTeks struct {
	Teks  string
	State AnswerEvalState
}

func drawParamEvaluasi(t *rapid.T) EvalParams {
	return EvalParams{
		MaxEvaluationRetries:       rapid.IntRange(0, 3).Draw(t, "max_evaluation_retries"),
		EvaluationRetryWindowHours: rapid.IntRange(1, 4).Draw(t, "jam_jendela_retry"),
		ClampMin:                   0,
	}
}

func drawStateEvaluasiAwal(t *rapid.T, P EvalParams, now time.Time) AnswerEvalState {
	status := []string{"", "pending", "scored", "failed", "unscored"}[rapid.IntRange(0, 4).Draw(t, "status_awal")]
	retry := rapid.IntRange(0, P.MaxEvaluationRetries).Draw(t, "retry_awal")
	var failedAt time.Time
	if status == "failed" {
		jamLalu := rapid.IntRange(0, 2*P.EvaluationRetryWindowHours+2).Draw(t, "jam_gagal_awal")
		failedAt = now.Add(-time.Duration(jamLalu) * time.Hour)
	}
	return AnswerEvalState{
		Status:     status,
		RetryCount: retry,
		FailedAt:   failedAt,
		IsBlank:    rapid.Bool().Draw(t, "jawaban_kosong"),
	}
}

func drawEventEvaluasi(t *rapid.T, now time.Time) (EvalEvent, time.Time) {
	jenis := []string{
		"submit", "eval_ok", "eval_failed",
		"retry_requested", "window_tick", "session_closed",
	}[rapid.IntRange(0, 5).Draw(t, "jenis_kejadian")]
	// Waktu non-menurun: jeda dalam jam sehingga batas jendela retry dapat
	// tepat terlampaui maupun belum.
	next := now.Add(time.Duration(rapid.IntRange(0, 48).Draw(t, "jeda_jam")) * time.Hour)
	return EvalEvent{Type: jenis, Now: next}, next
}

// TestPropW27_StatusEvaluasiTerminalDanSatuEfek adalah Property 27: urutan
// kejadian acak pada waktu non-menurun terhadap state machine referensi
// (desain 4.11). Memeriksa status terminal, efek tepat satu, retry count,
// FailedAt, outcome/error reason, dan invariant teks jawaban.
func TestPropW27_StatusEvaluasiTerminalDanSatuEfek(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		P := drawParamEvaluasi(t)
		now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		awal := drawStateEvaluasiAwal(t, P, now)
		jawaban := answerDenganTeks{Teks: "isi jawaban tidak boleh berubah", State: awal}

		statusSah := map[string]bool{"": true, "pending": true, "scored": true, "failed": true, "unscored": true}
		terminal := awal.Status == "scored" || awal.Status == "unscored"
		stateTerminal := awal
		jumlahEnqueue := 0
		inginEnqueue := 0
		jumlahKejadian := rapid.IntRange(1, 30).Draw(t, "jumlah_kejadian")

		for i := 0; i < jumlahKejadian; i++ {
			ev, next := drawEventEvaluasi(t, now)
			now = next
			sebelum := jawaban.State

			inginState := sebelum
			inginEffect := evalEffectNone
			inginOutcome := evalOutcomeOK
			inginReason := ""

			switch {
			case sebelum.Status == "" && ev.Type == "submit":
				if sebelum.IsBlank {
					inginState.Status = "scored"
				} else {
					inginState.Status = "pending"
					inginEffect = evalEffectEnqueue
				}

			case sebelum.Status == "pending" && ev.Type == "eval_ok":
				inginState.Status = "scored"

			case sebelum.Status == "pending" && ev.Type == "eval_failed":
				ingin := "failed"
				if sebelum.RetryCount >= P.MaxEvaluationRetries {
					ingin = "unscored"
				}
				inginState.Status = ingin
				if inginState.FailedAt.IsZero() {
					inginState.FailedAt = ev.Now
				}

			case sebelum.Status == "failed" && ev.Type == "retry_requested":
				if sebelum.RetryCount >= P.MaxEvaluationRetries {
					inginOutcome = evalOutcomeError
					inginReason = evalReasonRetryExhausted
				} else {
					inginState.Status = "pending"
					inginState.RetryCount = sebelum.RetryCount + 1
					inginEffect = evalEffectEnqueue
				}

			case sebelum.Status == "pending" && ev.Type == "retry_requested":
				inginOutcome = evalOutcomeNoop

			case sebelum.Status == "scored" && ev.Type == "retry_requested":
				inginOutcome = evalOutcomeNoop

			case sebelum.Status == "unscored" && ev.Type == "retry_requested":
				inginOutcome = evalOutcomeError
				inginReason = evalReasonUnscored

			case sebelum.Status == "" && ev.Type == "retry_requested":
				inginOutcome = evalOutcomeError
				inginReason = evalReasonNotSubmitted

			case sebelum.Status == "failed" && ev.Type == "window_tick":
				if ev.Now.Sub(sebelum.FailedAt) >= time.Duration(P.EvaluationRetryWindowHours)*time.Hour {
					inginState.Status = "unscored"
				}

			case (sebelum.Status == "pending" || sebelum.Status == "failed") && ev.Type == "session_closed":
				inginState.Status = "unscored"
			}

			hasil := ApplyEvaluationEvent(sebelum, ev, P)
			if !reflect.DeepEqual(hasil.State, inginState) {
				t.Fatalf("kejadian %d (%s pada %s): state=%+v, ingin %+v (sebelum %+v)",
					i, ev.Type, sebelum.Status, hasil.State, inginState, sebelum)
			}
			if hasil.Effect != inginEffect || hasil.Outcome != inginOutcome || hasil.Reason != inginReason {
				t.Fatalf("kejadian %d (%s pada %s): effect=%q outcome=%q reason=%q, ingin %q/%q/%q",
					i, ev.Type, sebelum.Status, hasil.Effect, hasil.Outcome, hasil.Reason,
					inginEffect, inginOutcome, inginReason)
			}

			if !statusSah[hasil.State.Status] {
				t.Fatalf("status tidak sah: %q", hasil.State.Status)
			}
			if terminal && !reflect.DeepEqual(hasil.State, stateTerminal) {
				t.Fatalf("status terminal berubah: %+v menjadi %+v", stateTerminal, hasil.State)
			}
			if hasil.State.RetryCount < sebelum.RetryCount || hasil.State.RetryCount > P.MaxEvaluationRetries {
				t.Fatalf("kejadian %d: retry_count %d -> %d (maks %d)",
					i, sebelum.RetryCount, hasil.State.RetryCount, P.MaxEvaluationRetries)
			}
			if jawaban.Teks != "isi jawaban tidak boleh berubah" {
				t.Fatalf("isi jawaban berubah menjadi %q", jawaban.Teks)
			}

			jawaban.State = hasil.State
			if hasil.State.Status == "scored" || hasil.State.Status == "unscored" {
				terminal = true
				stateTerminal = hasil.State
			}
			if hasil.Effect == evalEffectEnqueue {
				jumlahEnqueue++
			}
			if inginEffect == evalEffectEnqueue {
				inginEnqueue++
			}
		}

		if jumlahEnqueue != inginEnqueue {
			t.Fatalf("jumlah efek enqueue = %d, ingin %d", jumlahEnqueue, inginEnqueue)
		}
	})
}

// RetryEvaluation adalah model prosedur retry dengan Enqueue yang dapat
// gagal (rate limit, kuota, atau batas antrean). Bila Enqueue gagal,
// transaksi dibatalkan: status dan retry_count kembali seperti semula.
type RetryEvaluation struct {
	Enqueue func() error
}

const alasanEnqueueGagal = "enqueue_failed"

// Retry menerapkan retry_requested lalu menjalankan Enqueue bila ada efek
// antrean. Nilai balikan pertama menandakan apakah Enqueue berhasil.
func (m RetryEvaluation) Retry(a AnswerEvalState, ev EvalEvent, P EvalParams) (EvalResult, bool) {
	hasil := ApplyEvaluationEvent(a, ev, P)
	if hasil.Effect != evalEffectEnqueue {
		return hasil, true
	}
	gagal := m.Enqueue != nil && m.Enqueue() != nil
	if gagal {
		return EvalResult{
			State:   a,
			Effect:  evalEffectNone,
			Outcome: evalOutcomeError,
			Reason:  alasanEnqueueGagal,
		}, false
	}
	return hasil, true
}

// TestPropW27_EnqueueGagalMembatalkanRetry adalah bagian Property 27 untuk
// model RetryEvaluation: kegagalan Enqueue tidak boleh mengubah status maupun
// retry_count, sedangkan Enqueue berhasil melakukan keduanya.
func TestPropW27_EnqueueGagalMembatalkanRetry(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		P := drawParamEvaluasi(t)
		P.MaxEvaluationRetries = rapid.IntRange(1, 4).Draw(t, "max_retry_model")
		now := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
		retry := rapid.IntRange(0, P.MaxEvaluationRetries-1).Draw(t, "retry_model")
		a := AnswerEvalState{
			Status:     "failed",
			RetryCount: retry,
			FailedAt:   now.Add(-time.Hour),
			IsBlank:    false,
		}
		ev := EvalEvent{Type: "retry_requested", Now: now}

		galat := []error{
			errors.New("ai_quota_exceeded"),
			errors.New("ai_queue_limit_exceeded"),
			errors.New("rate_limited"),
		}[rapid.IntRange(0, 2).Draw(t, "jenis_galat")]

		modelGagal := RetryEvaluation{Enqueue: func() error { return galat }}
		hasilGagal, berhasil := modelGagal.Retry(a, ev, P)
		if berhasil || hasilGagal.Outcome != evalOutcomeError || hasilGagal.Reason != alasanEnqueueGagal {
			t.Fatalf("enqueue gagal: berhasil=%v hasil=%+v", berhasil, hasilGagal)
		}
		if !reflect.DeepEqual(hasilGagal.State, a) {
			t.Fatalf("enqueue gagal mengubah state: %+v, ingin %+v", hasilGagal.State, a)
		}
		if hasilGagal.State.Status != "failed" || hasilGagal.State.RetryCount != retry {
			t.Fatalf("enqueue gagal mengubah status/retry_count: %+v", hasilGagal.State)
		}

		modelSukses := RetryEvaluation{Enqueue: func() error { return nil }}
		hasilSukses, berhasil := modelSukses.Retry(a, ev, P)
		if !berhasil || hasilSukses.State.Status != "pending" ||
			hasilSukses.State.RetryCount != retry+1 || hasilSukses.Effect != evalEffectEnqueue {
			t.Fatalf("enqueue sukses: berhasil=%v hasil=%+v", berhasil, hasilSukses)
		}
	})
}

// TestPropW27_TransisiTabelMenjagaTerminal mengunci beberapa transisi
// penting yang juga menjadi oracle properti di atas.
func TestPropW27_TransisiTabelMenjagaTerminal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		P := EvalParams{MaxEvaluationRetries: 2, EvaluationRetryWindowHours: 24}
		now := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)

		kasus := []struct {
			nama        string
			state       AnswerEvalState
			ev          EvalEvent
			inginStatus string
			inginRetry  int
			inginEffect string
			inginOut    string
			inginReason string
		}{
			{"submit kosong", AnswerEvalState{IsBlank: true}, EvalEvent{Type: "submit", Now: now}, "scored", 0, "none", "ok", ""},
			{"submit non-kosong", AnswerEvalState{}, EvalEvent{Type: "submit", Now: now}, "pending", 0, "enqueue_evaluation", "ok", ""},
			{"eval_ok", AnswerEvalState{Status: "pending"}, EvalEvent{Type: "eval_ok", Now: now}, "scored", 0, "none", "ok", ""},
			{"eval_failed retry tersedia", AnswerEvalState{Status: "pending"}, EvalEvent{Type: "eval_failed", Now: now}, "failed", 0, "none", "ok", ""},
			{"eval_failed retry habis", AnswerEvalState{Status: "pending", RetryCount: 2}, EvalEvent{Type: "eval_failed", Now: now}, "unscored", 2, "none", "ok", ""},
			{"retry failed", AnswerEvalState{Status: "failed", FailedAt: now.Add(-time.Hour)}, EvalEvent{Type: "retry_requested", Now: now}, "pending", 1, "enqueue_evaluation", "ok", ""},
			{"retry exhausted", AnswerEvalState{Status: "failed", RetryCount: 2}, EvalEvent{Type: "retry_requested", Now: now}, "failed", 2, "none", "error", "retry_exhausted"},
			{"retry pending", AnswerEvalState{Status: "pending"}, EvalEvent{Type: "retry_requested", Now: now}, "pending", 0, "none", "noop_200", ""},
			{"retry scored", AnswerEvalState{Status: "scored"}, EvalEvent{Type: "retry_requested", Now: now}, "scored", 0, "none", "noop_200", ""},
			{"retry unscored", AnswerEvalState{Status: "unscored"}, EvalEvent{Type: "retry_requested", Now: now}, "unscored", 0, "none", "error", "unscored"},
			{"retry belum submit", AnswerEvalState{}, EvalEvent{Type: "retry_requested", Now: now}, "", 0, "none", "error", "not_submitted"},
			{"window belum lewat", AnswerEvalState{Status: "failed", FailedAt: now.Add(-23 * time.Hour)}, EvalEvent{Type: "window_tick", Now: now}, "failed", 0, "none", "ok", ""},
			{"window tepat lewat", AnswerEvalState{Status: "failed", FailedAt: now.Add(-24 * time.Hour)}, EvalEvent{Type: "window_tick", Now: now}, "unscored", 0, "none", "ok", ""},
			{"session closed", AnswerEvalState{Status: "failed", FailedAt: now}, EvalEvent{Type: "session_closed", Now: now}, "unscored", 0, "none", "ok", ""},
			{"eval_ok terlambat", AnswerEvalState{Status: "unscored"}, EvalEvent{Type: "eval_ok", Now: now}, "unscored", 0, "none", "ok", ""},
		}

		for _, c := range kasus {
			got := ApplyEvaluationEvent(c.state, c.ev, P)
			if got.State.Status != c.inginStatus || got.State.RetryCount != c.inginRetry ||
				got.Effect != c.inginEffect || got.Outcome != c.inginOut || got.Reason != c.inginReason {
				t.Fatalf("kasus %s: hasil = %+v, ingin status=%q retry=%d effect=%q outcome=%q reason=%q",
					c.nama, got, c.inginStatus, c.inginRetry, c.inginEffect, c.inginOut, c.inginReason)
			}
		}
	})
}
