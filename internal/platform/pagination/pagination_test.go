package pagination

import (
	"errors"
	"slices"
	"testing"
)

// fieldErrorsOf mengambil details dari ValidationError, atau nil bila err bukan ValidationError.
func fieldErrorsOf(err error) []FieldError {
	var ve *ValidationError
	if !errors.As(err, &ve) {
		return nil
	}
	return ve.Details
}

func TestLimitsForMatchesDesignTable(t *testing.T) {
	tests := []struct {
		kind ListKind
		want Limits
	}{
		{KindLearnerContent, Limits{Default: 20, Max: 100}},
		{KindSessionList, Limits{Default: 20, Max: 50}},
		{KindTranscript, Limits{Default: 50, Max: 200}},
		{KindLearnerRecords, Limits{Default: 50, Max: 200}},
		{KindAdmin, Limits{Default: 25, Max: 100}},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			got, err := LimitsFor(tt.kind)
			if err != nil {
				t.Fatalf("LimitsFor(%q) error = %v", tt.kind, err)
			}
			if got != tt.want {
				t.Fatalf("LimitsFor(%q) = %+v, want %+v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestLimitsForUnknownKindIsNotValidationError(t *testing.T) {
	_, err := LimitsFor("tidak_ada")
	if err == nil {
		t.Fatal("LimitsFor(unknown) error = nil, want error")
	}
	if IsValidationError(err) {
		t.Fatal("LimitsFor(unknown) mengembalikan ValidationError, seharusnya error pemanggil")
	}
}

func TestParseCursorQueryLimit(t *testing.T) {
	// KindLearnerContent: default 20, maksimum 100.
	tests := []struct {
		name        string
		limit       string
		wantLimit   int
		wantDetails []FieldError
	}{
		{"kosong memakai default", "", 20, nil},
		{"batas bawah 1", "1", 1, nil},
		{"batas atas 100", "100", 100, nil},
		{"nol ditolak", "0", 0, []FieldError{{"limit", "min_1"}}},
		{"negatif ditolak", "-3", 0, []FieldError{{"limit", "min_1"}}},
		{"di atas maksimum ditolak", "101", 0, []FieldError{{"limit", "max_100"}}},
		{"bukan angka ditolak", "abc", 0, []FieldError{{"limit", "integer"}}},
		{"desimal ditolak", "1.5", 0, []FieldError{{"limit", "integer"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := ParseCursorQuery(KindLearnerContent, tt.limit, "", 2)
			if got := fieldErrorsOf(err); !slices.Equal(got, tt.wantDetails) {
				t.Fatalf("details = %+v, want %+v (err=%v)", got, tt.wantDetails, err)
			}
			if tt.wantDetails == nil {
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				if req.Limit != tt.wantLimit {
					t.Fatalf("Limit = %d, want %d", req.Limit, tt.wantLimit)
				}
			}
		})
	}
}

func TestParseCursorQueryCollectsAllErrors(t *testing.T) {
	_, err := ParseCursorQuery(KindLearnerContent, "0", "bukan-cursor", 2)
	want := []FieldError{
		{"limit", "min_1"},
		{"cursor", "invalid_cursor"},
	}
	if got := fieldErrorsOf(err); !slices.Equal(got, want) {
		t.Fatalf("details = %+v, want %+v", got, want)
	}
}

func TestParseCursorQueryFirstPageHasNoKeys(t *testing.T) {
	req, err := ParseCursorQuery(KindSessionList, "", "", 2)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if req.Limit != 20 {
		t.Fatalf("Limit = %d, want default 20 untuk KindSessionList", req.Limit)
	}
	if req.Keys != nil {
		t.Fatalf("Keys = %v, want nil pada halaman pertama", req.Keys)
	}
}

func TestParseCursorQueryAcceptsEncodedCursor(t *testing.T) {
	enc, err := EncodeCursor("2025-01-31T08:00:00Z", "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b")
	if err != nil {
		t.Fatalf("EncodeCursor error = %v", err)
	}
	req, err := ParseCursorQuery(KindTranscript, "", enc, 2)
	if err != nil {
		t.Fatalf("ParseCursorQuery error = %v", err)
	}
	if len(req.Keys) != 2 {
		t.Fatalf("len(Keys) = %d, want 2", len(req.Keys))
	}
	if req.Limit != 50 {
		t.Fatalf("Limit = %d, want default 50 untuk KindTranscript", req.Limit)
	}
}

func TestParseCursorQueryRejectsWrongArity(t *testing.T) {
	enc, err := EncodeCursor("a")
	if err != nil {
		t.Fatalf("EncodeCursor error = %v", err)
	}
	_, err = ParseCursorQuery(KindLearnerContent, "", enc, 2)
	want := []FieldError{{"cursor", "invalid_cursor"}}
	if got := fieldErrorsOf(err); !slices.Equal(got, want) {
		t.Fatalf("details = %+v, want %+v", got, want)
	}
}

func TestParseCursorQueryRejectsProgrammingErrors(t *testing.T) {
	if _, err := ParseCursorQuery("tidak_ada", "", "", 1); err == nil || IsValidationError(err) {
		t.Fatalf("kind tidak dikenal: error = %v, want error non-validasi", err)
	}
	if _, err := ParseCursorQuery(KindLearnerContent, "", "", 0); err == nil || IsValidationError(err) {
		t.Fatalf("arity 0: error = %v, want error non-validasi", err)
	}
}

func TestParseOffsetQuery(t *testing.T) {
	// KindAdmin: default limit 25, maksimum 100, offset default 0.
	tests := []struct {
		name        string
		limit       string
		offset      string
		wantLimit   int
		wantOffset  int
		wantDetails []FieldError
	}{
		{"kosong memakai default", "", "", 25, 0, nil},
		{"limit dan offset valid", "10", "30", 10, 30, nil},
		{"limit maksimum", "100", "", 100, 0, nil},
		{"limit di atas maksimum", "101", "", 0, 0, []FieldError{{"limit", "max_100"}}},
		{"offset negatif", "", "-1", 0, 0, []FieldError{{"offset", "min_0"}}},
		{"offset bukan angka", "", "x", 0, 0, []FieldError{{"offset", "integer"}}},
		{"keduanya salah", "x", "x", 0, 0, []FieldError{{"limit", "integer"}, {"offset", "integer"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := ParseOffsetQuery(KindAdmin, tt.limit, tt.offset)
			if got := fieldErrorsOf(err); !slices.Equal(got, tt.wantDetails) {
				t.Fatalf("details = %+v, want %+v (err=%v)", got, tt.wantDetails, err)
			}
			if tt.wantDetails == nil {
				if req.Limit != tt.wantLimit || req.Offset != tt.wantOffset {
					t.Fatalf("req = %+v, want limit=%d offset=%d", req, tt.wantLimit, tt.wantOffset)
				}
			}
		})
	}
}
