package evaluation

import (
	"encoding/json"
	"errors"
	"time"
)

// Status yang mungkin pada TaskState (desain 4.9).
const (
	statusQueued     = "queued"
	statusProcessing = "processing"
	statusCompleted  = "completed"
	statusFailed     = "failed"
)

// Jenis kejadian yang dapat diterima NextTaskState.
const (
	eventDequeue     = "dequeue"
	eventComplete    = "complete"
	eventRequeue     = "requeue"
	eventFail        = "fail"
	eventLockExpired = "lock_expired"
)

// Pesan galat dan kode kegagalan bawaan.
const (
	errInvalidTaskTransition = "invalid_task_transition"
	errProviderUnavailable   = "ai_provider_unavailable"
)

// TaskState adalah keadaan satu AITask. Waktu terkunci (LockedAt) hanya
// boleh terisi pada status processing. Result wajib terisi pada completed,
// dan ErrorCode wajib terisi pada failed.
type TaskState struct {
	Status      string // queued | processing | completed | failed
	Attempts    int
	MaxAttempts int
	RunAfter    time.Time
	LockedAt    *time.Time
	Result      json.RawMessage // nil dibolehkan
	ErrorCode   string
}

// TaskEvent adalah kejadian yang diterapkan pada TaskState. CountAttempt
// dipakai hanya untuk requeue: true berarti attempts tidak diubah (pemanggil
// sudah menaikkannya), false berarti attempts dikurangi satu (retry karena
// throttling/validasi).
type TaskEvent struct {
	Type         string // dequeue | complete | requeue | fail | lock_expired
	Result       json.RawMessage
	RunAfter     time.Time
	CountAttempt bool
	ErrorCode    string
}

// NextTaskState menerapkan satu kejadian secara murni dan mengembalikan
// keadaan berikutnya. Transisi yang tidak diizinkan, data kejadian yang
// tidak lengkap, atau keadaan yang melanggar invarian ditolak dengan galat
// "invalid_task_transition"; keadaan lama dikembalikan apa adanya sehingga
// state terminal tidak pernah berubah.
func NextTaskState(task TaskState, event TaskEvent, now time.Time) (TaskState, error) {
	tolak := func() (TaskState, error) {
		return task, errors.New(errInvalidTaskTransition)
	}

	var next TaskState
	switch {
	case task.Status == statusQueued && event.Type == eventDequeue:
		// attempts harus tetap <= MaxAttempts setelah dinaikkan.
		if task.RunAfter.After(now) || task.Attempts >= task.MaxAttempts {
			return tolak()
		}
		locked := now
		next = task
		next.Status = statusProcessing
		next.Attempts = task.Attempts + 1
		next.LockedAt = &locked
		next.ErrorCode = ""

	case task.Status == statusProcessing && event.Type == eventComplete:
		if event.Result == nil {
			return tolak()
		}
		next = task
		next.Status = statusCompleted
		next.Result = event.Result
		next.LockedAt = nil
		next.ErrorCode = ""

	case task.Status == statusProcessing && event.Type == eventRequeue:
		next = task
		next.Status = statusQueued
		next.LockedAt = nil
		next.RunAfter = event.RunAfter
		if event.CountAttempt {
			next.Attempts = task.Attempts
		} else if next.Attempts > 0 {
			next.Attempts--
		}
		next.ErrorCode = ""

	case task.Status == statusProcessing && event.Type == eventFail:
		if event.ErrorCode == "" {
			return tolak()
		}
		next = task
		next.Status = statusFailed
		next.ErrorCode = event.ErrorCode
		next.LockedAt = nil

	case task.Status == statusProcessing && event.Type == eventLockExpired:
		next = task
		next.LockedAt = nil
		if task.Attempts < task.MaxAttempts {
			next.Status = statusQueued
			next.RunAfter = now
			next.ErrorCode = ""
		} else {
			next.Status = statusFailed
			next.ErrorCode = errProviderUnavailable
		}

	default:
		return tolak()
	}

	if !invarianTugasValid(next) {
		return tolak()
	}
	return next, nil
}

// invarianTugasValid memeriksa empat invarian yang harus berlaku setelah
// setiap transisi berhasil.
func invarianTugasValid(s TaskState) bool {
	switch s.Status {
	case statusQueued, statusProcessing, statusCompleted, statusFailed:
	default:
		return false
	}
	if s.Attempts < 0 || s.Attempts > s.MaxAttempts {
		return false
	}
	if (s.Status == statusProcessing) != (s.LockedAt != nil) {
		return false
	}
	if s.Status == statusCompleted && s.Result == nil {
		return false
	}
	if s.Status == statusFailed && s.ErrorCode == "" {
		return false
	}
	return true
}
