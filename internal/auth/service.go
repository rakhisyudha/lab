package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
	"github.com/rakhisyudha/lab/internal/platform/ids"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

// SettingsReader adalah bagian App_Settings yang dipakai auth. Antarmuka ini
// didefinisikan di sisi konsumen agar auth tidak bergantung pada implementasi.
type SettingsReader interface {
	Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
}

// Service mengoordinasikan registrasi, login, rotasi token, dan admin.
type Service struct {
	store          *Store
	issuer         *TokenIssuer
	settings       SettingsReader
	clock          clock.Clock
	passwordPolicy PasswordPolicy
	inviteTTL      time.Duration
}

func NewService(store *Store, issuer *TokenIssuer, s SettingsReader, c clock.Clock,
	policy PasswordPolicy, inviteTTL time.Duration) *Service {
	if policy.MinLength <= 0 {
		policy = DefaultPasswordPolicy()
	}
	if inviteTTL <= 0 {
		inviteTTL = 7 * 24 * time.Hour
	}
	return &Service{store: store, issuer: issuer, settings: s, clock: c,
		passwordPolicy: policy, inviteTTL: inviteTTL}
}

// InviteUsable adalah predikat murni untuk kegunaan Invite_Code (Requirement
// 7.8, 7.9): belum dipakai, belum dicabut, belum kedaluwarsa, dan email cocok
// tanpa membedakan huruf besar-kecil.
func InviteUsable(inv Invitation, email string, now time.Time) bool {
	if inv.UsedAt != nil || inv.RevokedAt != nil || !inv.ExpiresAt.After(now) {
		return false
	}
	if inv.Email == nil {
		return true
	}
	return NormalizeEmail(*inv.Email) == NormalizeEmail(email)
}

// Register membuat user dalam satu transaksi: kunci undangan, insert user,
// tandai undangan terpakai, lalu terbitkan token (Requirement 7.7, 7.10).
func (s *Service) Register(ctx context.Context, code, email, password, displayName string) (TokenPair, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return TokenPair{}, apperr.Validation(apperr.FieldError{Field: "email", Reason: "required"})
	}
	if err := ValidatePassword(password, s.passwordPolicy); err != nil {
		return TokenPair{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return TokenPair{}, err
	}
	now := s.clock.Now()
	var pair TokenPair

	err = db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
		inv, err := s.store.FindInvitationForUpdate(ctx, tx, HashToken(code))
		if errors.Is(err, ErrNotFound) || (err == nil && !InviteUsable(inv, email, now)) {
			return apperr.New(apperr.CodeInvalidInviteCode)
		}
		if err != nil {
			return err
		}
		user, err := s.store.InsertUser(ctx, tx, email, hash, displayName, inv.Role)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return apperr.New(apperr.CodeEmailAlreadyRegistered)
			}
			return err
		}
		if err := s.store.MarkInvitationUsed(ctx, tx, inv.ID, user.ID, now); err != nil {
			return err
		}
		pair, err = s.issuePair(ctx, tx, user)
		return err
	})
	return pair, err
}

// Login memverifikasi kredensial. Pesan error sama untuk email tidak terdaftar
// dan password salah (Requirement 7.11). Hash diperbarui bila parameternya sudah
// usang (rehash on login).
func (s *Service) Login(ctx context.Context, email, password string) (TokenPair, error) {
	user, err := s.store.FindUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return TokenPair{}, apperr.New(apperr.CodeInvalidCredentials)
		}
		return TokenPair{}, err
	}
	ok, needsRehash := VerifyPassword(password, user.PasswordHash)
	if !ok {
		return TokenPair{}, apperr.New(apperr.CodeInvalidCredentials)
	}
	if needsRehash {
		newHash, err := HashPassword(password)
		if err != nil {
			return TokenPair{}, err
		}
		if err := s.store.UpdatePassword(ctx, user.ID, newHash); err != nil {
			return TokenPair{}, err
		}
	}
	if !user.IsActive {
		return TokenPair{}, apperr.New(apperr.CodeInvalidCredentials)
	}
	now := s.clock.Now()
	if err := s.store.TouchLogin(ctx, user.ID, now); err != nil {
		return TokenPair{}, err
	}
	var pair TokenPair
	err = db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
		var err error
		pair, err = s.issuePair(ctx, tx, user)
		return err
	})
	return pair, err
}

// Refresh memutar Refresh_Token. Seluruh logika keputusan ada pada fungsi murni
// evaluation.RotateRefresh; transaksi hanya melakukan SELECT FOR UPDATE dan
// menuliskan hasilnya (Requirement 8.3, 8.4, 8.11, 26.4).
func (s *Service) Refresh(ctx context.Context, presented string) (TokenPair, error) {
	newRaw, err := NewOpaqueToken()
	if err != nil {
		return TokenPair{}, err
	}
	now := s.clock.Now()
	snap, err := s.settings.Snapshot(ctx, "auth.access_token_ttl_seconds", "auth.refresh_token_ttl_seconds")
	if err != nil {
		return TokenPair{}, err
	}
	accessTTL := time.Duration(snap.Int("auth.access_token_ttl_seconds")) * time.Second
	refreshTTL := time.Duration(snap.Int("auth.refresh_token_ttl_seconds")) * time.Second

	var outcome string
	var pair TokenPair

	err = db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
		current, err := s.store.FindRefreshForUpdate(ctx, tx, HashToken(presented))
		if errors.Is(err, ErrNotFound) {
			outcome = evaluation.RotateInvalid
			return nil
		}
		if err != nil {
			return err
		}
		rows, err := s.store.ListFamilyTokens(ctx, tx, current.FamilyID)
		if err != nil {
			return err
		}
		family := evaluation.FamilyState{ID: current.FamilyID.String(), UserID: current.UserID.String()}
		for _, r := range rows {
			tok := evaluation.TokenState{Hash: fmt.Sprintf("%x", r.TokenHash), ExpiresAt: r.ExpiresAt}
			if r.RevokedAt != nil {
				tok.RevokedAt = *r.RevokedAt
			}
			family.Tokens = append(family.Tokens, tok)
		}
		nextHash := HashToken(newRaw)
		result := evaluation.RotateRefresh(family, fmt.Sprintf("%x", HashToken(presented)),
			evaluation.HashedToken{Hash: fmt.Sprintf("%x", nextHash), ExpiresAt: now.Add(refreshTTL)}, now)
		outcome = result.Outcome

		switch outcome {
		case evaluation.RotateInvalid:
			return nil
		case evaluation.RotateReuseDetected:
			// Commit pencabutan family, lalu kembalikan error ke pemanggil.
			return s.store.RevokeFamily(ctx, tx, current.FamilyID, now, evaluation.RevokeReuseDetected)
		case evaluation.RotateIssued:
			newID, err := ids.NewV7()
			if err != nil {
				return err
			}
			if err := s.store.InsertRefresh(ctx, tx, RefreshToken{
				ID: newID, UserID: current.UserID, FamilyID: current.FamilyID,
				TokenHash: nextHash, ExpiresAt: now.Add(refreshTTL),
			}); err != nil {
				return err
			}
			if err := s.store.MarkRotated(ctx, tx, current.ID, newID, now); err != nil {
				return err
			}
			user, err := s.store.FindUserByID(ctx, current.UserID)
			if err != nil {
				return err
			}
			access, err := s.issuer.Issue(user.ID.String(), user.Role, accessTTL, now)
			if err != nil {
				return err
			}
			pair = TokenPair{AccessToken: access, RefreshToken: newRaw, TokenType: "Bearer", ExpiresIn: int64(accessTTL / time.Second)}
			return nil
		default:
			return fmt.Errorf("auth: outcome rotasi tidak dikenal: %s", outcome)
		}
	})
	if err != nil {
		return TokenPair{}, err
	}
	switch outcome {
	case evaluation.RotateReuseDetected:
		return TokenPair{}, apperr.New(apperr.CodeRefreshTokenReuseDetected)
	case evaluation.RotateInvalid:
		return TokenPair{}, apperr.New(apperr.CodeInvalidRefreshToken)
	}
	return pair, nil
}

// Logout mencabut seluruh Token_Family dari Refresh_Token yang dikirim.
// Token tidak dikenal dianggap sudah logout (idempoten).
func (s *Service) Logout(ctx context.Context, presented string) error {
	now := s.clock.Now()
	return db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
		current, err := s.store.FindRefreshForUpdate(ctx, tx, HashToken(presented))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return s.store.RevokeFamily(ctx, tx, current.FamilyID, now, evaluation.RevokeLogout)
	})
}

// Me mengambil data user.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (UserView, error) {
	u, err := s.store.FindUserByID(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return UserView{}, apperr.New(apperr.CodeNotFound)
	}
	if err != nil {
		return UserView{}, err
	}
	return u.View(), nil
}

// UpdateMe mengubah display_name dan preferred_locale.
func (s *Service) UpdateMe(ctx context.Context, userID uuid.UUID, displayName, locale *string) (UserView, error) {
	var fields []apperr.FieldError
	if displayName != nil && len(*displayName) > 200 {
		fields = append(fields, apperr.FieldError{Field: "display_name", Reason: "max_200"})
	}
	if len(fields) > 0 {
		return UserView{}, apperr.Validation(fields...)
	}
	now := s.clock.Now()
	_ = now
	if _, err := s.store.pool.Exec(ctx, `UPDATE users SET display_name = COALESCE($2, display_name),
		preferred_locale = COALESCE($3, preferred_locale) WHERE id = $1`, userID, displayName, locale); err != nil {
		return UserView{}, err
	}
	return s.Me(ctx, userID)
}

// ChangePassword mengganti password dan mencabut seluruh Refresh_Token user.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	if err := ValidatePassword(newPassword, s.passwordPolicy); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash); err != nil {
			return err
		}
		return s.store.RevokeUserTokens(ctx, tx, userID, s.clock.Now(), evaluation.RevokePasswordChanged)
	})
}

// CreateInvitation membuat undangan. Code asli dikembalikan tepat satu kali.
func (s *Service) CreateInvitation(ctx context.Context, actor uuid.UUID, email *string, role string) (CreatedInvitation, error) {
	if role != "user" && role != "admin" {
		return CreatedInvitation{}, apperr.Validation(apperr.FieldError{Field: "role", Reason: "oneof_user_admin"})
	}
	code, err := NewOpaqueToken()
	if err != nil {
		return CreatedInvitation{}, err
	}
	id, err := ids.NewV7()
	if err != nil {
		return CreatedInvitation{}, err
	}
	now := s.clock.Now()
	inv := Invitation{
		ID: id, CodeHash: HashToken(code), CreatedBy: actor, Email: email,
		Role: role, ExpiresAt: now.Add(s.inviteTTL),
	}
	if err := s.store.InsertInvitation(ctx, inv); err != nil {
		return CreatedInvitation{}, err
	}
	return CreatedInvitation{InvitationView: inv.View(), Code: code}, nil
}

func (s *Service) ListInvitations(ctx context.Context, limit, offset int) ([]InvitationView, int64, error) {
	items, total, err := s.store.ListInvitations(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	views := make([]InvitationView, 0, len(items))
	for _, v := range items {
		views = append(views, v.View())
	}
	return views, total, nil
}

func (s *Service) RevokeInvitation(ctx context.Context, id uuid.UUID) error {
	ok, err := s.store.RevokeInvitation(ctx, id, s.clock.Now())
	if err != nil {
		return err
	}
	if !ok {
		return apperr.New(apperr.CodeNotFound)
	}
	return nil
}

func (s *Service) ListUsers(ctx context.Context, limit, offset int) ([]UserView, int64, error) {
	users, total, err := s.store.ListUsers(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	views := make([]UserView, 0, len(users))
	for _, u := range users {
		views = append(views, u.View())
	}
	return views, total, nil
}

// UpdateUser mengubah is_active dan role. Menonaktifkan user mencabut seluruh
// Refresh_Token miliknya (Requirement 8.7).
func (s *Service) UpdateUser(ctx context.Context, id uuid.UUID, isActive *bool, role *string) (UserView, error) {
	if role != nil && *role != "user" && *role != "admin" {
		return UserView{}, apperr.Validation(apperr.FieldError{Field: "role", Reason: "oneof_user_admin"})
	}
	if err := s.store.UpdateUser(ctx, id, isActive, role); err != nil {
		return UserView{}, err
	}
	if isActive != nil && !*isActive {
		if err := db.WithTx(ctx, s.store.pool, func(tx pgx.Tx) error {
			return s.store.RevokeUserTokens(ctx, tx, id, s.clock.Now(), evaluation.RevokeUserDeactivated)
		}); err != nil {
			return UserView{}, err
		}
	}
	return s.Me(ctx, id)
}

// BootstrapAdmin membuat admin pertama bila belum ada. Dipanggil dari cmd/migrate.
func (s *Service) BootstrapAdmin(ctx context.Context, email, password string) (bool, error) {
	exists, err := s.store.ExistsAdmin(ctx)
	if err != nil || exists {
		return false, err
	}
	if err := ValidatePassword(password, s.passwordPolicy); err != nil {
		return false, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return false, err
	}
	id, err := ids.NewV7()
	if err != nil {
		return false, err
	}
	_, err = s.store.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, display_name, role) VALUES ($1, $2, $3, 'Admin', 'admin')
		 ON CONFLICT (lower(email)) DO NOTHING`, id, NormalizeEmail(email), hash)
	if err != nil {
		return false, err
	}
	return true, nil
}

// issuePair menerbitkan Access_Token dan Refresh_Token dalam transaksi yang sama.
func (s *Service) issuePair(ctx context.Context, tx pgx.Tx, user User) (TokenPair, error) {
	snap, err := s.settings.Snapshot(ctx, "auth.access_token_ttl_seconds", "auth.refresh_token_ttl_seconds")
	if err != nil {
		return TokenPair{}, err
	}
	accessTTL := time.Duration(snap.Int("auth.access_token_ttl_seconds")) * time.Second
	refreshTTL := time.Duration(snap.Int("auth.refresh_token_ttl_seconds")) * time.Second
	now := s.clock.Now()
	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	if refreshTTL <= 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
	access, err := s.issuer.Issue(user.ID.String(), user.Role, accessTTL, now)
	if err != nil {
		return TokenPair{}, err
	}
	rawRefresh, err := NewOpaqueToken()
	if err != nil {
		return TokenPair{}, err
	}
	familyID, err := ids.NewV7()
	if err != nil {
		return TokenPair{}, err
	}
	tokenID, err := ids.NewV7()
	if err != nil {
		return TokenPair{}, err
	}
	err = s.store.InsertRefresh(ctx, tx, RefreshToken{
		ID: tokenID, UserID: user.ID, FamilyID: familyID,
		TokenHash: HashToken(rawRefresh), ExpiresAt: now.Add(refreshTTL),
	})
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken: access, RefreshToken: rawRefresh, TokenType: "Bearer",
		ExpiresIn: int64(accessTTL / time.Second),
	}, nil
}
