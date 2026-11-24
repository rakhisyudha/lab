package evaluation

import "time"

// Status-status yang mungkin pada AnswerEvalState. Status kosong berarti
// jawaban belum disubmit. scored dan unscored adalah status terminal.
const (
	evalStatusPending  = "pending"
	evalStatusScored   = "scored"
	evalStatusFailed   = "failed"
	evalStatusUnscored = "unscored"
)

// Jenis-jenis kejadian yang diterima ApplyEvaluationEvent.
const (
	evalEventSubmit         = "submit"
	evalEventOK             = "eval_ok"
	evalEventFailed         = "eval_failed"
	evalEventRetryRequested = "retry_requested"
	evalEventWindowTick     = "window_tick"
	evalEventSessionClosed  = "session_closed"
)

// Efek yang diminta terjadi di luar fungsi murni (dijalankan prosedur
// pemanggil, bukan oleh ApplyEvaluationEvent).
const (
	evalEffectNone    = "none"
	evalEffectEnqueue = "enqueue_evaluation"
)

// Hasil pemanggilan ApplyEvaluationEvent.
const (
	evalOutcomeOK    = "ok"
	evalOutcomeNoop  = "noop_200"
	evalOutcomeError = "error"
)

// Alasan kegagalan retry.
const (
	evalReasonRetryExhausted = "retry_exhausted"
	evalReasonUnscored       = "unscored"
	evalReasonNotSubmitted   = "not_submitted"
)

// AnswerEvalState adalah keadaan evaluasi satu jawaban (desain 4.11). Field
// ini sengaja tidak memuat teks jawaban: isi jawaban tidak pernah berubah
// akibat transisi status (Requirement 25.4).
type AnswerEvalState struct {
	Status     string // "" | pending | scored | failed | unscored
	RetryCount int
	FailedAt   time.Time // nol = belum pernah gagal
	IsBlank    bool
}

// EvalParams memuat batas retry, jendela unscore, dan batas bawah skor.
// ClampMin dipakai pemanggil untuk memberi skor jawaban kosong saat submit;
// ApplyEvaluationEvent sendiri tidak menulis skor.
type EvalParams struct {
	MaxEvaluationRetries       int
	EvaluationRetryWindowHours int
	ClampMin                   float64
}

// EvalEvent adalah kejadian yang diterapkan pada AnswerEvalState. Now
// disuntikkan pemanggil agar fungsi tetap murni (Requirement 26.4).
type EvalEvent struct {
	Type string // submit | eval_ok | eval_failed | retry_requested | window_tick | session_closed
	Now  time.Time
}

// EvalResult memuat keadaan berikutnya, efek yang diminta, hasil pemanggilan,
// dan alasan bila outcome = error.
type EvalResult struct {
	State   AnswerEvalState
	Effect  string // none | enqueue_evaluation
	Outcome string // ok | noop_200 | error
	Reason  string // retry_exhausted | unscored | not_submitted
}

// ApplyEvaluationEvent menerapkan satu kejadian secara murni dan
// mengembalikan keadaan berikutnya beserta efek yang harus dijalankan
// pemanggil (desain 4.11). Transisi yang tidak dikenal tidak mengubah
// keadaan; scored dan unscored tidak pernah punya transisi keluar.
func ApplyEvaluationEvent(a AnswerEvalState, ev EvalEvent, P EvalParams) EvalResult {
	hasil := EvalResult{
		State:   a,
		Effect:  evalEffectNone,
		Outcome: evalOutcomeOK,
	}

	switch {
	case a.Status == "" && ev.Type == evalEventSubmit:
		if a.IsBlank {
			hasil.State.Status = evalStatusScored
		} else {
			hasil.State.Status = evalStatusPending
			hasil.Effect = evalEffectEnqueue
		}

	case a.Status == evalStatusPending && ev.Type == evalEventOK:
		hasil.State.Status = evalStatusScored

	case a.Status == evalStatusPending && ev.Type == evalEventFailed:
		// Catat kegagalan pertama saja; FailedAt tidak di-reset oleh retry.
		if hasil.State.FailedAt.IsZero() {
			hasil.State.FailedAt = ev.Now
		}
		if a.RetryCount >= P.MaxEvaluationRetries {
			hasil.State.Status = evalStatusUnscored
		} else {
			hasil.State.Status = evalStatusFailed
		}

	case a.Status == evalStatusFailed && ev.Type == evalEventRetryRequested:
		if a.RetryCount >= P.MaxEvaluationRetries {
			hasil.Outcome = evalOutcomeError
			hasil.Reason = evalReasonRetryExhausted
			return hasil
		}
		hasil.State.Status = evalStatusPending
		hasil.State.RetryCount = a.RetryCount + 1
		hasil.Effect = evalEffectEnqueue

	case (a.Status == evalStatusPending || a.Status == evalStatusScored) && ev.Type == evalEventRetryRequested:
		hasil.Outcome = evalOutcomeNoop

	case a.Status == evalStatusUnscored && ev.Type == evalEventRetryRequested:
		hasil.Outcome = evalOutcomeError
		hasil.Reason = evalReasonUnscored

	case a.Status == "" && ev.Type == evalEventRetryRequested:
		hasil.Outcome = evalOutcomeError
		hasil.Reason = evalReasonNotSubmitted

	case a.Status == evalStatusFailed && ev.Type == evalEventWindowTick:
		if ev.Now.Sub(a.FailedAt) >= time.Duration(P.EvaluationRetryWindowHours)*time.Hour {
			hasil.State.Status = evalStatusUnscored
		}

	case (a.Status == evalStatusPending || a.Status == evalStatusFailed) && ev.Type == evalEventSessionClosed:
		hasil.State.Status = evalStatusUnscored
	}

	return hasil
}
