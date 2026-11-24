// Package schema memvalidasi keluaran evaluator AI terhadap kontrak
// answer_evaluation. Validasi bersifat ketat: field yang tidak dikenal ditolak.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ConceptResult adalah satu entri cakupan konsep pada keluaran evaluator.
type ConceptResult struct {
	ConceptCode string `json:"concept_code"`
	Coverage    string `json:"coverage"`
	Evidence    string `json:"evidence"`
}

// Output adalah bentuk keluaran answer_evaluation yang diizinkan.
// Skor akhir dan severity sengaja tidak ada di sini; keduanya dihitung server.
type Output struct {
	ConceptResults             []ConceptResult `json:"concept_results"`
	MisconceptionCodes         []string        `json:"misconception_codes"`
	ResolvedMisconceptionCodes []string        `json:"resolved_misconception_codes"`
	Relevance                  string          `json:"relevance"`
	ScoreHint                  float64         `json:"score_hint"`
	Feedback                   string          `json:"feedback"`
	Confidence                 float64         `json:"confidence"`
	InjectionSuspected         bool            `json:"injection_suspected"`
}

// ValidateEvaluatorOutput mengurai raw secara ketat lalu memeriksa semua
// invarian kontrak. Field tambahan seperti "severity" atau "final_score"
// ditolak pada tahap penguraian.
func ValidateEvaluatorOutput(raw json.RawMessage, allowedConcepts, catalogCodes, detectedCodes []string) (Output, error) {
	var out Output
	if len(raw) == 0 {
		return Output{}, fmt.Errorf("schema: keluaran evaluator kosong")
	}

	pengurai := json.NewDecoder(bytes.NewReader(raw))
	pengurai.DisallowUnknownFields()
	if err := pengurai.Decode(&out); err != nil {
		return Output{}, fmt.Errorf("schema: keluaran tidak sesuai kontrak: %w", err)
	}
	// Satu pemanggilan hanya boleh menghasilkan satu objek JSON.
	var sisa any
	if err := pengurai.Decode(&sisa); err != io.EOF {
		if err == nil {
			return Output{}, fmt.Errorf("schema: keluaran memuat lebih dari satu nilai JSON")
		}
		return Output{}, fmt.Errorf("schema: sisa keluaran bukan JSON valid: %w", err)
	}

	if err := validasiConceptResults(out.ConceptResults, allowedConcepts); err != nil {
		return Output{}, err
	}

	katalog := himpunan(catalogCodes)
	for _, code := range out.MisconceptionCodes {
		if !katalog[code] {
			return Output{}, fmt.Errorf("schema: misconception_codes memuat kode di luar katalog: %q", code)
		}
	}

	terdeteksi := himpunan(detectedCodes)
	for _, code := range out.ResolvedMisconceptionCodes {
		if !terdeteksi[code] {
			return Output{}, fmt.Errorf("schema: resolved_misconception_codes memuat kode yang tidak terdeteksi: %q", code)
		}
	}

	for _, code := range out.MisconceptionCodes {
		for _, selesai := range out.ResolvedMisconceptionCodes {
			if code == selesai {
				return Output{}, fmt.Errorf("schema: kode %q muncul pada misconception_codes dan resolved_misconception_codes", code)
			}
		}
	}

	switch out.Relevance {
	case "on_topic", "off_topic", "empty":
	default:
		return Output{}, fmt.Errorf("schema: relevance tidak valid: %q", out.Relevance)
	}
	if out.ScoreHint < 0 || out.ScoreHint > 100 {
		return Output{}, fmt.Errorf("schema: score_hint di luar rentang [0, 100]: %v", out.ScoreHint)
	}
	if out.Confidence < 0 || out.Confidence > 1 {
		return Output{}, fmt.Errorf("schema: confidence di luar rentang [0, 1]: %v", out.Confidence)
	}

	return out, nil
}

// validasiConceptResults memastikan tepat satu entri per konsep yang diizinkan,
// tanpa entri duplikat, hilang, atau asing, dengan coverage yang sah.
func validasiConceptResults(results []ConceptResult, allowedConcepts []string) error {
	diizinkan := himpunan(allowedConcepts)
	if len(results) != len(diizinkan) {
		return fmt.Errorf("schema: concept_results berisi %d entri, ingin tepat %d", len(results), len(diizinkan))
	}

	terlihat := make(map[string]bool, len(results))
	for _, r := range results {
		if _, ok := diizinkan[r.ConceptCode]; !ok {
			return fmt.Errorf("schema: concept_results memuat kode asing: %q", r.ConceptCode)
		}
		if terlihat[r.ConceptCode] {
			return fmt.Errorf("schema: concept_results memuat kode ganda: %q", r.ConceptCode)
		}
		terlihat[r.ConceptCode] = true
		switch r.Coverage {
		case "found", "partial", "missed":
		default:
			return fmt.Errorf("schema: coverage tidak valid untuk %q: %q", r.ConceptCode, r.Coverage)
		}
	}
	for code := range diizinkan {
		if !terlihat[code] {
			return fmt.Errorf("schema: concept_results kehilangan kode: %q", code)
		}
	}
	return nil
}

// himpunan mengubah daftar kode menjadi peta keanggotaan.
func himpunan(kode []string) map[string]bool {
	hasil := make(map[string]bool, len(kode))
	for _, k := range kode {
		hasil[k] = true
	}
	return hasil
}
