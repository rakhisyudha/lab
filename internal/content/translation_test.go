package content

import (
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// TestPropW3_WorkflowTranslationMengikutiStateMachineDanVersioning adalah
// Property 3 (wajib, area 7: aturan status dan lifecycle Translation).
func TestPropW3_WorkflowTranslationMengikutiStateMachineDanVersioning(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		state := NewTranslation("awal")
		now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

		ops := rapid.SliceOfN(rapid.IntRange(0, 3), 1, 50).Draw(t, "operasi")
		for i, op := range ops {
			switch op {
			case 0: // edit dengan version acak
				expected := state.Version + rapid.IntRange(-1, 1).Draw(t, "delta_versi")
				text := rapid.String().Draw(t, "teks")
				next, err := ApplyEdit(state, EditInput{ExpectedVersion: expected, Text: text})
				if expected != state.Version {
					if err == nil {
						t.Fatalf("edit dengan version stale diterima")
					}
					if ae, ok := apperr.As(err); !ok || ae.Code != apperr.CodeVersionConflict {
						t.Fatalf("edit stale: %v, want version_conflict", err)
					}
					if next != state {
						t.Fatal("edit gagal tidak boleh mengubah state")
					}
					continue
				}
				if err != nil {
					t.Fatalf("edit valid ditolak: %v", err)
				}
				if next.Version != state.Version+1 {
					t.Fatalf("version %d, want %d", next.Version, state.Version+1)
				}
				if state.Status == StatusPublished && (next.Status != StatusDraft || !next.PublishedAt.IsZero()) {
					t.Fatal("edit pada published harus kembali ke draft dan menghapus published_at")
				}
				state = next

			case 1: // transisi ke status acak
				targets := []string{StatusDraft, StatusInReview, StatusPublished, StatusArchived, "tidak_valid"}
				target := targets[rapid.IntRange(0, len(targets)-1).Draw(t, "target")]
				reviewer := ""
				isAdmin := false
				if rapid.Bool().Draw(t, "punya_reviewer") {
					reviewer = "admin-1"
					isAdmin = true
				}
				step := time.Duration(i+1) * time.Minute
				next, err := ApplyTransition(state, TransitionInput{
					Target: target, ReviewerID: reviewer, IsAdmin: isAdmin, Now: now.Add(step),
				})
				if err != nil {
					if ae, ok := apperr.As(err); !ok || ae.Code != apperr.CodeInvalidStatusTransition {
						t.Fatalf("transisi: %v, want invalid_status_transition", err)
					}
					if next != state {
						t.Fatal("transisi gagal tidak boleh mengubah state")
					}
					continue
				}
				if next.Status != target {
					t.Fatalf("status %q, want %q", next.Status, target)
				}
				if next.Version != state.Version {
					t.Fatal("transisi tidak boleh mengubah version")
				}
				state = next

			case 2: // peristiwa waktu (tidak mengubah state)
				now = now.Add(time.Duration(rapid.IntRange(1, 1440).Draw(t, "menit")) * time.Minute)
			}

			// Invariant: published mensyaratkan reviewer, reviewed_at, published_at.
			if state.Status == StatusPublished &&
				(state.ReviewerID == "" || state.ReviewedAt.IsZero() || state.PublishedAt.IsZero()) {
				t.Fatalf("published tanpa reviewer/waktu: %+v", state)
			}
			if state.Version < 1 {
				t.Fatalf("version harus >= 1, dapat %d", state.Version)
			}
		}
	})
}

func TestWorkflowTranslationTransisiValid(t *testing.T) {
	cases := []struct {
		from, to string
		ok       bool
	}{
		{StatusDraft, StatusInReview, true},
		{StatusInReview, StatusDraft, true},
		{StatusInReview, StatusPublished, true},
		{StatusPublished, StatusArchived, true},
		{StatusArchived, StatusDraft, true},
		{StatusDraft, StatusPublished, false},
		{StatusPublished, StatusDraft, false},
	}
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range cases {
		state := TranslationState{Status: tc.from, Version: 1}
		_, err := ApplyTransition(state, TransitionInput{
			Target: tc.to, ReviewerID: "admin-1", IsAdmin: true, Now: now,
		})
		if (err == nil) != tc.ok {
			t.Errorf("%s -> %s: err=%v, want ok=%v", tc.from, tc.to, err, tc.ok)
		}
	}
}

func TestWorkflowTranslationPublishButuhAdmin(t *testing.T) {
	state := TranslationState{Status: StatusInReview, Version: 1}
	for _, in := range []TransitionInput{
		{Target: StatusPublished, IsAdmin: true},
		{Target: StatusPublished, ReviewerID: "user-1", IsAdmin: false},
	} {
		if _, err := ApplyTransition(state, in); err == nil {
			t.Fatal("publish tanpa reviewer admin harus ditolak")
		}
	}
	next, err := ApplyTransition(state, TransitionInput{
		Target: StatusPublished, ReviewerID: "admin-1", IsAdmin: true,
		Now: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("publish valid: %v", err)
	}
	if next.Status != StatusPublished || next.PublishedAt.IsZero() {
		t.Fatalf("hasil publish tidak lengkap: %+v", next)
	}
}
