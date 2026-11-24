package settings

import (
	"bytes"
	"encoding/json"
	"math/big"
	"reflect"
	"unicode/utf8"

	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// schemaDef adalah subset JSON Schema yang dipakai app_settings. Keyword di luar
// daftar ini diabaikan, sehingga skema baru dapat ditambahkan tanpa mengubah
// validator selama tidak membatasi nilai secara diam-diam.
type schemaDef struct {
	Type      json.RawMessage   `json:"type"`
	Minimum   *json.Number      `json:"minimum"`
	Maximum   *json.Number      `json:"maximum"`
	Enum      []json.RawMessage `json:"enum"`
	MinLength *int              `json:"minLength"`
	MaxLength *int              `json:"maxLength"`
}

// validateValue memeriksa value terhadap value_schema milik key. Seluruh
// kegagalan dikembalikan sebagai apperr validation_error yang menyebut key.
func validateValue(key string, rawSchema, value json.RawMessage) error {
	fail := func(reason string) error {
		return apperr.Validation(apperr.FieldError{Field: key, Reason: reason})
	}

	if len(bytes.TrimSpace(rawSchema)) == 0 {
		return fail("skema nilai tidak tersedia")
	}
	var sc schemaDef
	if err := json.Unmarshal(rawSchema, &sc); err != nil {
		return fail("skema nilai tidak valid")
	}

	v, err := decodeJSON(value)
	if err != nil {
		return fail("nilai bukan JSON yang valid")
	}

	types, err := parseTypes(sc.Type)
	if err != nil {
		return fail("tipe pada skema tidak didukung")
	}
	if len(types) > 0 && !matchesAnyType(v, types) {
		return fail("tipe nilai tidak sesuai skema")
	}

	if len(sc.Enum) > 0 {
		found := false
		for _, raw := range sc.Enum {
			allowed, err := decodeJSON(raw)
			if err != nil {
				continue
			}
			if jsonEqual(allowed, v) {
				found = true
				break
			}
		}
		if !found {
			return fail("nilai tidak termasuk daftar enum")
		}
	}

	if sc.Minimum != nil || sc.Maximum != nil {
		if n, ok := v.(json.Number); ok {
			if r, ok := new(big.Rat).SetString(n.String()); ok {
				if sc.Minimum != nil {
					if min, ok := new(big.Rat).SetString(sc.Minimum.String()); ok && r.Cmp(min) < 0 {
						return fail("nilai di bawah batas minimum")
					}
				}
				if sc.Maximum != nil {
					if max, ok := new(big.Rat).SetString(sc.Maximum.String()); ok && r.Cmp(max) > 0 {
						return fail("nilai melebihi batas maksimum")
					}
				}
			}
		}
	}

	if s, ok := v.(string); ok {
		n := utf8.RuneCountInString(s)
		if sc.MinLength != nil && n < *sc.MinLength {
			return fail("panjang string di bawah batas minimum")
		}
		if sc.MaxLength != nil && n > *sc.MaxLength {
			return fail("panjang string melebihi batas maksimum")
		}
	}

	return nil
}

// parseTypes membaca keyword "type" yang boleh berupa string tunggal atau array
// string. Skema tanpa type diterima (misalnya skema yang hanya memakai enum).
func parseTypes(raw json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var single string
	if err := json.Unmarshal(trimmed, &single); err == nil {
		return []string{single}, nil
	}
	var many []string
	if err := json.Unmarshal(trimmed, &many); err != nil {
		return nil, ErrInvalidValue
	}
	return many, nil
}

// matchesAnyType melaporkan apakah v memenuhi salah satu tipe yang didukung.
// integer menolak bilangan pecahan dan string; null mensyaratkan literal null.
func matchesAnyType(v any, types []string) bool {
	for _, typ := range types {
		if matchesType(v, typ) {
			return true
		}
	}
	return false
}

func matchesType(v any, typ string) bool {
	switch typ {
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		r, ok := new(big.Rat).SetString(n.String())
		return ok && r.IsInt()
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	default:
		return false
	}
}

// decodeJSON mengurai JSON dengan UseNumber agar bilangan tidak berubah menjadi
// float64 (penting untuk membedakan integer dari pecahan).
func decodeJSON(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// jsonEqual membandingkan dua nilai JSON secara semantik. Bilangan dibandingkan
// sebagai rasional, sehingga 1 dan 1.0 dianggap sama.
func jsonEqual(a, b any) bool {
	switch av := a.(type) {
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return false
		}
		ar, aok := new(big.Rat).SetString(av.String())
		br, bok := new(big.Rat).SetString(bv.String())
		return aok && bok && ar.Cmp(br) == 0
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, item := range av {
			other, exists := bv[k]
			if !exists || !jsonEqual(item, other) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

// ratFromNumber mengubah json.Number menjadi rasional. Dipakai validator dan
// aturan lintas kunci agar perbandingan tidak bergantung presisi float64.
func ratFromNumber(n json.Number) (*big.Rat, bool) {
	return new(big.Rat).SetString(n.String())
}

// numberFromJSON mengambil angka dari RawMessage. Mengembalikan false untuk
// nilai null, string, maupun JSON yang tidak dapat diurai.
func numberFromJSON(raw json.RawMessage) (*big.Rat, bool) {
	v, err := decodeJSON(raw)
	if err != nil {
		return nil, false
	}
	n, ok := v.(json.Number)
	if !ok {
		return nil, false
	}
	return ratFromNumber(n)
}
