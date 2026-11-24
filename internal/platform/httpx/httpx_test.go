package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// testEnv adalah router uji dengan log JSON yang ditulis ke buf.
type testEnv struct {
	engine *gin.Engine
	api    *gin.RouterGroup
	logs   *bytes.Buffer
}

func newTestEnv(t *testing.T, opts Options) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	buf := &bytes.Buffer{}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewJSONHandler(buf, nil))
	}
	engine, api := NewRouter(opts)
	return &testEnv{engine: engine, api: api, logs: buf}
}

func (e *testEnv) do(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func decodeEnvelope(t *testing.T, body []byte) ErrorEnvelope {
	t.Helper()
	var env ErrorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("body bukan Error_Envelope: %v; body=%s", err, body)
	}
	return env
}

func TestFailWritesEnvelopeWithRequestID(t *testing.T) {
	env := newTestEnv(t, Options{})
	env.api.GET("/x", func(c *gin.Context) {
		Fail(c, apperr.New(apperr.CodeForbidden))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	w := env.do(req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	got := decodeEnvelope(t, w.Body.Bytes())
	if got.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", got.Error.Code)
	}
	if got.Error.Message == "" {
		t.Error("message kosong")
	}
	if got.Error.RequestID == "" || got.Error.RequestID != w.Header().Get(RequestIDHeader) {
		t.Errorf("request_id = %q, want sama dengan header %q", got.Error.RequestID, w.Header().Get(RequestIDHeader))
	}
	if string(mustJSON(t, got.Error.Details)) != "{}" {
		t.Errorf("details = %v, want objek kosong bila tidak ada details", got.Error.Details)
	}
}

func TestFailValidationErrorDetails(t *testing.T) {
	env := newTestEnv(t, Options{})
	env.api.POST("/v", func(c *gin.Context) {
		Fail(c, apperr.Validation(apperr.FieldError{Field: "password", Reason: "min_12"}))
	})

	w := env.do(httptest.NewRequest(http.MethodPost, "/api/v1/v", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var raw struct {
		Error struct {
			Code    string `json:"code"`
			Details []struct {
				Field  string `json:"field"`
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Error.Code != "validation_error" || len(raw.Error.Details) != 1 ||
		raw.Error.Details[0].Field != "password" || raw.Error.Details[0].Reason != "min_12" {
		t.Errorf("body = %s, want validation_error dengan details[{password,min_12}]", w.Body.String())
	}
}

func TestFailPlainErrorBecomesInternalErrorWithoutLeak(t *testing.T) {
	env := newTestEnv(t, Options{})
	const secretDetail = "pq: password authentication failed for user db_admin"
	env.api.GET("/boom", func(c *gin.Context) {
		Fail(c, errors.New(secretDetail))
	})

	w := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/boom", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "password authentication") {
		t.Errorf("detail infrastruktur bocor ke response: %s", w.Body.String())
	}
	got := decodeEnvelope(t, w.Body.Bytes())
	if got.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", got.Error.Code)
	}
	if !strings.Contains(env.logs.String(), "password authentication") {
		t.Error("penyebab error tidak tercatat di log server")
	}
}

func TestFailNilErrorDoesNotPanic(t *testing.T) {
	env := newTestEnv(t, Options{})
	env.api.GET("/nil", func(c *gin.Context) { Fail(c, nil) })

	w := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/nil", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestUnknownRouteReturnsNotFoundEnvelope(t *testing.T) {
	env := newTestEnv(t, Options{})
	w := env.do(httptest.NewRequest(http.MethodGet, "/tidak/ada", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	got := decodeEnvelope(t, w.Body.Bytes())
	if got.Error.Code != "not_found" || got.Error.RequestID == "" {
		t.Errorf("body = %s, want not_found dengan request_id", w.Body.String())
	}
}

func TestPanicBecomesInternalErrorWithoutStack(t *testing.T) {
	env := newTestEnv(t, Options{})
	env.api.GET("/panic", func(c *gin.Context) {
		panic("rahasia-panic-value")
	})

	w := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/panic", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	body := w.Body.String()
	for _, leak := range []string{"goroutine", "runtime/debug", "rahasia-panic-value", "panic"} {
		if strings.Contains(body, leak) {
			t.Errorf("response memuat %q: %s", leak, body)
		}
	}
	if got := decodeEnvelope(t, w.Body.Bytes()); got.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", got.Error.Code)
	}
	logs := env.logs.String()
	if !strings.Contains(logs, "panic_recovered") || !strings.Contains(logs, "stack") {
		t.Errorf("log tidak memuat stack trace panic: %s", logs)
	}
}

func TestAccessLogFields(t *testing.T) {
	env := newTestEnv(t, Options{})
	env.api.GET("/me", func(c *gin.Context) {
		SetUserID(c.Request.Context(), "user-7")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me?token=rahasia-query", nil)
	req.Header.Set(RequestIDHeader, "log-req-00001")
	env.do(req)

	line := lastLogLine(t, env.logs.String())
	if line["request_id"] != "log-req-00001" {
		t.Errorf("request_id = %v", line["request_id"])
	}
	if line["user_id"] != "user-7" {
		t.Errorf("user_id = %v, want user-7", line["user_id"])
	}
	if line["method"] != "GET" || line["path"] != "/api/v1/me" {
		t.Errorf("method/path = %v %v", line["method"], line["path"])
	}
	if line["status"] != float64(200) {
		t.Errorf("status = %v, want 200", line["status"])
	}
	if _, ok := line["duration_ms"]; !ok {
		t.Error("duration_ms tidak ada")
	}
	if strings.Contains(env.logs.String(), "rahasia-query") {
		t.Error("query string ikut tercatat di log")
	}
}

func TestAccessLogOmitsUserIDWhenAnonymous(t *testing.T) {
	env := newTestEnv(t, Options{})
	env.api.GET("/anon", func(c *gin.Context) { c.Status(http.StatusOK) })
	env.do(httptest.NewRequest(http.MethodGet, "/api/v1/anon", nil))

	if _, ok := lastLogLine(t, env.logs.String())["user_id"]; ok {
		t.Error("user_id ada pada request anonim")
	}
}

func TestHealthNoAuthAndNoConfigLeak(t *testing.T) {
	env := newTestEnv(t, Options{HealthCheck: func(ctx context.Context) error { return nil }})
	w := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["status"] != "ok" {
		t.Errorf("body = %v, want hanya {status: ok}", body)
	}
}

func TestHealthUnavailableHidesCause(t *testing.T) {
	env := newTestEnv(t, Options{HealthCheck: func(ctx context.Context) error {
		return errors.New("dial tcp 10.0.0.5:5432: connection refused")
	}})
	w := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if strings.Contains(w.Body.String(), "10.0.0.5") {
		t.Errorf("alamat infrastruktur bocor: %s", w.Body.String())
	}
	if !strings.Contains(env.logs.String(), "connection refused") {
		t.Error("penyebab health gagal tidak tercatat di log")
	}
}

func TestCORSAllowsConfiguredOriginOnly(t *testing.T) {
	env := newTestEnv(t, Options{CORSAllowedOrigins: []string{"https://app.example.com"}})
	env.api.GET("/c", func(c *gin.Context) { c.Status(http.StatusOK) })

	allowed := httptest.NewRequest(http.MethodGet, "/api/v1/c", nil)
	allowed.Header.Set("Origin", "HTTPS://app.example.com")
	w := env.do(allowed)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "HTTPS://app.example.com" {
		t.Errorf("ACAO = %q, want origin yang diizinkan", got)
	}
	if got := w.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, "X-Request-ID") {
		t.Errorf("Expose-Headers = %q, want memuat X-Request-ID", got)
	}

	other := httptest.NewRequest(http.MethodGet, "/api/v1/c", nil)
	other.Header.Set("Origin", "https://evil.example.com")
	w = env.do(other)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO = %q untuk origin tidak diizinkan, want kosong", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	env := newTestEnv(t, Options{CORSAllowedOrigins: []string{"https://app.example.com"}})
	env.api.PATCH("/p", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/p", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	w := env.do(req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status preflight = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "PATCH") {
		t.Errorf("Allow-Methods = %q, want memuat PATCH", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Idempotency-Key") {
		t.Errorf("Allow-Headers = %q, want memuat Idempotency-Key", got)
	}
}

func TestBodyLimitRejectsDeclaredLargeBody(t *testing.T) {
	env := newTestEnv(t, Options{})
	called := false
	env.api.POST("/big", func(c *gin.Context) { called = true; c.Status(http.StatusOK) })

	body := strings.Repeat("a", int(MaxBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/big", strings.NewReader(body))
	w := env.do(req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", w.Code)
	}
	if got := decodeEnvelope(t, w.Body.Bytes()); got.Error.Code != "payload_too_large" {
		t.Errorf("code = %q, want payload_too_large", got.Error.Code)
	}
	if called {
		t.Error("handler dipanggil untuk body di atas batas")
	}
}

func TestBodyLimitAllowsBodyAtLimit(t *testing.T) {
	env := newTestEnv(t, Options{})
	env.api.POST("/ok", func(c *gin.Context) {
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Errorf("ReadAll: %v", err)
		}
		c.String(http.StatusOK, "%d", len(b))
	})

	body := strings.Repeat("a", int(MaxBodyBytes))
	w := env.do(httptest.NewRequest(http.MethodPost, "/api/v1/ok", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 untuk body tepat di batas", w.Code)
	}
}

func TestNewRouterDefaultsMaxBodyBytes(t *testing.T) {
	env := newTestEnv(t, Options{MaxBodyBytes: 0})
	env.api.POST("/d", func(c *gin.Context) { c.Status(http.StatusOK) })

	body := strings.Repeat("a", int(MaxBodyBytes)+1)
	w := env.do(httptest.NewRequest(http.MethodPost, "/api/v1/d", strings.NewReader(body)))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 dengan batas default", w.Code)
	}
}

// discardLogger membuang log, agar output test tetap bersih.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// lastLogLine mengembalikan baris log JSON terakhir sebagai map.
func lastLogLine(t *testing.T, logs string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logs), "\n")
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("baris log bukan JSON: %v; log=%s", err, logs)
	}
	return m
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
