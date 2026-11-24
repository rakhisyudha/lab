package httpx

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rakhisyudha/lab/internal/platform/ids"
)

// RequestIDHeader adalah header request dan response untuk Request_ID.
const RequestIDHeader = "X-Request-ID"

// requestIDPattern adalah aturan Request_ID yang diterima dari header klien (design 2.1).
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// ResolveRequestID mengembalikan incoming bila cocok dengan aturan Request_ID.
// Selain itu, server membuat UUID v7 baru.
func ResolveRequestID(incoming string) string {
	if requestIDPattern.MatchString(incoming) {
		return incoming
	}
	return newRequestID()
}

// fallbackSeq dipakai hanya bila sumber acak sistem gagal membuat UUID.
var fallbackSeq atomic.Uint64

// newRequestID membuat UUID v7. Bila pembuatan gagal, id dibentuk dari waktu dan
// penghitung proses agar tetap unik dan lolos aturan pattern.
func newRequestID() string {
	id, err := ids.NewV7()
	if err == nil {
		return id.String()
	}
	return fmt.Sprintf("%016x%016x", time.Now().UnixNano(), fallbackSeq.Add(1))
}

// requestState menyimpan identitas satu request yang dibaca oleh access log.
// Pointer ini dipasang di context.Context request, sehingga service yang menerima
// ctx dapat mengisi user_id tanpa mengenal gin.
type requestState struct {
	id string

	mu     sync.Mutex
	userID string
}

type stateKey struct{}

func withState(ctx context.Context, s *requestState) context.Context {
	return context.WithValue(ctx, stateKey{}, s)
}

func stateFrom(ctx context.Context) *requestState {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(stateKey{}).(*requestState)
	return s
}

// RequestIDFrom mengembalikan Request_ID request pada ctx, atau string kosong bila tidak ada.
func RequestIDFrom(ctx context.Context) string {
	if s := stateFrom(ctx); s != nil {
		return s.id
	}
	return ""
}

// SetUserID mencatat user_id pada request agar muncul di log akses. Nilai kosong
// tidak mengubah apa pun. Pemanggilan tanpa state request juga tidak melakukan apa pun.
func SetUserID(ctx context.Context, userID string) {
	s := stateFrom(ctx)
	if s == nil || userID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userID = userID
}

// UserIDFrom mengembalikan user_id yang dicatat dengan SetUserID, atau string kosong.
func UserIDFrom(ctx context.Context) string {
	s := stateFrom(ctx)
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userID
}

// RequestID memasang Request_ID pada setiap request. Nilai dari header X-Request-ID
// dipakai bila valid, selain itu dibuat baru. Nilai selalu dikembalikan pada header response.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		s := &requestState{id: ResolveRequestID(c.GetHeader(RequestIDHeader))}
		c.Header(RequestIDHeader, s.id)
		c.Request = c.Request.WithContext(withState(c.Request.Context(), s))
		c.Next()
	}
}
