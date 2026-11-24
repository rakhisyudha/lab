package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type testRow struct {
	ID string
}

func rowKeys(r testRow) []any { return []any{r.ID} }

func TestEncodeDecodeCursorRoundTrip(t *testing.T) {
	enc, err := EncodeCursor("2025-01-31T08:00:00Z", "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b")
	if err != nil {
		t.Fatalf("EncodeCursor error = %v", err)
	}
	keys, err := DecodeCursor(enc, 2)
	if err != nil {
		t.Fatalf("DecodeCursor error = %v", err)
	}
	if string(keys[0]) != `"2025-01-31T08:00:00Z"` {
		t.Fatalf("key[0] = %s, want timestamp string", keys[0])
	}
	if string(keys[1]) != `"0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"` {
		t.Fatalf("key[1] = %s, want uuid string", keys[1])
	}
}

func TestEncodeCursorNumberKeyKeepsPrecision(t *testing.T) {
	// 2^53 + 1 tidak dapat direpresentasikan float64 tanpa kehilangan nilai.
	const big int64 = 9007199254740993
	enc, err := EncodeCursor(big)
	if err != nil {
		t.Fatalf("EncodeCursor error = %v", err)
	}
	keys, err := DecodeCursor(enc, 1)
	if err != nil {
		t.Fatalf("DecodeCursor error = %v", err)
	}
	if string(keys[0]) != "9007199254740993" {
		t.Fatalf("key = %s, want angka tanpa pembulatan", keys[0])
	}
}

func TestEncodeCursorRejectsInvalidKeys(t *testing.T) {
	var nilPtr *string
	tests := []struct {
		name string
		keys []any
	}{
		{"tanpa kunci", nil},
		{"kunci nil", []any{nil}},
		{"pointer nil menjadi null", []any{nilPtr}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := EncodeCursor(tt.keys...); err == nil {
				t.Fatal("EncodeCursor error = nil, want error")
			}
		})
	}
}

// encodeRaw membentuk cursor dari string JSON mentah untuk menguji decoder.
func encodeRaw(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func TestDecodeCursorRejectsMalformed(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
		arity  int
	}{
		{"kosong", "", 1},
		{"bukan base64url", "!!!", 1},
		{"berpadding", base64.URLEncoding.EncodeToString([]byte(`{"k":["a"]}`)), 1},
		{"bukan JSON", encodeRaw("bukan json"), 1},
		{"field tidak dikenal", encodeRaw(`{"k":["a"],"x":1}`), 1},
		{"tanpa field k", encodeRaw(`{}`), 1},
		{"jumlah kunci kurang", encodeRaw(`{"k":["a"]}`), 2},
		{"jumlah kunci lebih", encodeRaw(`{"k":["a","b"]}`), 1},
		{"kunci null", encodeRaw(`{"k":[null]}`), 1},
		{"kunci objek", encodeRaw(`{"k":[{}]}`), 1},
		{"kunci array", encodeRaw(`{"k":[[1]]}`), 1},
		{"kunci bool", encodeRaw(`{"k":[true]}`), 1},
		{"data setelah JSON", encodeRaw(`{"k":["a"]} {}`), 1},
		{"terlalu panjang", strings.Repeat("A", maxCursorLen+1), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeCursor(tt.cursor, tt.arity)
			if !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("DecodeCursor error = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

func TestDecodeCursorAcceptsStringAndNumberKeys(t *testing.T) {
	keys, err := DecodeCursor(encodeRaw(`{"k":["a",1]}`), 2)
	if err != nil {
		t.Fatalf("DecodeCursor error = %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want 2", len(keys))
	}
}

func TestNewCursorPageTrimsAndBuildsNextCursor(t *testing.T) {
	// limit 2, baris yang diambil limit+1 = 3: halaman berisi a,b dan ada halaman berikutnya.
	fetched := []testRow{{"a"}, {"b"}, {"c"}}
	page, err := NewCursorPage(fetched, 2, func(r testRow) []any { return rowKeys(r) })
	if err != nil {
		t.Fatalf("NewCursorPage error = %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != "a" || page.Items[1].ID != "b" {
		t.Fatalf("Items = %+v, want [a b]", page.Items)
	}
	if !page.Page.HasMore {
		t.Fatal("HasMore = false, want true")
	}
	if page.Page.NextCursor == nil {
		t.Fatal("NextCursor = nil, want cursor")
	}
	keys, err := DecodeCursor(*page.Page.NextCursor, 1)
	if err != nil {
		t.Fatalf("DecodeCursor(next) error = %v", err)
	}
	if string(keys[0]) != `"b"` {
		t.Fatalf("next cursor key = %s, want kunci item terakhir \"b\"", keys[0])
	}
	if page.Page.Limit != 2 {
		t.Fatalf("Page.Limit = %d, want 2", page.Page.Limit)
	}
}

func TestNewCursorPageLastPageHasNullCursor(t *testing.T) {
	tests := []struct {
		name    string
		fetched []testRow
		want    int
	}{
		{"jumlah sama dengan limit", []testRow{{"a"}, {"b"}}, 2},
		{"kurang dari limit", []testRow{{"a"}}, 1},
		{"kosong", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := NewCursorPage(tt.fetched, 2, func(r testRow) []any { return rowKeys(r) })
			if err != nil {
				t.Fatalf("NewCursorPage error = %v", err)
			}
			if page.Page.HasMore || page.Page.NextCursor != nil {
				t.Fatalf("Page = %+v, want has_more false dan next_cursor null", page.Page)
			}
			if len(page.Items) != tt.want {
				t.Fatalf("len(Items) = %d, want %d", len(page.Items), tt.want)
			}
		})
	}
}

func TestNewCursorPageRejectsNonPositiveLimit(t *testing.T) {
	_, err := NewCursorPage([]testRow{}, 0, func(r testRow) []any { return rowKeys(r) })
	if err == nil {
		t.Fatal("NewCursorPage(limit 0) error = nil, want error")
	}
}

func TestCursorPageJSONShape(t *testing.T) {
	page, err := NewCursorPage[testRow](nil, 2, func(r testRow) []any { return rowKeys(r) })
	if err != nil {
		t.Fatalf("NewCursorPage error = %v", err)
	}
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	want := `{"items":[],"page":{"limit":2,"next_cursor":null,"has_more":false}}`
	if string(b) != want {
		t.Fatalf("json = %s, want %s", b, want)
	}
}
