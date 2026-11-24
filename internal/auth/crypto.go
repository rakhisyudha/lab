// Package auth menangani registrasi, login, token, dan undangan invite-only.
// File ini berisi primitif kriptografi: argon2id, pembuatan token acak, dan hash
// SHA-256 (Requirement 7.4, 7.5, 18.2).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Parameter argon2id (Requirement 7.4). PHC string menyimpan parameter agar
// dapat dinaikkan kemudian dan hash lama diperbarui saat login.
const (
	argonMemory      = 64 * 1024 // KiB
	argonIterations  = 3
	argonParallelism = 2
	argonSaltLen     = 16
	argonKeyLen      = 32
	tokenBytes       = 32
)

// ErrInvalidHashFormat menandakan PHC string argon2id yang tidak dikenal.
var ErrInvalidHashFormat = errors.New("auth: format hash argon2id tidak valid")

// HashPassword menghasilkan PHC string argon2id dengan parameter standar.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, uint8(argonParallelism), argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword memeriksa password terhadap PHC string. Nilai kedua melaporkan
// apakah hash perlu diperbarui (parameter berubah atau algoritma berbeda).
func VerifyPassword(password, encoded string) (ok bool, needsRehash bool) {
	memory, iterations, parallelism, salt, want, err := parsePHC(encoded)
	if err != nil {
		return false, false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, uint8(parallelism), uint32(len(want)))
	match := subtle.ConstantTimeCompare(got, want) == 1
	current := memory == argonMemory && iterations == argonIterations && parallelism == argonParallelism && len(want) == argonKeyLen
	return match, match && !current
}

func parsePHC(encoded string) (memory, iterations, parallelism uint32, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	// Format: ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, key]
	if len(parts) != 6 || parts[1] != "argon2id" || !strings.HasPrefix(parts[2], "v=") {
		return 0, 0, 0, nil, nil, ErrInvalidHashFormat
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return 0, 0, 0, nil, nil, ErrInvalidHashFormat
	}
	var m, t, p uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHashFormat
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHashFormat
	}
	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 || m == 0 || t == 0 || p == 0 {
		return 0, 0, 0, nil, nil, ErrInvalidHashFormat
	}
	return m, t, p, salt, key, nil
}

// NewOpaqueToken menghasilkan token opaque 256 bit yang dikodekan base64url
// tanpa padding. Dipakai untuk Invite_Code dan Refresh_Token.
func NewOpaqueToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: token acak: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken menghitung SHA-256 dari token. Karena token memiliki entropi
// 256 bit, hash SHA-256 cukup dan nilai asli tidak perlu disimpan.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
