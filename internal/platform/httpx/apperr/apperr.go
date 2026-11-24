// Package apperr mendefinisikan error aplikasi yang dipetakan ke Error_Envelope
// oleh paket httpx. Daftar code dan HTTP status mengikuti tabel error model
// pada design, bagian 2.2 (Requirement 9.2, 9.3).
//
// Handler dan service mengembalikan *Error. Paket ini tidak mengimpor paket lain
// proyek agar dapat dipakai dari modul mana pun tanpa siklus dependensi.
package apperr

import (
	"errors"
	"fmt"
)

// Code adalah nilai field `code` pada Error_Envelope.
type Code string

// Daftar code error. Nilainya harus sama dengan tabel pada design 2.2.
const (
	CodeValidationError           Code = "validation_error"
	CodeUnauthenticated           Code = "unauthenticated"
	CodeInvalidCredentials        Code = "invalid_credentials"
	CodeInvalidRefreshToken       Code = "invalid_refresh_token"
	CodeRefreshTokenReuseDetected Code = "refresh_token_reuse_detected"
	CodeForbidden                 Code = "forbidden"
	CodeInvalidInviteCode         Code = "invalid_invite_code"
	CodeNotFound                  Code = "not_found"
	CodeTranslationNotAvailable   Code = "translation_not_available"
	CodeConflict                  Code = "conflict"
	CodeEmailAlreadyRegistered    Code = "email_already_registered"
	CodeInvalidStatusTransition   Code = "invalid_status_transition"
	CodeVersionConflict           Code = "version_conflict"
	CodeInvalidSessionTransition  Code = "invalid_session_transition"
	CodeSessionNotActive          Code = "session_not_active"
	CodeSessionExpired            Code = "session_expired"
	CodeStaleQuestion             Code = "stale_question"
	CodeAttemptNotActive          Code = "attempt_not_active"
	CodeEvaluationNotRetryable    Code = "evaluation_not_retryable"
	CodePrerequisiteCycle         Code = "prerequisite_cycle"
	CodeInsufficientQuestions     Code = "insufficient_questions"
	CodeCatalogViolation          Code = "catalog_violation"
	CodePublishRequirementsNotMet Code = "publish_requirements_not_met"
	CodeGoldenGateFailed          Code = "golden_gate_failed"
	CodePayloadTooLarge           Code = "payload_too_large"
	CodeRateLimited               Code = "rate_limited"
	CodeAIQuotaExceeded           Code = "ai_quota_exceeded"
	CodeAIQueueLimitExceeded      Code = "ai_queue_limit_exceeded"
	CodeAIProviderUnavailable     Code = "ai_provider_unavailable"
	CodeInternalError             Code = "internal_error"
)

// spec menyimpan HTTP status dan pesan default untuk satu code.
type spec struct {
	status  int
	message string
}

// specs adalah tabel error model (design 2.2). Pesan tidak memuat detail infrastruktur.
var specs = map[Code]spec{
	CodeValidationError:           {400, "Request tidak valid."},
	CodeUnauthenticated:           {401, "Autentikasi diperlukan atau token tidak valid."},
	CodeInvalidCredentials:        {401, "Email atau password salah."},
	CodeInvalidRefreshToken:       {401, "Refresh token tidak valid atau kedaluwarsa."},
	CodeRefreshTokenReuseDetected: {401, "Refresh token dipakai ulang. Seluruh sesi dicabut, silakan login ulang."},
	CodeForbidden:                 {403, "Akses ditolak."},
	CodeInvalidInviteCode:         {403, "Invite code tidak valid."},
	CodeNotFound:                  {404, "Sumber tidak ditemukan."},
	CodeTranslationNotAvailable:   {404, "Terjemahan tidak tersedia untuk locale ini."},
	CodeConflict:                  {409, "Konflik dengan data yang ada."},
	CodeEmailAlreadyRegistered:    {409, "Email sudah terdaftar."},
	CodeInvalidStatusTransition:   {409, "Transisi status tidak valid."},
	CodeVersionConflict:           {409, "Versi data tidak sesuai."},
	CodeInvalidSessionTransition:  {409, "Transisi status sesi tidak valid."},
	CodeSessionNotActive:          {409, "Sesi tidak aktif."},
	CodeSessionExpired:            {409, "Sesi sudah kedaluwarsa."},
	CodeStaleQuestion:             {409, "Pertanyaan sudah tidak aktif."},
	CodeAttemptNotActive:          {409, "Attempt tidak aktif."},
	CodeEvaluationNotRetryable:    {409, "Evaluasi jawaban ini tidak dapat diulang."},
	CodePrerequisiteCycle:         {409, "Prasyarat topic membentuk siklus."},
	CodeInsufficientQuestions:     {422, "Jumlah pertanyaan tidak cukup."},
	CodeCatalogViolation:          {422, "Kode misconception atau concept tidak ada pada katalog topic."},
	CodePublishRequirementsNotMet: {422, "Syarat publikasi belum terpenuhi."},
	CodeGoldenGateFailed:          {422, "Aktivasi prompt contract ditolak oleh gerbang Golden Set."},
	CodePayloadTooLarge:           {413, "Ukuran request melebihi batas."},
	CodeRateLimited:               {429, "Terlalu banyak request. Coba lagi nanti."},
	CodeAIQuotaExceeded:           {429, "Kuota AI habis."},
	CodeAIQueueLimitExceeded:      {429, "Jumlah AI task aktif telah mencapai batas."},
	CodeAIProviderUnavailable:     {503, "Layanan AI sedang tidak tersedia."},
	CodeInternalError:             {500, "Terjadi kesalahan internal."},
}

// FieldError adalah satu item `details` untuk validation_error (Requirement 9.8).
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Error adalah error API. Field Details berisi objek bebas untuk code selain
// validation_error; untuk validation_error berisi []FieldError.
type Error struct {
	Code       Code
	HTTPStatus int
	Details    any
	// Message opsional. Bila kosong, pesan default untuk Code dipakai.
	Message string
}

// Error memenuhi interface error. Teks ini hanya untuk log dan debugging.
func (e *Error) Error() string {
	return fmt.Sprintf("apperr %s (http %d)", e.Code, e.HTTPStatus)
}

// ResolvedMessage mengembalikan Message bila terisi, selain itu pesan default untuk Code.
func (e *Error) ResolvedMessage() string {
	if e.Message != "" {
		return e.Message
	}
	if s, ok := specs[e.Code]; ok {
		return s.message
	}
	return specs[CodeInternalError].message
}

// New membuat Error tanpa details. HTTP status diambil dari tabel error model.
func New(code Code) *Error {
	return WithDetails(code, nil)
}

// WithDetails membuat Error dengan details. Code yang tidak ada di tabel
// dipetakan ke HTTP 500 agar tidak menghasilkan status yang tidak terdefinisi.
func WithDetails(code Code, details any) *Error {
	status := 500
	if s, ok := specs[code]; ok {
		status = s.status
	}
	return &Error{Code: code, HTTPStatus: status, Details: details}
}

// Validation membuat validation_error dengan daftar field yang gagal.
// Details selalu berupa array, meskipun kosong.
func Validation(fields ...FieldError) *Error {
	if fields == nil {
		fields = []FieldError{}
	}
	return WithDetails(CodeValidationError, fields)
}

// HTTPStatusFor mengembalikan HTTP status untuk code dan true bila code dikenal.
func HTTPStatusFor(code Code) (int, bool) {
	s, ok := specs[code]
	return s.status, ok
}

// As mencari *Error di dalam rantai error (mendukung fmt.Errorf dengan %w).
func As(err error) (*Error, bool) {
	var ae *Error
	if errors.As(err, &ae) && ae != nil {
		return ae, true
	}
	return nil, false
}
