package pagination

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrInvalidCursor dikembalikan bila cursor tidak dapat di-decode.
var ErrInvalidCursor = errors.New("pagination: cursor tidak valid")

// maxCursorLen membatasi panjang cursor sebelum di-decode. Cursor keyset
// yang sah jauh lebih pendek dari batas ini.
const maxCursorLen = 1024

// cursorEnvelope adalah bentuk JSON sebelum dikodekan base64url: {"k":[...]}.
// Nilai disimpan sebagai json.RawMessage agar tipe asli (string, angka) tidak
// berubah saat decode. Angka tidak melewati float64.
type cursorEnvelope struct {
	K []json.RawMessage `json:"k"`
}

// EncodeCursor membentuk cursor dari nilai keyset sesuai urutan sort, misalnya
// (created_at, id). Nilai harus berupa tipe yang dapat di-marshal JSON dan tidak
// boleh nil. Hasilnya base64url tanpa padding.
func EncodeCursor(keys ...any) (string, error) {
	if len(keys) == 0 {
		return "", errors.New("pagination: cursor memerlukan minimal satu kunci keyset")
	}
	raw := make([]json.RawMessage, len(keys))
	for i, k := range keys {
		if k == nil {
			return "", fmt.Errorf("pagination: kunci keyset ke-%d bernilai nil", i)
		}
		b, err := json.Marshal(k)
		if err != nil {
			return "", fmt.Errorf("pagination: kunci keyset ke-%d tidak dapat di-marshal: %w", i, err)
		}
		if string(b) == "null" {
			return "", fmt.Errorf("pagination: kunci keyset ke-%d bernilai null", i)
		}
		raw[i] = b
	}
	payload, err := json.Marshal(cursorEnvelope{K: raw})
	if err != nil {
		return "", fmt.Errorf("pagination: cursor tidak dapat di-marshal: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeCursor memvalidasi cursor dan mengembalikan nilai keyset mentah.
// arity adalah jumlah kunci keyset yang diharapkan. Setiap cursor yang rusak,
// memiliki field tidak dikenal, berisi data setelah JSON, atau memuat nilai
// selain string dan angka, menghasilkan ErrInvalidCursor.
func DecodeCursor(s string, arity int) ([]json.RawMessage, error) {
	if arity < 1 {
		return nil, errors.New("pagination: arity keyset harus minimal 1")
	}
	if s == "" || len(s) > maxCursorLen {
		return nil, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}

	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var env cursorEnvelope
	if err := dec.Decode(&env); err != nil {
		return nil, ErrInvalidCursor
	}
	// Harus tidak ada token lain setelah objek JSON.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidCursor
	}
	if len(env.K) != arity {
		return nil, ErrInvalidCursor
	}
	for _, k := range env.K {
		var v any
		if err := json.Unmarshal(k, &v); err != nil {
			return nil, ErrInvalidCursor
		}
		switch v.(type) {
		case string, float64:
		default:
			return nil, ErrInvalidCursor
		}
	}
	return env.K, nil
}

// CursorRequest adalah parameter paginasi cursor yang sudah tervalidasi.
type CursorRequest struct {
	// Limit adalah jumlah item per halaman.
	Limit int
	// Keys berisi nilai keyset dari cursor. Nil pada halaman pertama.
	Keys []json.RawMessage
}

// QueryLimit mengembalikan jumlah baris yang harus diambil dari penyimpanan,
// yaitu Limit + 1. Baris ekstra menandakan adanya halaman berikutnya.
func (r CursorRequest) QueryLimit() int {
	return r.Limit + 1
}

// CursorPageInfo adalah objek `page` pada response cursor.
type CursorPageInfo struct {
	Limit      int     `json:"limit"`
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// CursorPage adalah response daftar dengan cursor pagination.
type CursorPage[T any] struct {
	Items []T            `json:"items"`
	Page  CursorPageInfo `json:"page"`
}

// NewCursorPage membentuk halaman dari baris yang diambil dengan QueryLimit.
// Bila baris lebih dari limit, baris ekstra dibuang, has_more menjadi true, dan
// next_cursor dibentuk dari nilai keyset item terakhir yang dikembalikan.
// keyOf mengembalikan nilai keyset item sesuai urutan sort. Items tidak pernah nil.
func NewCursorPage[T any](fetched []T, limit int, keyOf func(T) []any) (CursorPage[T], error) {
	if limit < 1 {
		return CursorPage[T]{}, errors.New("pagination: limit harus minimal 1")
	}
	page := CursorPage[T]{Page: CursorPageInfo{Limit: limit}}
	if len(fetched) > limit {
		items := fetched[:limit]
		next, err := EncodeCursor(keyOf(items[limit-1])...)
		if err != nil {
			return CursorPage[T]{}, err
		}
		page.Items = items
		page.Page.NextCursor = &next
		page.Page.HasMore = true
	} else {
		page.Items = fetched
	}
	if page.Items == nil {
		page.Items = []T{}
	}
	return page, nil
}

// ParseCursorQuery memvalidasi query `limit` dan `cursor` untuk jenis daftar kind.
// Nilai kosong berarti default untuk limit dan halaman pertama untuk cursor.
// keyArity adalah jumlah kunci keyset pada cursor. Semua kesalahan parameter
// dikumpulkan dalam satu *ValidationError.
func ParseCursorQuery(kind ListKind, limit, cursor string, keyArity int) (CursorRequest, error) {
	l, err := LimitsFor(kind)
	if err != nil {
		return CursorRequest{}, err
	}
	if keyArity < 1 {
		return CursorRequest{}, errors.New("pagination: arity keyset harus minimal 1")
	}

	var details []FieldError
	n, fe := parseLimit(limit, l)
	if fe != nil {
		details = append(details, *fe)
	}

	req := CursorRequest{Limit: n}
	if cursor != "" {
		keys, err := DecodeCursor(cursor, keyArity)
		if err != nil {
			details = append(details, FieldError{Field: "cursor", Reason: reasonInvalidCursor})
		} else {
			req.Keys = keys
		}
	}
	if err := newValidationError(details); err != nil {
		return CursorRequest{}, err
	}
	return req, nil
}
