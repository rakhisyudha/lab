package taskqueue

import (
	"context"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/google/uuid"
)

// TestPropW22_AntreanMemilihMenurutPrioritasDanMembatasiPerUser adalah
// Property 22 (wajib, area 7: aturan status dan lifecycle AITask).
func TestPropW22_AntreanMemilihMenurutPrioritasDanMembatasiPerUser(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		limit := rapid.IntRange(1, 6).Draw(t, "batas_per_user")
		q := NewMemoryQueue(limit)
		ctx := context.Background()

		user := uuid.MustParse("11111111-1111-4111-8111-111111111111")
		enqueued := 0
		count := rapid.IntRange(1, 15).Draw(t, "jumlah_task")
		for i := 0; i < count; i++ {
			priority := rapid.IntRange(1, 3).Draw(t, "prioritas")
			delay := rapid.IntRange(0, 30).Draw(t, "tunda")
			runAfter := now.Add(time.Duration(delay) * time.Minute)
			_, err := q.Enqueue(ctx, NewTask{
				UserID: &user, TaskType: "answer_evaluation", Priority: priority, RunAfter: runAfter,
			}, now)
			if enqueued >= limit {
				if err != ErrQueueLimit {
					t.Fatalf("task ke-%d seharusnya ditolak ErrQueueLimit, dapat %v", i, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("enqueue gagal: %v", err)
			}
			enqueued++
		}

		// Dequeue satu per satu: status menjadi processing dan attempts naik.
		for i := 0; i < enqueued; i++ {
			batch, err := q.Dequeue(ctx, "worker-1", now.Add(time.Hour), 1)
			if err != nil || len(batch) == 0 {
				t.Fatalf("dequeue ke-%d kosong: %v", i, err)
			}
			if batch[0].Status != StatusProcessing || batch[0].Attempts != 1 {
				t.Fatalf("state setelah dequeue: %+v", batch[0])
			}
		}

		// Urutan batch penuh harus ascending menurut prioritas.
		ordered := NewMemoryQueue(100)
		for _, p := range []int{3, 1, 2, 1, 3} {
			if _, err := ordered.Enqueue(ctx, NewTask{TaskType: "answer_evaluation", Priority: p}, now); err != nil {
				t.Fatal(err)
			}
		}
		batch, err := ordered.Dequeue(ctx, "worker-2", now, 10)
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(batch); i++ {
			if batch[i-1].Priority > batch[i].Priority {
				t.Fatalf("urutan prioritas salah: %v", batch)
			}
		}

		// Idempotensi: kunci sama dari user sama mengembalikan task yang sama.
		idemKey := "eval-retry:1:1"
		a, err := ordered.Enqueue(ctx, NewTask{UserID: &user, TaskType: "answer_evaluation",
			IdempotencyKey: idemKey}, now)
		if err != nil {
			t.Fatal(err)
		}
		b, err := ordered.Enqueue(ctx, NewTask{UserID: &user, TaskType: "answer_evaluation",
			IdempotencyKey: idemKey}, now)
		if err != nil {
			t.Fatal(err)
		}
		if a.ID != b.ID {
			t.Fatal("idempotency_key menghasilkan dua task berbeda")
		}
	})
}
