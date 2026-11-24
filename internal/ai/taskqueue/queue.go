// Package taskqueue mengelola antrean AITask berbasis PostgreSQL dan versi
// in-memory. Semua perubahan status memakai fungsi murni evaluation.NextTaskState.
package taskqueue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status AITask (Requirement 16.5).
const (
	StatusQueued     = "queued"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

var (
	ErrNotFound     = errors.New("taskqueue: task tidak ditemukan")
	ErrQueueLimit   = errors.New("taskqueue: batas antrean per user terlampaui")
	ErrInvalidState = errors.New("taskqueue: transisi status tidak valid")
)

// Task adalah keadaan satu AITask.
type Task struct {
	ID               uuid.UUID
	UserID           *uuid.UUID
	TaskType         string
	Priority         int
	Status           string
	Payload          json.RawMessage
	Result           json.RawMessage
	ErrorCode        string
	ErrorMessage     string
	Attempts         int
	MaxAttempts      int
	RunAfter         time.Time
	LockedAt         *time.Time
	LockedBy         string
	IdempotencyKey   string
	ResourceType     string
	ResourceID       *uuid.UUID
	PromptContractID *uuid.UUID
	CreatedAt        time.Time
}

// NewTask adalah data untuk membuat AITask baru.
type NewTask struct {
	UserID           *uuid.UUID
	TaskType         string
	Priority         int
	Payload          json.RawMessage
	MaxAttempts      int
	RunAfter         time.Time
	IdempotencyKey   string
	ResourceType     string
	ResourceID       *uuid.UUID
	PromptContractID *uuid.UUID
}

// Queue adalah kontrak antrean yang dipakai worker dan API.
type Queue interface {
	Enqueue(ctx context.Context, in NewTask, now time.Time) (Task, error)
	Dequeue(ctx context.Context, workerID string, now time.Time, limit int) ([]Task, error)
	Complete(ctx context.Context, id uuid.UUID, result json.RawMessage, now time.Time) error
	Requeue(ctx context.Context, id uuid.UUID, runAfter time.Time, countAttempt bool, now time.Time) error
	Fail(ctx context.Context, id uuid.UUID, errorCode, message string, now time.Time) error
	ReapStale(ctx context.Context, lockTimeout time.Duration, now time.Time) error
	Get(ctx context.Context, id uuid.UUID) (Task, error)
	CountActive(ctx context.Context, userID uuid.UUID) (int, error)
}

// IdempotencyKeyOf mengembalikan kunci idempotensi dari header.
func IdempotencyKeyOf(header string) string { return header }
