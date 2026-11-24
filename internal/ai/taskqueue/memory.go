package taskqueue

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/ids"
)

// MemoryQueue adalah implementasi Queue in-process untuk single-instance/dev.
type MemoryQueue struct {
	mu           sync.Mutex
	tasks        map[uuid.UUID]Task
	idem         map[string]uuid.UUID
	perUserLimit int
	seq          uint64
}

func NewMemoryQueue(perUserLimit int) *MemoryQueue {
	if perUserLimit <= 0 {
		perUserLimit = 20
	}
	return &MemoryQueue{
		tasks: map[uuid.UUID]Task{}, idem: map[string]uuid.UUID{}, perUserLimit: perUserLimit,
	}
}

func (q *MemoryQueue) Enqueue(_ context.Context, in NewTask, now time.Time) (Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if in.UserID != nil {
		key := idemKey(*in.UserID, in.IdempotencyKey)
		if id, ok := q.idem[key]; ok {
			return q.tasks[id], nil
		}
		active := 0
		for _, t := range q.tasks {
			if t.UserID != nil && *t.UserID == *in.UserID && (t.Status == StatusQueued || t.Status == StatusProcessing) {
				active++
			}
		}
		if active >= q.perUserLimit {
			return Task{}, ErrQueueLimit
		}
	}
	id, err := ids.NewV7()
	if err != nil {
		return Task{}, err
	}
	q.seq++
	if in.Payload == nil {
		in.Payload = json.RawMessage("{}")
	}
	if in.MaxAttempts <= 0 {
		in.MaxAttempts = 5
	}
	if in.Priority < 1 || in.Priority > 3 {
		in.Priority = 2
	}
	if in.RunAfter.IsZero() {
		in.RunAfter = now
	}
	t := Task{
		ID: id, UserID: in.UserID, TaskType: in.TaskType, Priority: in.Priority,
		Status: StatusQueued, Payload: in.Payload, MaxAttempts: in.MaxAttempts,
		RunAfter: in.RunAfter, IdempotencyKey: in.IdempotencyKey, ResourceType: in.ResourceType,
		ResourceID: in.ResourceID, PromptContractID: in.PromptContractID,
	}
	q.tasks[id] = t
	if in.UserID != nil && in.IdempotencyKey != "" {
		q.idem[idemKey(*in.UserID, in.IdempotencyKey)] = id
	}
	return t, nil
}

func idemKey(userID uuid.UUID, key string) string { return userID.String() + "\x00" + key }

func (q *MemoryQueue) Dequeue(_ context.Context, workerID string, now time.Time, limit int) ([]Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	candidates := make([]Task, 0)
	for _, t := range q.tasks {
		if t.Status == StatusQueued && !t.RunAfter.After(now) {
			candidates = append(candidates, t)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		if !candidates[i].RunAfter.Equal(candidates[j].RunAfter) {
			return candidates[i].RunAfter.Before(candidates[j].RunAfter)
		}
		return candidates[i].ID.String() < candidates[j].ID.String()
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := make([]Task, 0, len(candidates))
	for _, t := range candidates {
		next, err := evaluation.NextTaskState(toState(t), evaluation.TaskEvent{Type: "dequeue"}, now)
		if err != nil {
			continue
		}
		t = fromState(t, next)
		t.LockedBy = workerID
		q.tasks[t.ID] = t
		out = append(out, t)
	}
	return out, nil
}

func (q *MemoryQueue) Complete(_ context.Context, id uuid.UUID, result json.RawMessage, now time.Time) error {
	return q.transition(id, evaluation.TaskEvent{Type: "complete", Result: result}, now,
		func(t *Task, s evaluation.TaskState) { t.Result = s.Result })
}

func (q *MemoryQueue) Requeue(_ context.Context, id uuid.UUID, runAfter time.Time, countAttempt bool, now time.Time) error {
	return q.transition(id, evaluation.TaskEvent{Type: "requeue", RunAfter: runAfter, CountAttempt: countAttempt}, now,
		func(t *Task, s evaluation.TaskState) { t.RunAfter = s.RunAfter })
}

func (q *MemoryQueue) Fail(_ context.Context, id uuid.UUID, errorCode, message string, now time.Time) error {
	return q.transition(id, evaluation.TaskEvent{Type: "fail", ErrorCode: errorCode}, now,
		func(t *Task, s evaluation.TaskState) {
			t.ErrorCode = s.ErrorCode
			t.ErrorMessage = message
		})
}

func (q *MemoryQueue) transition(id uuid.UUID, event evaluation.TaskEvent, now time.Time, apply func(*Task, evaluation.TaskState)) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	t, ok := q.tasks[id]
	if !ok {
		return ErrNotFound
	}
	next, err := evaluation.NextTaskState(toState(t), event, now)
	if err != nil {
		return ErrInvalidState
	}
	updated := fromState(t, next)
	apply(&updated, next)
	q.tasks[id] = updated
	return nil
}

func (q *MemoryQueue) ReapStale(_ context.Context, lockTimeout time.Duration, now time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for id, t := range q.tasks {
		if t.Status != StatusProcessing || t.LockedAt == nil || now.Sub(*t.LockedAt) <= lockTimeout {
			continue
		}
		event := evaluation.TaskEvent{Type: "lock_expired"}
		next, err := evaluation.NextTaskState(toState(t), event, now)
		if err != nil {
			continue
		}
		updated := fromState(t, next)
		if updated.Status == StatusFailed {
			updated.ErrorCode = "ai_provider_unavailable"
		}
		q.tasks[id] = updated
	}
	// Aging: priority 3 yang menunggu > 15 menit naik ke priority 2.
	for id, t := range q.tasks {
		if t.Status == StatusQueued && t.Priority == 3 && now.Sub(t.RunAfter) > 15*time.Minute {
			t.Priority = 2
			q.tasks[id] = t
		}
	}
	return nil
}

func (q *MemoryQueue) Get(_ context.Context, id uuid.UUID) (Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	t, ok := q.tasks[id]
	if !ok {
		return Task{}, ErrNotFound
	}
	return t, nil
}

func (q *MemoryQueue) CountActive(_ context.Context, userID uuid.UUID) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, t := range q.tasks {
		if t.UserID != nil && *t.UserID == userID && (t.Status == StatusQueued || t.Status == StatusProcessing) {
			n++
		}
	}
	return n, nil
}

func toState(t Task) evaluation.TaskState {
	return evaluation.TaskState{
		Status: t.Status, Attempts: t.Attempts, MaxAttempts: t.MaxAttempts,
		RunAfter: t.RunAfter, LockedAt: t.LockedAt, Result: t.Result, ErrorCode: t.ErrorCode,
	}
}

func fromState(t Task, s evaluation.TaskState) Task {
	t.Status = s.Status
	t.Attempts = s.Attempts
	t.RunAfter = s.RunAfter
	t.LockedAt = s.LockedAt
	t.Result = s.Result
	t.ErrorCode = s.ErrorCode
	if s.Status != StatusProcessing {
		t.LockedBy = ""
	}
	return t
}

var _ = errors.New
