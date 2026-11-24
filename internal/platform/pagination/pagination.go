// Package pagination menyediakan helper paginasi untuk daftar API sesuai
// design 2.3: cursor keyset (limit, cursor, next_cursor) dan offset
// (limit, offset, total).
//
// Paket ini tidak mengakses database maupun HTTP. Parameter yang tidak valid
// dikembalikan sebagai *ValidationError. Pemanggil memetakannya ke Error_Envelope
// validation_error (HTTP 400) dengan details berupa daftar {field, reason}.
package pagination

import (
	"errors"
	"fmt"
	"strconv"
)

// ListKind menandai jenis daftar. Setiap jenis memiliki limit default dan maksimum.
type ListKind string

const (
	// KindLearnerContent mencakup Topics, Materials, dan Roadmaps milik Learner. Jenis cursor.
	KindLearnerContent ListKind = "learner_content"
	// KindSessionList mencakup Interview_Session dan assessment attempt. Jenis cursor.
	KindSessionList ListKind = "session_list"
	// KindTranscript mencakup Transcript (messages) sebuah Interview_Session. Jenis cursor.
	KindTranscript ListKind = "transcript"
	// KindLearnerRecords mencakup MasteryRecord dan misconception milik Learner. Jenis cursor.
	KindLearnerRecords ListKind = "learner_records"
	// KindAdmin mencakup daftar admin: invitations, users, Misconception, Golden_Set,
	// prompt contracts, AITask admin, dan App_Settings. Jenis offset.
	KindAdmin ListKind = "admin"
)

// Limits berisi nilai default dan maksimum `limit` sebuah jenis daftar.
type Limits struct {
	Default int
	Max     int
}

// limitsByKind mengikuti tabel "Pagination" pada design 2.3.
var limitsByKind = map[ListKind]Limits{
	KindLearnerContent: {Default: 20, Max: 100},
	KindSessionList:    {Default: 20, Max: 50},
	KindTranscript:     {Default: 50, Max: 200},
	KindLearnerRecords: {Default: 50, Max: 200},
	KindAdmin:          {Default: 25, Max: 100},
}

// Alasan validasi pada FieldError.Reason.
const (
	reasonInteger       = "integer"
	reasonMinOne        = "min_1"
	reasonMinZero       = "min_0"
	reasonInvalidCursor = "invalid_cursor"
)

// FieldError menunjuk satu parameter yang tidak valid. Bentuknya sama dengan
// elemen `details` pada validation_error.
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// ValidationError dikembalikan saat satu atau lebih parameter paginasi tidak valid.
type ValidationError struct {
	Details []FieldError
}

// Error memenuhi interface error. Pesan ini tidak dikirim ke klien.
func (e *ValidationError) Error() string {
	return "pagination: parameter tidak valid"
}

// LimitsFor mengembalikan limit default dan maksimum untuk jenis daftar.
// Jenis yang tidak dikenal menghasilkan error biasa, bukan ValidationError,
// karena itu kesalahan pemanggil dan bukan input klien.
func LimitsFor(kind ListKind) (Limits, error) {
	l, ok := limitsByKind[kind]
	if !ok {
		return Limits{}, fmt.Errorf("pagination: jenis daftar tidak dikenal %q", kind)
	}
	return l, nil
}

// parseLimit memvalidasi query `limit`. Nilai kosong berarti default.
// Nilai di luar rentang [1, Max] atau bukan bilangan bulat ditolak.
func parseLimit(raw string, l Limits) (int, *FieldError) {
	if raw == "" {
		return l.Default, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &FieldError{Field: "limit", Reason: reasonInteger}
	}
	if n < 1 {
		return 0, &FieldError{Field: "limit", Reason: reasonMinOne}
	}
	if n > l.Max {
		return 0, &FieldError{Field: "limit", Reason: "max_" + strconv.Itoa(l.Max)}
	}
	return n, nil
}

// newValidationError mengembalikan nil bila tidak ada kesalahan.
// Pemeriksaan ini menghindari nil pointer yang terbungkus interface error.
func newValidationError(details []FieldError) error {
	if len(details) == 0 {
		return nil
	}
	return &ValidationError{Details: details}
}

// IsValidationError melaporkan apakah err adalah ValidationError.
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}
