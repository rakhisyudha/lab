package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// BindJSON membaca body JSON ke obj dan memvalidasi tag `binding`. Kegagalan
// dipetakan ke apperr:
//   - body melebihi batas: payload_too_large (413)
//   - body kosong, JSON rusak, atau tipe tidak cocok, dan gagal validasi tag: validation_error
//     dengan details [{field, reason}] (Requirement 9.8).
//
// Field diambil dari nama field Go dalam snake_case, dan reason berupa nama tag
// validator, ditambah parameter bila ada (contoh: min_12).
func BindJSON(c *gin.Context, obj any) error {
	err := c.ShouldBindJSON(obj)
	if err == nil {
		return nil
	}
	return mapBindError(err)
}

func mapBindError(err error) *apperr.Error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return apperr.New(apperr.CodePayloadTooLarge)
	}

	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) {
		fields := make([]apperr.FieldError, 0, len(verrs))
		for _, fe := range verrs {
			fields = append(fields, apperr.FieldError{
				Field:  validationFieldName(fe),
				Reason: validationReason(fe),
			})
		}
		return apperr.Validation(fields...)
	}

	// json.Decoder mengembalikan SyntaxError untuk token rusak dan ErrUnexpectedEOF
	// untuk JSON yang terpotong. Keduanya adalah body JSON tidak valid.
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) || errors.Is(err, io.ErrUnexpectedEOF) {
		return apperr.Validation(apperr.FieldError{Field: "body", Reason: "invalid_json"})
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		field := typeErr.Field
		if field == "" {
			field = "body"
		}
		return apperr.Validation(apperr.FieldError{Field: field, Reason: "invalid_type"})
	}

	if errors.Is(err, io.EOF) {
		return apperr.Validation(apperr.FieldError{Field: "body", Reason: "required"})
	}

	return apperr.Validation(apperr.FieldError{Field: "body", Reason: "invalid_body"})
}

// validationFieldName mengubah namespace validator (Root.Nested.Field) menjadi
// jalur snake_case tanpa nama tipe root, contoh: "address.postal_code".
func validationFieldName(fe validator.FieldError) string {
	parts := strings.Split(fe.Namespace(), ".")
	if len(parts) < 2 {
		return toSnakeCase(fe.Field())
	}
	for i := 1; i < len(parts); i++ {
		parts[i] = toSnakeCase(parts[i])
	}
	return strings.Join(parts[1:], ".")
}

func validationReason(fe validator.FieldError) string {
	if p := fe.Param(); p != "" {
		return fe.Tag() + "_" + p
	}
	return fe.Tag()
}

// toSnakeCase mengubah CamelCase menjadi snake_case dan menangani akronim.
// Contoh: "UserID" menjadi "user_id", "DisplayName" menjadi "display_name".
func toSnakeCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextIsLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
