package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// Principal adalah identitas request yang sudah diautentikasi.
type Principal struct {
	UserID string
	Role   string
}

type principalKey struct{}

// PrincipalFrom mengambil Principal dari context request.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Middleware memasang autentikasi Access_Token pada rute Gin.
type Middleware struct {
	issuer *TokenIssuer
}

func NewMiddleware(issuer *TokenIssuer) *Middleware {
	return &Middleware{issuer: issuer}
}

// Authenticate memverifikasi header Authorization: Bearer <Access_Token>.
// Token tidak valid atau tidak ada menghasilkan 401 unauthenticated
// (Requirement 8.8).
func (m *Middleware) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
			return
		}
		claims, err := m.issuer.Parse(parts[1])
		if err != nil {
			httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
			return
		}
		p := Principal{UserID: claims.Subject, Role: claims.Role}
		ctx := context.WithValue(c.Request.Context(), principalKey{}, p)
		c.Request = c.Request.WithContext(ctx)
		httpx.SetUserID(ctx, p.UserID)
		c.Next()
	}
}

// RequireRole membatasi rute pada daftar role. Role yang tidak memenuhi
// menghasilkan 403 forbidden (Requirement 8.9, 10.10).
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(c *gin.Context) {
		p, ok := PrincipalFrom(c.Request.Context())
		if !ok {
			httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
			return
		}
		if _, ok := allowed[p.Role]; !ok {
			httpx.Fail(c, apperr.New(apperr.CodeForbidden))
			return
		}
		c.Next()
	}
}

// ParseUUID mengurai id dari path atau mengembalikan validation_error.
func ParseUUID(c *gin.Context, name, raw string) (uuidValue string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: name, Reason: "required"}))
		return "", false
	}
	// Validasi bentuk UUID tanpa mengimpor uuid di seluruh handler.
	if len(raw) != 36 || raw[8] != '-' || raw[13] != '-' || raw[18] != '-' || raw[23] != '-' {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: name, Reason: "uuid"}))
		return "", false
	}
	return raw, true
}

// noContent menulis 204 tanpa body.
func noContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
