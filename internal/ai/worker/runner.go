// Package worker menjalankan loop dequeue AITask dan job terjadwal.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	"github.com/rakhisyudha/lab/internal/platform/clock"
)

// Handler memproses satu AITask. Handler dari phase berikutnya mendaftarkan
// diri pada Registry berdasarkan task_type.
type Handler func(ctx context.Context, task taskqueue.Task) error

// Runner adalah loop worker dengan registry handler dan job pemeliharaan.
type Runner struct {
	queue       taskqueue.Queue
	handlers    map[string]Handler
	logger      *slog.Logger
	clock       clock.Clock
	workerID    string
	lockTimeout time.Duration
	interval    time.Duration
}

type Options struct {
	Queue       taskqueue.Queue
	Handlers    map[string]Handler
	Logger      *slog.Logger
	Clock       clock.Clock
	WorkerID    string
	LockTimeout time.Duration
	Interval    time.Duration
}

func New(opts Options) *Runner {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Clock == nil {
		opts.Clock = clock.System{}
	}
	if opts.WorkerID == "" {
		opts.WorkerID = fmt.Sprintf("worker-%d", time.Now().UnixNano())
	}
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = 120 * time.Second
	}
	if opts.Interval <= 0 {
		opts.Interval = 250 * time.Millisecond
	}
	return &Runner{
		queue: opts.Queue, handlers: opts.Handlers, logger: opts.Logger,
		clock: opts.Clock, workerID: opts.WorkerID,
		lockTimeout: opts.LockTimeout, interval: opts.Interval,
	}
}

// Run berjalan sampai ctx dibatalkan. Satu tick memproses task yang tersedia
// lalu menjalankan reaper. Graceful shutdown diatur pemanggil lewat context.
func (r *Runner) Run(ctx context.Context) error {
	r.logger.Info("worker: mulai", slog.String("worker_id", r.workerID))
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	maintenance := time.NewTicker(30 * time.Second)
	defer maintenance.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("worker: berhenti")
			return nil
		case <-maintenance.C:
			if err := r.queue.ReapStale(ctx, r.lockTimeout, r.clock.Now()); err != nil {
				r.logger.Error("worker: reaper", slog.Any("error", err))
			}
		case <-ticker.C:
			tasks, err := r.queue.Dequeue(ctx, r.workerID, r.clock.Now(), 4)
			if err != nil {
				r.logger.Error("worker: dequeue", slog.Any("error", err))
				continue
			}
			for _, task := range tasks {
				r.handle(ctx, task)
			}
		}
	}
}

func (r *Runner) handle(ctx context.Context, task taskqueue.Task) {
	handler, ok := r.handlers[task.TaskType]
	if !ok {
		if err := r.queue.Fail(ctx, task.ID, "unsupported_task_type",
			"tidak ada handler untuk "+task.TaskType, r.clock.Now()); err != nil {
			r.logger.Error("worker: gagal menandai task", slog.Any("error", err))
		}
		return
	}
	if err := handler(ctx, task); err != nil {
		r.logger.Error("worker: handler gagal",
			slog.String("task_id", task.ID.String()), slog.Any("error", err))
	}
}
