// Package content mengelola konten bilingual dan workflow Translation.
// File ini berisi aturan murni state machine dan versioning Translation;
// tidak melakukan I/O agar dapat diuji dengan property-based test.
package content

import (
	"time"

	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// Status Translation (Requirement 3.5, 4.3).
const (
	StatusDraft     = "draft"
	StatusInReview  = "in_review"
	StatusPublished = "published"
	StatusArchived  = "archived"
)

// TranslationState adalah keadaan satu baris Translation yang relevan bagi
// aturan workflow. Text mewakili gabungan field teks entitas.
type TranslationState struct {
	Status      string
	Version     int
	ReviewerID  string
	ReviewedAt  time.Time
	PublishedAt time.Time
	Text        string
}

// NewTranslation membuat Translation baru: status draft dan version 1
// (Requirement 4.1).
func NewTranslation(text string) TranslationState {
	return TranslationState{Status: StatusDraft, Version: 1, Text: text}
}

// validTransitions adalah tabel transisi status yang diizinkan (Requirement 4.3).
var validTransitions = map[string]map[string]bool{
	StatusDraft:     {StatusInReview: true},
	StatusInReview:  {StatusDraft: true, StatusPublished: true},
	StatusPublished: {StatusArchived: true},
	StatusArchived:  {StatusDraft: true},
}

// EditInput adalah data perubahan teks. ExpectedVersion harus sama dengan
// version tersimpan; bila berbeda, perubahan ditolak dengan version_conflict
// (Requirement 4.6).
type EditInput struct {
	ExpectedVersion int
	Text            string
}

// ApplyEdit menerapkan perubahan teks. Version naik satu. Bila Translation
// sedang published, status kembali ke draft dan published_at dikosongkan
// (Requirement 4.2, 4.7). Field reviewer tidak dihapus.
func ApplyEdit(state TranslationState, in EditInput) (TranslationState, error) {
	if in.ExpectedVersion != state.Version {
		return state, apperr.New(apperr.CodeVersionConflict)
	}
	updated := state
	updated.Version++
	updated.Text = in.Text
	if state.Status == StatusPublished {
		updated.Status = StatusDraft
		updated.PublishedAt = time.Time{}
	}
	return updated, nil
}

// TransitionInput adalah permintaan perubahan status. ReviewerID wajib diisi
// dan IsAdmin harus true untuk transisi ke published (Requirement 4.4).
type TransitionInput struct {
	Target     string
	ReviewerID string
	IsAdmin    bool
	Now        time.Time
}

// ApplyTransition menerapkan transisi status. Transisi di luar tabel ditolak
// dengan invalid_status_transition (Requirement 4.5). Transisi ke published
// mengisi reviewer_id, reviewed_at, dan published_at.
func ApplyTransition(state TranslationState, in TransitionInput) (TranslationState, error) {
	if !validTransitions[state.Status][in.Target] {
		return state, apperr.New(apperr.CodeInvalidStatusTransition)
	}
	if in.Target == StatusPublished && (in.ReviewerID == "" || !in.IsAdmin) {
		return state, apperr.New(apperr.CodeInvalidStatusTransition)
	}
	updated := state
	updated.Status = in.Target
	if in.Target == StatusPublished {
		updated.ReviewerID = in.ReviewerID
		updated.ReviewedAt = in.Now
		updated.PublishedAt = in.Now
	}
	return updated, nil
}

// CanPublish melaporkan apakah sebuah Translation dapat dipublikasikan.
// Dipakai layer service sebelum memanggil ApplyTransition.
func CanPublish(state TranslationState) bool {
	return validTransitions[state.Status][StatusPublished]
}
