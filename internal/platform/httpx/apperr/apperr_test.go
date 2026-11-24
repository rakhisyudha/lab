package apperr

import (
	"errors"
	"fmt"
	"testing"
)

// allCodes memuat seluruh code dari design 2.2 untuk memastikan tabel lengkap.
var allCodes = []Code{
	CodeValidationError, CodeUnauthenticated, CodeInvalidCredentials, CodeInvalidRefreshToken,
	CodeRefreshTokenReuseDetected, CodeForbidden, CodeInvalidInviteCode, CodeNotFound,
	CodeTranslationNotAvailable, CodeConflict, CodeEmailAlreadyRegistered, CodeInvalidStatusTransition,
	CodeVersionConflict, CodeInvalidSessionTransition, CodeSessionNotActive, CodeSessionExpired,
	CodeStaleQuestion, CodeAttemptNotActive, CodeEvaluationNotRetryable, CodePrerequisiteCycle,
	CodeInsufficientQuestions, CodeCatalogViolation, CodePublishRequirementsNotMet, CodeGoldenGateFailed,
	CodePayloadTooLarge, CodeRateLimited, CodeAIQuotaExceeded, CodeAIQueueLimitExceeded,
	CodeAIProviderUnavailable, CodeInternalError,
}

func TestEveryCodeHasHTTPStatus(t *testing.T) {
	for _, c := range allCodes {
		status, ok := HTTPStatusFor(c)
		if !ok {
			t.Errorf("code %q tidak ada di tabel error model", c)
			continue
		}
		if status < 400 || status > 599 {
			t.Errorf("code %q memiliki status %d di luar 4xx/5xx", c, status)
		}
	}
}

func TestMinimalCodeStatuses(t *testing.T) {
	// Nilai dari Requirement 9.3 dan 25.6, serta design 2.2.
	want := map[Code]int{
		CodeValidationError:         400,
		CodeUnauthenticated:         401,
		CodeForbidden:               403,
		CodeNotFound:                404,
		CodeTranslationNotAvailable: 404,
		CodeConflict:                409,
		CodeRateLimited:             429,
		CodeAIQuotaExceeded:         429,
		CodeAIQueueLimitExceeded:    429,
		CodeAIProviderUnavailable:   503,
		CodeEvaluationNotRetryable:  409,
		CodeInternalError:           500,
		CodePayloadTooLarge:         413,
	}
	for code, status := range want {
		got, ok := HTTPStatusFor(code)
		if !ok || got != status {
			t.Errorf("code %q: status = %d (ok=%v), want %d", code, got, ok, status)
		}
	}
}

func TestNewUsesStatusFromTable(t *testing.T) {
	e := New(CodeRateLimited)
	if e.Code != CodeRateLimited || e.HTTPStatus != 429 {
		t.Fatalf("New(rate_limited) = {%q, %d}, want {rate_limited, 429}", e.Code, e.HTTPStatus)
	}
	if e.Details != nil {
		t.Errorf("Details = %v, want nil untuk New", e.Details)
	}
}

func TestUnknownCodeFallsBackTo500(t *testing.T) {
	e := New(Code("tidak_dikenal"))
	if e.HTTPStatus != 500 {
		t.Fatalf("status untuk code tidak dikenal = %d, want 500", e.HTTPStatus)
	}
	if _, ok := HTTPStatusFor(Code("tidak_dikenal")); ok {
		t.Fatal("HTTPStatusFor(code tidak dikenal) ok = true, want false")
	}
}

func TestValidationDetailsIsArrayEvenWhenEmpty(t *testing.T) {
	e := Validation()
	fields, ok := e.Details.([]FieldError)
	if !ok {
		t.Fatalf("Details bertipe %T, want []FieldError", e.Details)
	}
	if fields == nil {
		t.Fatal("Details nil, want slice kosong yang tidak nil")
	}
	if e.HTTPStatus != 400 {
		t.Errorf("status = %d, want 400", e.HTTPStatus)
	}
}

func TestResolvedMessage(t *testing.T) {
	if got := New(CodeNotFound).ResolvedMessage(); got == "" {
		t.Error("pesan default kosong untuk not_found")
	}
	e := &Error{Code: CodeConflict, HTTPStatus: 409, Message: "slug sudah dipakai"}
	if got := e.ResolvedMessage(); got != "slug sudah dipakai" {
		t.Errorf("ResolvedMessage() = %q, want pesan override", got)
	}
}

func TestAsFindsWrappedError(t *testing.T) {
	base := New(CodeInvalidCredentials)
	wrapped := fmt.Errorf("login: %w", base)

	got, ok := As(wrapped)
	if !ok || got != base {
		t.Fatalf("As(wrapped) = (%v, %v), want (base, true)", got, ok)
	}
	if _, ok := As(errors.New("biasa")); ok {
		t.Fatal("As(error biasa) ok = true, want false")
	}
	if _, ok := As(nil); ok {
		t.Fatal("As(nil) ok = true, want false")
	}
}
