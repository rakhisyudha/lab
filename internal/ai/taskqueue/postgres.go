package taskqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/db"
)

// PostgresQueue menyimpan AITask pada tabel ai_tasks.
type PostgresQueue struct {
	pool *pgxpool.Pool
}

func NewPostgresQueue(pool *pgxpool.Pool) *PostgresQueue {
	return &PostgresQueue{pool: pool}
}

const taskColumns = `id, user_id, task_type, priority, status, payload, result, error_code,
	error_message, attempts, max_attempts, run_after, locked_at, locked_by,
	idempotency_key, resource_type, resource_id, prompt_contract_id, created_at`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var userID, resourceID, promptID, lockedAt sql.NullString
	var result, payload []byte
	var errorCode, errorMessage, lockedBy, idem, resourceType sql.NullString
	err := row.Scan(&t.ID, &userID, &t.TaskType, &t.Priority, &t.Status, &payload, &result,
		&errorCode, &errorMessage, &t.Attempts, &t.MaxAttempts, &t.RunAfter,
		&lockedAt, &lockedBy, &idem, &resourceType, &resourceID, &promptID, &t.CreatedAt)
	if err != nil {
		return Task{}, err
	}
	if userID.Valid {
		id, err := uuid.Parse(userID.String)
		if err != nil {
			return Task{}, err
		}
		t.UserID = &id
	}
	if resourceID.Valid {
		id, err := uuid.Parse(resourceID.String)
		if err != nil {
			return Task{}, err
		}
		t.ResourceID = &id
	}
	if promptID.Valid {
		id, err := uuid.Parse(promptID.String)
		if err != nil {
			return Task{}, err
		}
		t.PromptContractID = &id
	}
	if lockedAt.Valid {
		ts, err := time.Parse(time.RFC3339Nano, lockedAt.String)
		if err != nil {
			return Task{}, err
		}
		t.LockedAt = &ts
	}
	t.Payload = json.RawMessage(payload)
	if len(result) > 0 {
		t.Result = json.RawMessage(result)
	}
	t.ErrorCode = errorCode.String
	t.ErrorMessage = errorMessage.String
	t.LockedBy = lockedBy.String
	t.IdempotencyKey = idem.String
	t.ResourceType = resourceType.String
	return t, nil
}

func (q *PostgresQueue) Enqueue(ctx context.Context, in NewTask, now time.Time) (Task, error) {
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
	var created Task
	err := db.WithTx(ctx, q.pool, func(tx pgx.Tx) error {
		if in.UserID != nil {
			// Kunci advisory menserialkan pemeriksaan batas per user antar instance.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, in.UserID.String()); err != nil {
				return err
			}
			if in.IdempotencyKey != "" {
				row := tx.QueryRow(ctx, `SELECT `+taskColumns+` FROM ai_tasks WHERE user_id = $1 AND idempotency_key = $2`,
					*in.UserID, in.IdempotencyKey)
				t, err := scanTask(row)
				if err == nil {
					created = t
					return nil
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
			}
			var active int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM ai_tasks
				WHERE user_id = $1 AND status IN ('queued','processing')`, *in.UserID).Scan(&active); err != nil {
				return err
			}
			// Batas dibaca pemanggil lewat QueueWithLimit; default 20.
			if active >= 20 {
				return ErrQueueLimit
			}
		}
		row := tx.QueryRow(ctx, `INSERT INTO ai_tasks
			(user_id, task_type, priority, payload, max_attempts, run_after,
			 idempotency_key, resource_type, resource_id, prompt_contract_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+taskColumns,
			in.UserID, in.TaskType, in.Priority, in.Payload, in.MaxAttempts, in.RunAfter,
			nullableString(in.IdempotencyKey), nullableString(in.ResourceType), in.ResourceID, in.PromptContractID)
		t, err := scanTask(row)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" && in.IdempotencyKey != "" && in.UserID != nil {
				existing := tx.QueryRow(ctx, `SELECT `+taskColumns+` FROM ai_tasks
					WHERE user_id = $1 AND idempotency_key = $2`, *in.UserID, in.IdempotencyKey)
				created, err = scanTask(existing)
				return err
			}
			return err
		}
		created = t
		return nil
	})
	return created, err
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (q *PostgresQueue) Dequeue(ctx context.Context, workerID string, now time.Time, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 1
	}
	var out []Task
	err := db.WithTx(ctx, q.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+taskColumns+` FROM ai_tasks
			WHERE status = 'queued' AND run_after <= $1
			ORDER BY priority, run_after, created_at FOR UPDATE SKIP LOCKED LIMIT $2`, now, limit)
		if err != nil {
			return err
		}
		var tasks []Task
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				rows.Close()
				return err
			}
			tasks = append(tasks, t)
		}
		rows.Close()
		for _, t := range tasks {
			next, err := evaluation.NextTaskState(toState(t), evaluation.TaskEvent{Type: "dequeue"}, now)
			if err != nil {
				continue
			}
			updated := fromState(t, next)
			updated.LockedBy = workerID
			if _, err := tx.Exec(ctx, `UPDATE ai_tasks SET status=$2, attempts=$3, locked_at=$4, locked_by=$5,
				started_at = COALESCE(started_at, $4) WHERE id=$1`,
				updated.ID, updated.Status, updated.Attempts, updated.LockedAt, workerID); err != nil {
				return err
			}
			out = append(out, updated)
		}
		return nil
	})
	return out, err
}

func (q *PostgresQueue) Complete(ctx context.Context, id uuid.UUID, result json.RawMessage, now time.Time) error {
	return q.transition(ctx, id, evaluation.TaskEvent{Type: "complete", Result: result}, now, "completed", result, "", nil)
}

func (q *PostgresQueue) Requeue(ctx context.Context, id uuid.UUID, runAfter time.Time, countAttempt bool, now time.Time) error {
	return q.transition(ctx, id, evaluation.TaskEvent{Type: "requeue", RunAfter: runAfter, CountAttempt: countAttempt}, now,
		"queued", nil, "", &runAfter)
}

func (q *PostgresQueue) Fail(ctx context.Context, id uuid.UUID, errorCode, message string, now time.Time) error {
	return q.transition(ctx, id, evaluation.TaskEvent{Type: "fail", ErrorCode: errorCode}, now,
		"failed", nil, errorCode, nil)
}

func (q *PostgresQueue) transition(ctx context.Context, id uuid.UUID, event evaluation.TaskEvent, now time.Time,
	status string, result json.RawMessage, errorCode string, runAfter *time.Time) error {
	return db.WithTx(ctx, q.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+taskColumns+` FROM ai_tasks WHERE id = $1 FOR UPDATE`, id)
		t, err := scanTask(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		next, err := evaluation.NextTaskState(toState(t), event, now)
		if err != nil {
			return ErrInvalidState
		}
		updated := fromState(t, next)
		_, err = tx.Exec(ctx, `UPDATE ai_tasks SET status=$2, attempts=$3, locked_at=NULL, locked_by=NULL,
			result=$4, error_code=$5, run_after=$6, finished_at=CASE WHEN $2 IN ('completed','failed') THEN $7 ELSE finished_at END
			WHERE id=$1`,
			id, updated.Status, updated.Attempts, result, errorCode, runAfter, now)
		return err
	})
}

func (q *PostgresQueue) ReapStale(ctx context.Context, lockTimeout time.Duration, now time.Time) error {
	return db.WithTx(ctx, q.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE ai_tasks SET status='queued', locked_at=NULL, locked_by=NULL, run_after=$1
			WHERE status='processing' AND locked_at < $2 AND attempts < max_attempts`,
			now, now.Add(-lockTimeout)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_tasks SET status='failed', error_code='ai_provider_unavailable',
			finished_at=$1, locked_at=NULL, locked_by=NULL
			WHERE status='processing' AND locked_at < $2 AND attempts >= max_attempts`,
			now, now.Add(-lockTimeout)); err != nil {
			return err
		}
		// Aging: priority 3 yang menunggu lebih dari 15 menit naik ke priority 2.
		_, err := tx.Exec(ctx, `UPDATE ai_tasks SET priority=2 WHERE status='queued' AND priority=3 AND run_after < $1`,
			now.Add(-15*time.Minute))
		return err
	})
}

func (q *PostgresQueue) Get(ctx context.Context, id uuid.UUID) (Task, error) {
	row := q.pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM ai_tasks WHERE id = $1`, id)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return t, err
}

func (q *PostgresQueue) CountActive(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := q.pool.QueryRow(ctx, `SELECT count(*) FROM ai_tasks
		WHERE user_id = $1 AND status IN ('queued','processing')`, userID).Scan(&n)
	return n, err
}

var _ = fmt.Sprintf
