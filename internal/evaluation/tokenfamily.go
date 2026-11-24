// Package evaluation berisi fungsi murni untuk logika penilaian dan invariant.
// File ini tidak melakukan I/O: waktu, identifier, dan hash token baru diterima
// sebagai argumen agar dapat diuji dengan property-based test (Requirement 26.4).
package evaluation

import "time"

// Alasan pencabutan Refresh_Token. Nilai ini harus sama dengan constraint
// CHECK pada kolom refresh_tokens.revoked_reason.
const (
	RevokeRotated         = "rotated"
	RevokeLogout          = "logout"
	RevokeReuseDetected   = "reuse_detected"
	RevokeUserDeactivated = "user_deactivated"
	RevokePasswordChanged = "password_changed"
)

// TokenState adalah keadaan satu Refresh_Token dalam sebuah Token_Family.
// Hash adalah SHA-256 dari token opaque. RevokedAt bernilai zero bila token
// masih aktif; ReplacedBy adalah hash token pengganti setelah rotasi sah.
type TokenState struct {
	Hash          string
	ExpiresAt     time.Time
	RevokedAt     time.Time
	RevokedReason string
	ReplacedBy    string
}

// Active melaporkan apakah token masih aktif pada waktu now. Token yang
// kedaluwarsa tidak dianggap aktif, tetapi tidak di-revoke oleh fungsi murni.
func (t TokenState) Active(now time.Time) bool {
	return t.RevokedAt.IsZero() && t.ExpiresAt.After(now)
}

// FamilyState adalah keadaan seluruh Token_Family. Pemanggil bertugas
// menyalin nilai ini sebelum memodifikasi bila perlu menyimpan versi lama.
type FamilyState struct {
	ID     string
	UserID string
	Tokens []TokenState
}

// ActiveCount menghitung jumlah token aktif pada waktu now.
func (f FamilyState) ActiveCount(now time.Time) int {
	n := 0
	for _, t := range f.Tokens {
		if t.Active(now) {
			n++
		}
	}
	return n
}

// HasHash melaporkan apakah family memuat token dengan hash tertentu.
func (f FamilyState) HasHash(hash string) bool {
	for _, t := range f.Tokens {
		if t.Hash == hash {
			return true
		}
	}
	return false
}

// HashedToken baru dipakai oleh RotateRefresh. expiresAt adalah kedaluwarsa
// token baru dan now adalah waktu kejadian; keduanya disuntikkan pemanggil.
type HashedToken struct {
	Hash      string
	ExpiresAt time.Time
}

// Hasil rotasi Refresh_Token (Requirement 8.3, 8.4, 8.5, 8.11).
const (
	RotateIssued        = "issued"
	RotateReuseDetected = "reuse_detected"
	RotateInvalid       = "invalid"
)

// RotateResult adalah hasil RotateRefresh. Family selalu berisi keadaan
// setelah operasi: pada reuse seluruh token dicabut, pada invalid keadaan
// tidak berubah, pada issued token lama dicabut dan token baru ditambahkan.
type RotateResult struct {
	Outcome   string
	Family    FamilyState
	ErrorCode string
}

// RotateRefresh memproses satu permintaan refresh. Karena server tidak punya
// grace window (D-14), token dengan RevokedAt terisi -- termasuk yang dicabut
// oleh rotasi sah -- selalu dianggap reuse, berapa pun selisih waktunya.
//
// Aturan:
//   - hash tidak dikenal atau kedaluwarsa -> invalid, family tidak berubah;
//   - hash sudah dicabut -> seluruh family dicabut dengan alasan reuse_detected;
//   - selain itu -> token lama dicabut (rotated), replaced_by diisi hash baru,
//     dan token baru ditambahkan pada family yang sama.
func RotateRefresh(family FamilyState, presentedHash string, next HashedToken, now time.Time) RotateResult {
	idx := -1
	for i := range family.Tokens {
		if family.Tokens[i].Hash == presentedHash {
			idx = i
			break
		}
	}
	if idx < 0 {
		return RotateResult{Outcome: RotateInvalid, Family: family, ErrorCode: "invalid_refresh_token"}
	}

	current := family.Tokens[idx]
	// Tanpa grace window: revoked_at terisi (alasan apa pun) selalu reuse.
	if !current.RevokedAt.IsZero() {
		return RotateResult{
			Outcome:   RotateReuseDetected,
			Family:    RevokeFamily(family, now, RevokeReuseDetected),
			ErrorCode: "refresh_token_reuse_detected",
		}
	}
	if !current.ExpiresAt.After(now) {
		return RotateResult{Outcome: RotateInvalid, Family: family, ErrorCode: "invalid_refresh_token"}
	}

	// Salin family agar pemanggil dapat menyimpan keadaan lama bila perlu.
	updated := cloneFamily(family)
	updated.Tokens[idx].RevokedAt = now
	updated.Tokens[idx].RevokedReason = RevokeRotated
	updated.Tokens[idx].ReplacedBy = next.Hash
	updated.Tokens = append(updated.Tokens, TokenState{
		Hash:      next.Hash,
		ExpiresAt: next.ExpiresAt,
	})
	return RotateResult{Outcome: RotateIssued, Family: updated}
}

// RevokeFamily mencabut seluruh token aktif dalam family. Token yang sudah
// dicabut tidak ditimpa alasannya, sehingga jejak reuse tidak hilang.
func RevokeFamily(family FamilyState, now time.Time, reason string) FamilyState {
	updated := cloneFamily(family)
	for i := range updated.Tokens {
		if updated.Tokens[i].RevokedAt.IsZero() {
			updated.Tokens[i].RevokedAt = now
			updated.Tokens[i].RevokedReason = reason
		}
	}
	return updated
}

func cloneFamily(f FamilyState) FamilyState {
	out := f
	out.Tokens = append([]TokenState(nil), f.Tokens...)
	return out
}
