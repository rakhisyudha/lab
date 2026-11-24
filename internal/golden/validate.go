package golden

import (
	"fmt"

	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// CaseInput adalah data pembuatan Golden_Case.
type CaseInput struct {
	QuestionID             string
	Locale                 string
	Category               string
	SampleAnswer           string
	ExpectedScoreMin       float64
	ExpectedScoreMax       float64
	ExpectedFound          []string
	ExpectedMissed         []string
	ExpectedMisconceptions []string
}

// ValidateCase memeriksa konsistensi label Golden_Case terhadap katalog Topic,
// Concept pertanyaan, dan Translation yang dipublikasikan (Requirement 17.4).
func ValidateCase(in CaseInput, catalogCodes, conceptCodes, publishedLocales []string) error {
	var fields []apperr.FieldError
	if in.ExpectedScoreMin > in.ExpectedScoreMax {
		fields = append(fields, apperr.FieldError{Field: "expected_score_min", Reason: "lte_expected_score_max"})
	}
	if !has(Categories, in.Category) {
		fields = append(fields, apperr.FieldError{Field: "category", Reason: "invalid_category"})
	}
	if len(in.SampleAnswer) > 8000 {
		fields = append(fields, apperr.FieldError{Field: "sample_answer", Reason: "max_8000"})
	}
	if !has(publishedLocales, in.Locale) {
		fields = append(fields, apperr.FieldError{Field: "locale", Reason: "translation_not_available"})
	}
	conceptSet := toSet(conceptCodes)
	for _, code := range in.ExpectedFound {
		if !conceptSet[code] {
			fields = append(fields, apperr.FieldError{Field: "expected_found_concept_codes", Reason: fmt.Sprintf("unknown:%s", code)})
		}
	}
	for _, code := range in.ExpectedMissed {
		if !conceptSet[code] {
			fields = append(fields, apperr.FieldError{Field: "expected_missed_concept_codes", Reason: fmt.Sprintf("unknown:%s", code)})
		}
	}
	for _, code := range in.ExpectedFound {
		if has(in.ExpectedMissed, code) {
			fields = append(fields, apperr.FieldError{Field: "expected_found_concept_codes", Reason: fmt.Sprintf("overlap:%s", code)})
		}
	}
	catalogSet := toSet(catalogCodes)
	for _, code := range in.ExpectedMisconceptions {
		if !catalogSet[code] {
			fields = append(fields, apperr.FieldError{Field: "expected_misconception_codes", Reason: fmt.Sprintf("catalog_violation:%s", code)})
		}
	}
	if len(fields) > 0 {
		return apperr.Validation(fields...)
	}
	return nil
}
