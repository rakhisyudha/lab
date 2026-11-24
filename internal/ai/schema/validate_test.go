package schema

import (
	"encoding/json"
	"testing"
)

func contohValid() map[string]any {
	return map[string]any{
		"concept_results": []any{
			map[string]any{"concept_code": "idempotency", "coverage": "found", "evidence": "dikirim dua kali hasilnya sama"},
			map[string]any{"concept_code": "partial_update", "coverage": "partial", "evidence": "PATCH mengubah sebagian"},
		},
		"misconception_codes":          []any{},
		"resolved_misconception_codes": []any{"put_creates_only"},
		"relevance":                    "on_topic",
		"score_hint":                   92.0,
		"feedback":                     "Penjelasan Anda sudah baik.",
		"confidence":                   0.9,
		"injection_suspected":          false,
	}
}

func keRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	mentah, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("gagal membuat JSON: %v", err)
	}
	return mentah
}

func TestValidateEvaluatorOutputValid(t *testing.T) {
	izin := []string{"idempotency", "partial_update"}
	katalog := []string{"put_creates_only", "sql_myth"}
	terdeteksi := []string{"put_creates_only"}

	got, err := ValidateEvaluatorOutput(keRaw(t, contohValid()), izin, katalog, terdeteksi)
	if err != nil {
		t.Fatalf("keluaran valid ditolak: %v", err)
	}
	if len(got.ConceptResults) != 2 || got.Relevance != "on_topic" || got.ScoreHint != 92 || got.Confidence != 0.9 {
		t.Fatalf("hasil tidak sesuai contoh: %+v", got)
	}
	if len(got.ResolvedMisconceptionCodes) != 1 || got.ResolvedMisconceptionCodes[0] != "put_creates_only" {
		t.Fatalf("kode terselesaikan tidak terbaca: %+v", got)
	}

	// Batas inclusif harus diterima.
	for _, pasangan := range []struct {
		field string
		nilai float64
	}{
		{"score_hint", 0}, {"score_hint", 100},
		{"confidence", 0}, {"confidence", 1},
	} {
		v := contohValid()
		v[pasangan.field] = pasangan.nilai
		if _, err := ValidateEvaluatorOutput(keRaw(t, v), izin, katalog, terdeteksi); err != nil {
			t.Fatalf("%s = %v seharusnya valid: %v", pasangan.field, pasangan.nilai, err)
		}
	}
}

func TestValidateEvaluatorOutputMenolakPelanggaran(t *testing.T) {
	izin := []string{"idempotency", "partial_update"}
	katalog := []string{"put_creates_only", "sql_myth"}
	terdeteksi := []string{"put_creates_only"}

	duplikat := contohValid()
	duplikat["concept_results"] = []any{
		map[string]any{"concept_code": "idempotency", "coverage": "found", "evidence": "a"},
		map[string]any{"concept_code": "idempotency", "coverage": "found", "evidence": "b"},
	}
	hilang := contohValid()
	hilang["concept_results"] = []any{
		map[string]any{"concept_code": "idempotency", "coverage": "found", "evidence": "a"},
	}
	asing := contohValid()
	asing["concept_results"] = []any{
		map[string]any{"concept_code": "idempotency", "coverage": "found", "evidence": "a"},
		map[string]any{"concept_code": "caching", "coverage": "found", "evidence": "b"},
	}
	coverageRusak := contohValid()
	coverageRusak["concept_results"] = []any{
		map[string]any{"concept_code": "idempotency", "coverage": "mostly", "evidence": "a"},
		map[string]any{"concept_code": "partial_update", "coverage": "found", "evidence": "b"},
	}
	katalogRusak := contohValid()
	katalogRusak["misconception_codes"] = []any{"sql_injection_myth"}
	resolvedRusak := contohValid()
	resolvedRusak["resolved_misconception_codes"] = []any{"sql_myth"}
	irisan := contohValid()
	irisan["misconception_codes"] = []any{"put_creates_only"}

	kasus := []struct {
		nama string
		raw  json.RawMessage
	}{
		{"field severity ditolak", keRaw(t, map[string]any{"severity": "critical"})},
		{"field skor akhir ditolak", keRaw(t, map[string]any{"final_score": 90})},
		{"dua objek JSON ditolak", json.RawMessage(`{"relevance":"empty"}{"relevance":"empty"}`)},
		{"JSON rusak ditolak", json.RawMessage(`{"relevance":`)},
		{"keluaran kosong ditolak", nil},
		{"konsep ganda ditolak", keRaw(t, duplikat)},
		{"konsep hilang ditolak", keRaw(t, hilang)},
		{"kode konsep asing ditolak", keRaw(t, asing)},
		{"coverage tidak valid ditolak", keRaw(t, coverageRusak)},
		{"kode di luar katalog ditolak", keRaw(t, katalogRusak)},
		{"resolusi kode tak terdeteksi ditolak", keRaw(t, resolvedRusak)},
		{"kode pada kedua daftar ditolak", keRaw(t, irisan)},
		{"relevance tidak valid ditolak", json.RawMessage(gantiField(t, "relevance", `"side_topic"`))},
		{"score_hint terlalu besar ditolak", json.RawMessage(gantiField(t, "score_hint", `140`))},
		{"score_hint negatif ditolak", json.RawMessage(gantiField(t, "score_hint", `-1`))},
		{"confidence terlalu besar ditolak", json.RawMessage(gantiField(t, "confidence", `1.1`))},
	}

	for _, k := range kasus {
		t.Run(k.nama, func(t *testing.T) {
			if _, err := ValidateEvaluatorOutput(k.raw, izin, katalog, terdeteksi); err == nil {
				t.Fatalf("keluaran seharusnya ditolak: %s", string(k.raw))
			}
		})
	}
}

// gantiField mengubah satu field pada contoh valid menjadi fragmen JSON mentah.
func gantiField(t *testing.T, field, nilai string) string {
	t.Helper()
	v := contohValid()
	delete(v, field)
	mentah := string(keRaw(t, v))
	mentah = mentah[:len(mentah)-1] + `,"` + field + `":` + nilai + `}`
	return mentah
}

func TestValidateEvaluatorOutputTanpaKonsep(t *testing.T) {
	v := contohValid()
	v["concept_results"] = []any{}
	v["misconception_codes"] = []any{}
	v["resolved_misconception_codes"] = []any{}
	v["relevance"] = "empty"
	if _, err := ValidateEvaluatorOutput(keRaw(t, v), nil, nil, nil); err != nil {
		t.Fatalf("keluaran tanpa konsep yang diizinkan seharusnya valid: %v", err)
	}
}
