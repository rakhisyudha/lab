package evaluation

import (
	"fmt"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestPropW5_RotasiRefreshMenjagaSatuTokenAktifDanMendeteksiReuse adalah
// Property 5 (wajib, area 8: invariant autentikasi token). Urutan operasi acak
// dijalankan terhadap model Token_Family in-memory. Tidak ada grace window:
// pemakaian ulang pada selisih waktu nol atau lebih selalu reuse.
func TestPropW5_RotasiRefreshMenjagaSatuTokenAktifDanMendeteksiReuse(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		family := FamilyState{
			ID:     "family-1",
			UserID: "user-1",
			Tokens: []TokenState{{Hash: "token-0", ExpiresAt: now.Add(24 * time.Hour)}},
		}
		revoked := map[string]bool{}
		counter := 0

		ops := rapid.SliceOfN(rapid.IntRange(0, 5), 1, 40).Draw(t, "operasi")
		for _, op := range ops {
			switch op {
			case 0: // refresh dengan token aktif
				hash := activeHash(family, now)
				if hash == "" {
					continue
				}
				counter++
				next := HashedToken{
					Hash:      fmt.Sprintf("token-%d", counter),
					ExpiresAt: now.Add(24 * time.Hour),
				}
				res := RotateRefresh(family, hash, next, now)
				if res.Outcome != RotateIssued {
					t.Fatalf("refresh token aktif %q: outcome=%s, want issued", hash, res.Outcome)
				}
				revoked[hash] = true
				family = res.Family

			case 1: // refresh dengan token yang sudah dicabut (reuse)
				hash := randomKey(t, revoked, "hash_yang_dicabut")
				if hash == "" {
					continue
				}
				counter++
				next := HashedToken{
					Hash:      fmt.Sprintf("token-%d", counter),
					ExpiresAt: now.Add(24 * time.Hour),
				}
				res := RotateRefresh(family, hash, next, now)
				if res.Outcome != RotateReuseDetected {
					t.Fatalf("reuse %q: outcome=%s, want reuse_detected", hash, res.Outcome)
				}
				if res.ErrorCode != "refresh_token_reuse_detected" {
					t.Fatalf("reuse %q: error_code=%q", hash, res.ErrorCode)
				}
				family = res.Family

			case 2: // refresh dengan token tidak dikenal
				before := cloneFamily(family)
				next := HashedToken{Hash: "token-unknown", ExpiresAt: now.Add(time.Hour)}
				res := RotateRefresh(family, fmt.Sprintf("tidak-dikenal-%d", counter), next, now)
				if res.Outcome != RotateInvalid {
					t.Fatalf("token tidak dikenal: outcome=%s, want invalid", res.Outcome)
				}
				if !sameFamily(before, res.Family) {
					t.Fatal("token tidak dikenal tidak boleh mengubah family")
				}

			case 3: // logout / pencabutan seluruh family
				family = RevokeFamily(family, now, RevokeLogout)

			case 4: // peristiwa waktu
				now = now.Add(time.Duration(rapid.IntRange(1, 3600).Draw(t, "detik")) * time.Second)
			}

			// Invariant utama: paling banyak satu token aktif per family.
			if n := family.ActiveCount(now); n > 1 {
				t.Fatalf("family memiliki %d token aktif, want <= 1: %+v", n, family.Tokens)
			}
		}
	})
}

// TestRotateRefreshKedaluwarsaMemastikan token kedaluwarsa ditolak tanpa
// mencabut family (Requirement 8.5).
func TestRotateRefreshKedaluwarsa(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	family := FamilyState{
		ID: "f",
		Tokens: []TokenState{
			{Hash: "lama", ExpiresAt: now.Add(-time.Second)},
			{Hash: "aktif", ExpiresAt: now.Add(time.Hour)},
		},
	}
	res := RotateRefresh(family, "lama", HashedToken{Hash: "baru", ExpiresAt: now.Add(time.Hour)}, now)
	if res.Outcome != RotateInvalid || res.ErrorCode != "invalid_refresh_token" {
		t.Fatalf("outcome=%s error=%q, want invalid/invalid_refresh_token", res.Outcome, res.ErrorCode)
	}
	if !res.Family.Tokens[1].Active(now) {
		t.Fatal("token aktif lain tidak boleh terpengaruh oleh token kedaluwarsa")
	}
}

// TestRotateRefreshTanpaGraceWindow memastikan pemakaian ulang token yang
// baru saja dirotasi (selisih waktu nol) tetap dianggap reuse.
func TestRotateRefreshTanpaGraceWindow(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	family := FamilyState{ID: "f", Tokens: []TokenState{{Hash: "a", ExpiresAt: now.Add(time.Hour)}}}
	first := RotateRefresh(family, "a", HashedToken{Hash: "b", ExpiresAt: now.Add(time.Hour)}, now)
	if first.Outcome != RotateIssued {
		t.Fatalf("rotasi pertama: %s", first.Outcome)
	}
	second := RotateRefresh(first.Family, "a", HashedToken{Hash: "c", ExpiresAt: now.Add(time.Hour)}, now)
	if second.Outcome != RotateReuseDetected || second.Family.ActiveCount(now) != 0 {
		t.Fatalf("reuse tanpa grace window: outcome=%s active=%d", second.Outcome, second.Family.ActiveCount(now))
	}
}

func activeHash(f FamilyState, now time.Time) string {
	for _, tok := range f.Tokens {
		if tok.Active(now) {
			return tok.Hash
		}
	}
	return ""
}

func randomKey(t *rapid.T, m map[string]bool, label string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys[rapid.IntRange(0, len(keys)-1).Draw(t, label)]
}

func sameFamily(a, b FamilyState) bool {
	if a.ID != b.ID || a.UserID != b.UserID || len(a.Tokens) != len(b.Tokens) {
		return false
	}
	for i := range a.Tokens {
		if a.Tokens[i] != b.Tokens[i] {
			return false
		}
	}
	return true
}
