package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestResolveRequestIDKeepsValidHeader(t *testing.T) {
	valid := []string{
		"abcdefgh",                             // 8 karakter, batas bawah
		"req_12345-ABCdef",                     // campuran huruf, angka, _, -
		strings.Repeat("a", 64),                // 64 karakter, batas atas
		"0b7c1c0e-5f6a-4a52-9f0c-5f1d0b6a7d11", // bentuk UUID
	}
	for _, in := range valid {
		if got := ResolveRequestID(in); got != in {
			t.Errorf("ResolveRequestID(%q) = %q, want nilai header dipakai", in, got)
		}
	}
}

func TestResolveRequestIDReplacesInvalidHeader(t *testing.T) {
	invalid := []string{
		"",
		"abcdefg",               // 7 karakter
		strings.Repeat("a", 65), // 65 karakter
		"abc defgh",             // spasi
		"abc.defgh",             // titik
		"abc\ndefgh",            // newline
		"abcdefgh!",             // karakter tidak diizinkan
	}
	uuidShape := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	for _, in := range invalid {
		got := ResolveRequestID(in)
		if got == in {
			t.Errorf("ResolveRequestID(%q) memakai header yang tidak valid", in)
		}
		if !uuidShape.MatchString(got) {
			t.Errorf("ResolveRequestID(%q) = %q, want UUID baru", in, got)
		}
		if v, err := uuid.Parse(got); err != nil || v.Version() != 7 {
			t.Errorf("ResolveRequestID(%q) = %q, want UUID v7", in, got)
		}
	}
}

func TestResolveRequestIDGeneratesUniqueValues(t *testing.T) {
	a := ResolveRequestID("")
	b := ResolveRequestID("")
	if a == b {
		t.Fatalf("dua request berbeda menghasilkan Request_ID sama: %q", a)
	}
}

func TestRequestIDMiddlewareSetsHeaderAndContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine, api := NewRouter(Options{Logger: discardLogger()})
	var fromCtx string
	api.GET("/echo", func(c *gin.Context) {
		fromCtx = RequestIDFrom(c.Request.Context())
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/echo", nil)
	req.Header.Set(RequestIDHeader, "klien-id-12345")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if got := w.Header().Get(RequestIDHeader); got != "klien-id-12345" {
		t.Errorf("header response = %q, want nilai header request", got)
	}
	if fromCtx != "klien-id-12345" {
		t.Errorf("RequestIDFrom(ctx) = %q, want sama dengan header", fromCtx)
	}
}

func TestRequestIDMiddlewareReplacesInvalidHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine, _ := NewRouter(Options{Logger: discardLogger()})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set(RequestIDHeader, "x")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	got := w.Header().Get(RequestIDHeader)
	if got == "" || got == "x" {
		t.Fatalf("header response = %q, want Request_ID baru", got)
	}
}

func TestSetUserIDAndUserIDFrom(t *testing.T) {
	if got := UserIDFrom(context.Background()); got != "" {
		t.Errorf("UserIDFrom tanpa state = %q, want kosong", got)
	}
	// Tanpa state request, SetUserID tidak boleh panik.
	SetUserID(context.Background(), "user-1")

	s := &requestState{id: "rid-00000001"}
	ctx := withState(context.Background(), s)
	SetUserID(ctx, "")
	if got := UserIDFrom(ctx); got != "" {
		t.Errorf("SetUserID dengan nilai kosong mengubah user_id menjadi %q", got)
	}
	SetUserID(ctx, "user-42")
	if got := UserIDFrom(ctx); got != "user-42" {
		t.Errorf("UserIDFrom = %q, want user-42", got)
	}
}
