package auth

import (
	"time"

	"github.com/google/uuid"
)

// User adalah baris tabel users. PasswordHash sengaja dipisah dari UserView
// agar tidak pernah masuk ke response (Requirement 18.1).
type User struct {
	ID              uuid.UUID
	Email           string
	PasswordHash    string
	DisplayName     string
	Role            string
	PreferredLocale *string
	IsActive        bool
	LastLoginAt     *time.Time
}

// UserView adalah DTO publik untuk user. Tidak memuat hash maupun token.
type UserView struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	Role            string     `json:"role"`
	PreferredLocale *string    `json:"preferred_locale"`
	IsActive        bool       `json:"is_active"`
	LastLoginAt     *time.Time `json:"last_login_at"`
}

func (u User) View() UserView {
	return UserView{
		ID:              u.ID.String(),
		Email:           u.Email,
		DisplayName:     u.DisplayName,
		Role:            u.Role,
		PreferredLocale: u.PreferredLocale,
		IsActive:        u.IsActive,
		LastLoginAt:     u.LastLoginAt,
	}
}

// TokenPair adalah response penerbitan/rotasi token.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// Invitation adalah baris tabel invitations. CodeHash tidak pernah dikirim ke
// client; Invite_Code asli hanya dikembalikan satu kali saat pembuatan.
type Invitation struct {
	ID        uuid.UUID
	CodeHash  []byte
	CreatedBy uuid.UUID
	Email     *string
	Role      string
	ExpiresAt time.Time
	UsedAt    *time.Time
	UsedBy    *uuid.UUID
	RevokedAt *time.Time
}

// InvitationView adalah DTO publik untuk undangan.
type InvitationView struct {
	ID        string     `json:"id"`
	Email     *string    `json:"email"`
	Role      string     `json:"role"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

// CreatedInvitation menambahkan Invite_Code asli yang hanya muncul sekali.
type CreatedInvitation struct {
	InvitationView
	Code string `json:"code"`
}

func (v Invitation) View() InvitationView {
	return InvitationView{
		ID:        v.ID.String(),
		Email:     v.Email,
		Role:      v.Role,
		ExpiresAt: v.ExpiresAt,
		UsedAt:    v.UsedAt,
		RevokedAt: v.RevokedAt,
	}
}

// RefreshToken adalah baris tabel refresh_tokens.
type RefreshToken struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	FamilyID      uuid.UUID
	TokenHash     []byte
	ExpiresAt     time.Time
	RevokedAt     *time.Time
	RevokedReason *string
	ReplacedBy    *uuid.UUID
}
