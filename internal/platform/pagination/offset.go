package pagination

import "strconv"

// OffsetRequest adalah parameter paginasi offset yang sudah tervalidasi.
type OffsetRequest struct {
	Limit  int
	Offset int
}

// OffsetPageInfo adalah objek `page` pada response offset.
type OffsetPageInfo struct {
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

// OffsetPage adalah response daftar dengan offset pagination.
type OffsetPage[T any] struct {
	Items []T            `json:"items"`
	Page  OffsetPageInfo `json:"page"`
}

// ParseOffsetQuery memvalidasi query `limit` dan `offset` untuk jenis daftar kind.
// Nilai kosong berarti default: limit sesuai kind, offset 0. Offset harus >= 0.
// Semua kesalahan parameter dikumpulkan dalam satu *ValidationError.
func ParseOffsetQuery(kind ListKind, limit, offset string) (OffsetRequest, error) {
	l, err := LimitsFor(kind)
	if err != nil {
		return OffsetRequest{}, err
	}

	var details []FieldError
	n, fe := parseLimit(limit, l)
	if fe != nil {
		details = append(details, *fe)
	}

	off := 0
	if offset != "" {
		v, err := strconv.Atoi(offset)
		switch {
		case err != nil:
			details = append(details, FieldError{Field: "offset", Reason: reasonInteger})
		case v < 0:
			details = append(details, FieldError{Field: "offset", Reason: reasonMinZero})
		default:
			off = v
		}
	}

	if err := newValidationError(details); err != nil {
		return OffsetRequest{}, err
	}
	return OffsetRequest{Limit: n, Offset: off}, nil
}

// NewOffsetPage membentuk response offset. total adalah jumlah seluruh baris
// yang diambil pemanggil dengan COUNT. Items tidak pernah nil.
func NewOffsetPage[T any](items []T, req OffsetRequest, total int64) OffsetPage[T] {
	if items == nil {
		items = []T{}
	}
	return OffsetPage[T]{
		Items: items,
		Page: OffsetPageInfo{
			Limit:  req.Limit,
			Offset: req.Offset,
			Total:  total,
		},
	}
}
