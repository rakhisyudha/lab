package httpx

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

type bindAddress struct {
	PostalCode string `json:"postal_code" binding:"required"`
}

type bindBody struct {
	Email       string      `json:"email" binding:"required,email"`
	Password    string      `json:"password" binding:"required,min=12"`
	DisplayName string      `json:"display_name" binding:"required"`
	Address     bindAddress `json:"address"`
	Count       int         `json:"count"`
}

// bindHandler memanggil BindJSON dan langsung menulis error atau 204 bila berhasil.
func bindHandler(t *testing.T) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in bindBody
		if err := BindJSON(c, &in); err != nil {
			Fail(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func bindRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	env := newTestEnv(t, Options{})
	env.api.POST("/bind", bindHandler(t))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bind", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return env.do(req)
}

type fieldReason struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func detailsOf(t *testing.T, w *httptest.ResponseRecorder) []fieldReason {
	t.Helper()
	var raw struct {
		Error struct {
			Code    string        `json:"code"`
			Details []fieldReason `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body bukan envelope: %v; body=%s", err, w.Body.String())
	}
	if raw.Error.Code != "validation_error" {
		t.Fatalf("code = %q, want validation_error; body=%s", raw.Error.Code, w.Body.String())
	}
	return raw.Error.Details
}

func hasDetail(details []fieldReason, field, reason string) bool {
	for _, d := range details {
		if d.Field == field && d.Reason == reason {
			return true
		}
	}
	return false
}

func TestBindJSONValidBody(t *testing.T) {
	w := bindRequest(t, `{"email":"a@b.co","password":"sangat-panjang-12","display_name":"Rakhi","address":{"postal_code":"40115"},"count":3}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

func TestBindJSONMissingAndInvalidFieldsMapToDetails(t *testing.T) {
	w := bindRequest(t, `{"email":"bukan-email","password":"pendek","address":{}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	details := detailsOf(t, w)

	checks := []struct{ field, reason string }{
		{"email", "email"},
		{"password", "min_12"},
		{"display_name", "required"},
		{"address.postal_code", "required"},
	}
	for _, c := range checks {
		if !hasDetail(details, c.field, c.reason) {
			t.Errorf("details %v tidak memuat {%s, %s}", details, c.field, c.reason)
		}
	}
}

func TestBindJSONSyntaxError(t *testing.T) {
	w := bindRequest(t, `{"email":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !hasDetail(detailsOf(t, w), "body", "invalid_json") {
		t.Errorf("details tidak memuat {body, invalid_json}: %s", w.Body.String())
	}
}

func TestBindJSONTypeMismatch(t *testing.T) {
	w := bindRequest(t, `{"count":"tiga"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !hasDetail(detailsOf(t, w), "count", "invalid_type") {
		t.Errorf("details tidak memuat {count, invalid_type}: %s", w.Body.String())
	}
}

func TestBindJSONEmptyBody(t *testing.T) {
	w := bindRequest(t, ``)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !hasDetail(detailsOf(t, w), "body", "required") {
		t.Errorf("details tidak memuat {body, required}: %s", w.Body.String())
	}
}

func TestBindJSONUnknownBodyErrorFallsBackToInvalidBody(t *testing.T) {
	// Sumber error yang bukan JSON maupun validator dipetakan ke invalid_body.
	got := mapBindError(io.ErrClosedPipe)
	if got.Code != apperr.CodeValidationError {
		t.Fatalf("code = %q, want validation_error", got.Code)
	}
	fields, ok := got.Details.([]apperr.FieldError)
	if !ok || len(fields) != 1 || fields[0].Reason != "invalid_body" {
		t.Errorf("details = %v, want [{body invalid_body}]", got.Details)
	}
}

func TestBindJSONOversizedBodyMapsToPayloadTooLarge(t *testing.T) {
	env := newTestEnv(t, Options{})
	env.api.POST("/bind", bindHandler(t))

	// Panjang tidak diketahui (ContentLength -1) sehingga batas diterapkan oleh MaxBytesReader.
	body := io.MultiReader(
		strings.NewReader(`{"display_name":"`),
		strings.NewReader(strings.Repeat("a", int(MaxBodyBytes))),
		strings.NewReader(`"}`),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bind", body)
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	w := env.do(req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", w.Code, w.Body.String())
	}
	if got := decodeEnvelope(t, w.Body.Bytes()); got.Error.Code != "payload_too_large" {
		t.Errorf("code = %q, want payload_too_large", got.Error.Code)
	}
}

func TestToSnakeCase(t *testing.T) {
	cases := map[string]string{
		"Email":           "email",
		"DisplayName":     "display_name",
		"UserID":          "user_id",
		"ID":              "id",
		"RefreshTokenTTL": "refresh_token_ttl",
		"PostalCode2":     "postal_code2",
		"alreadySnake":    "already_snake",
	}
	for in, want := range cases {
		if got := toSnakeCase(in); got != want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}
