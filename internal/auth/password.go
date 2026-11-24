package auth

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// PasswordPolicy adalah kebijakan password yang dapat dikonfigurasi
// (Requirement 18.2). Nilai zero memakai default.
type PasswordPolicy struct {
	MinLength     int
	RequireUpper  bool
	RequireLower  bool
	RequireDigit  bool
	RequireSymbol bool
}

// DefaultPasswordPolicy adalah kebijakan minimum P0: panjang 12 dan wajib
// memiliki huruf besar, huruf kecil, serta angka.
func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{MinLength: 12, RequireUpper: true, RequireLower: true, RequireDigit: true}
}

// ValidatePassword mengembalikan validation_error bila password tidak memenuhi
// kebijakan. Setiap pelanggaran menjadi satu FieldError.
func ValidatePassword(password string, policy PasswordPolicy) error {
	if policy.MinLength <= 0 {
		policy = DefaultPasswordPolicy()
	}
	var fields []apperr.FieldError
	if len([]rune(password)) < policy.MinLength {
		fields = append(fields, apperr.FieldError{Field: "password", Reason: fmt.Sprintf("min_%d", policy.MinLength)})
	}
	var upper, lower, digit, symbol bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			symbol = true
		}
	}
	if policy.RequireUpper && !upper {
		fields = append(fields, apperr.FieldError{Field: "password", Reason: "require_upper"})
	}
	if policy.RequireLower && !lower {
		fields = append(fields, apperr.FieldError{Field: "password", Reason: "require_lower"})
	}
	if policy.RequireDigit && !digit {
		fields = append(fields, apperr.FieldError{Field: "password", Reason: "require_digit"})
	}
	if policy.RequireSymbol && !symbol {
		fields = append(fields, apperr.FieldError{Field: "password", Reason: "require_symbol"})
	}
	if len(fields) > 0 {
		return apperr.Validation(fields...)
	}
	return nil
}

// NormalizeEmail membuang spasi dan menurunkan huruf, dipakai untuk perbandingan
// email yang tidak sensitif huruf besar-kecil.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
