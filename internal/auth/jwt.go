package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/rakhisyudha/lab/internal/platform/ids"
)

// Claims adalah klaim Access_Token (Requirement 8.1): sub, role, iat, exp, jti.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// ErrInvalidToken menandakan Access_Token yang tidak dapat diverifikasi.
var ErrInvalidToken = errors.New("auth: access token tidak valid")

// TokenIssuer menerbitkan dan memverifikasi Access_Token JWT HS256.
type TokenIssuer struct {
	key []byte
}

// NewTokenIssuer membuat issuer dengan kunci rahasia dari environment.
func NewTokenIssuer(signingKey string) *TokenIssuer {
	return &TokenIssuer{key: []byte(signingKey)}
}

// Issue membuat Access_Token. Masa berlaku ttl dibaca pemanggil dari
// App_Settings, sehingga perubahan TTL hanya berlaku untuk token baru
// (Requirement 8.2).
func (i *TokenIssuer) Issue(userID, role string, ttl time.Duration, now time.Time) (string, error) {
	jti, err := ids.NewV7()
	if err != nil {
		return "", fmt.Errorf("auth: jti: %w", err)
	}
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        jti.String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.key)
	if err != nil {
		return "", fmt.Errorf("auth: menandatangani token: %w", err)
	}
	return signed, nil
}

// Parse memverifikasi tanda tangan, klaim yang wajib, dan masa berlaku.
func (i *TokenIssuer) Parse(raw string) (Claims, error) {
	var claims Claims
	parsed, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("auth: metode penandatanganan tidak didukung")
		}
		return i.key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid {
		return Claims{}, ErrInvalidToken
	}
	if claims.Subject == "" || claims.Role == "" || claims.ExpiresAt == nil || claims.IssuedAt == nil || claims.ID == "" {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}
