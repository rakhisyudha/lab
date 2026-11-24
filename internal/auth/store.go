package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store mengakses tabel users, invitations, dan refresh_tokens.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (User, error) {
	var u User
	var preferred sql.NullString
	var lastLogin sql.NullTime
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Role,
		&preferred, &u.IsActive, &lastLogin)
	if err != nil {
		return User{}, err
	}
	if preferred.Valid {
		u.PreferredLocale = &preferred.String
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return u, nil
}

const userColumns = `id, email, password_hash, display_name, role, preferred_locale, is_active, last_login_at`

// ErrNotFound menandakan baris tidak ditemukan.
var ErrNotFound = errors.New("auth: baris tidak ditemukan")

// FindUserByEmail mencari user dengan perbandingan email case-insensitive.
func (s *Store) FindUserByEmail(ctx context.Context, email string) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`, email))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) FindUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) InsertUser(ctx context.Context, tx pgx.Tx, email, hash, displayName, role string) (User, error) {
	q := `INSERT INTO users (email, password_hash, display_name, role) VALUES ($1, $2, $3, $4)
	      RETURNING ` + userColumns
	row := tx.QueryRow(ctx, q, email, hash, displayName, role)
	u, err := scanUser(row)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, id, hash)
	return err
}

func (s *Store) TouchLogin(ctx context.Context, id uuid.UUID, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at = $2 WHERE id = $1`, id, now)
	return err
}

func (s *Store) UpdateUser(ctx context.Context, id uuid.UUID, isActive *bool, role *string) error {
	if isActive != nil {
		if _, err := s.pool.Exec(ctx, `UPDATE users SET is_active = $2 WHERE id = $1`, id, *isActive); err != nil {
			return err
		}
	}
	if role != nil {
		if _, err := s.pool.Exec(ctx, `UPDATE users SET role = $2 WHERE id = $1`, id, *role); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]User, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := make([]User, 0, limit)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

func (s *Store) ExistsAdmin(ctx context.Context) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin')`).Scan(&exists)
	return exists, err
}

func scanInvitation(row scanner) (Invitation, error) {
	var v Invitation
	var email sql.NullString
	var usedBy sql.NullString
	var usedAt, revokedAt sql.NullTime
	err := row.Scan(&v.ID, &v.CodeHash, &v.CreatedBy, &email, &v.Role, &v.ExpiresAt,
		&usedAt, &usedBy, &revokedAt)
	if err != nil {
		return Invitation{}, err
	}
	if email.Valid {
		v.Email = &email.String
	}
	if usedAt.Valid {
		v.UsedAt = &usedAt.Time
	}
	if usedBy.Valid {
		id, err := uuid.Parse(usedBy.String)
		if err != nil {
			return Invitation{}, fmt.Errorf("auth: used_by: %w", err)
		}
		v.UsedBy = &id
	}
	if revokedAt.Valid {
		v.RevokedAt = &revokedAt.Time
	}
	return v, nil
}

const invitationColumns = `id, code_hash, created_by, email, role, expires_at, used_at, used_by, revoked_at`

func (s *Store) FindInvitationForUpdate(ctx context.Context, tx pgx.Tx, codeHash []byte) (Invitation, error) {
	v, err := scanInvitation(tx.QueryRow(ctx,
		`SELECT `+invitationColumns+` FROM invitations WHERE code_hash = $1 FOR UPDATE`, codeHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrNotFound
	}
	return v, err
}

func (s *Store) InsertInvitation(ctx context.Context, v Invitation) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO invitations
		(id, code_hash, created_by, email, role, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		v.ID, v.CodeHash, v.CreatedBy, v.Email, v.Role, v.ExpiresAt)
	return err
}

func (s *Store) MarkInvitationUsed(ctx context.Context, tx pgx.Tx, id, userID uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE invitations SET used_at = $2, used_by = $3 WHERE id = $1`, id, now, userID)
	return err
}

func (s *Store) ListInvitations(ctx context.Context, limit, offset int) ([]Invitation, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM invitations`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+invitationColumns+` FROM invitations ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Invitation, 0, limit)
	for rows.Next() {
		v, err := scanInvitation(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, v)
	}
	return items, total, rows.Err()
}

func (s *Store) RevokeInvitation(ctx context.Context, id uuid.UUID, now time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE invitations SET revoked_at = $2 WHERE id = $1 AND used_at IS NULL AND revoked_at IS NULL`, id, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func scanRefresh(row scanner) (RefreshToken, error) {
	var t RefreshToken
	var revokedAt sql.NullTime
	var reason, replacedBy sql.NullString
	err := row.Scan(&t.ID, &t.UserID, &t.FamilyID, &t.TokenHash, &t.ExpiresAt,
		&revokedAt, &reason, &replacedBy)
	if err != nil {
		return RefreshToken{}, err
	}
	if revokedAt.Valid {
		t.RevokedAt = &revokedAt.Time
	}
	if reason.Valid {
		t.RevokedReason = &reason.String
	}
	if replacedBy.Valid {
		id, err := uuid.Parse(replacedBy.String)
		if err != nil {
			return RefreshToken{}, fmt.Errorf("auth: replaced_by: %w", err)
		}
		t.ReplacedBy = &id
	}
	return t, nil
}

const refreshColumns = `id, user_id, family_id, token_hash, expires_at, revoked_at, revoked_reason, replaced_by`

func (s *Store) FindRefreshForUpdate(ctx context.Context, tx pgx.Tx, hash []byte) (RefreshToken, error) {
	t, err := scanRefresh(tx.QueryRow(ctx,
		`SELECT `+refreshColumns+` FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return RefreshToken{}, ErrNotFound
	}
	return t, err
}

func (s *Store) ListFamilyTokens(ctx context.Context, tx pgx.Tx, familyID uuid.UUID) ([]RefreshToken, error) {
	rows, err := tx.Query(ctx, `SELECT `+refreshColumns+` FROM refresh_tokens WHERE family_id = $1`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RefreshToken, 0, 2)
	for rows.Next() {
		t, err := scanRefresh(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

func (s *Store) InsertRefresh(ctx context.Context, tx pgx.Tx, t RefreshToken) error {
	_, err := tx.Exec(ctx, `INSERT INTO refresh_tokens
		(id, user_id, family_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		t.ID, t.UserID, t.FamilyID, t.TokenHash, t.ExpiresAt)
	return err
}

func (s *Store) MarkRotated(ctx context.Context, tx pgx.Tx, oldID, newID uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE refresh_tokens
		SET revoked_at = $2, revoked_reason = 'rotated', replaced_by = $3 WHERE id = $1`,
		oldID, now, newID)
	return err
}

func (s *Store) RevokeFamily(ctx context.Context, tx pgx.Tx, familyID uuid.UUID, now time.Time, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE refresh_tokens
		SET revoked_at = $2, revoked_reason = $3
		WHERE family_id = $1 AND revoked_at IS NULL`, familyID, now, reason)
	return err
}

func (s *Store) RevokeUserTokens(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE refresh_tokens
		SET revoked_at = $2, revoked_reason = $3
		WHERE user_id = $1 AND revoked_at IS NULL`, userID, now, reason)
	return err
}
