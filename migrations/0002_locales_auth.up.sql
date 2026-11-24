CREATE TABLE supported_locales (
  code         text PRIMARY KEY CHECK (code ~ '^[a-z]{2,3}$'),
  display_name text NOT NULL,
  is_default   boolean NOT NULL DEFAULT false,
  is_enabled   boolean NOT NULL DEFAULT true,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
-- paling banyak satu default
CREATE UNIQUE INDEX supported_locales_one_default_uk ON supported_locales (is_default) WHERE is_default;
SELECT attach_updated_at_trigger('supported_locales');

CREATE TABLE users (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email            text NOT NULL CHECK (char_length(email) BETWEEN 3 AND 254),
  password_hash    text NOT NULL,                       -- argon2id encoded (PHC string)
  display_name     text,
  role             text NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
  preferred_locale text REFERENCES supported_locales(code) ON DELETE RESTRICT,
  is_active        boolean NOT NULL DEFAULT true,
  last_login_at    timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_lower_uk ON users (lower(email));
SELECT attach_updated_at_trigger('users');

CREATE TABLE invitations (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code_hash   bytea NOT NULL UNIQUE,                    -- SHA-256 dari Invite_Code (256-bit random)
  created_by  uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  email       text,                                     -- null = bebas, terisi = terkunci ke email
  role        text NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
  expires_at  timestamptz NOT NULL,
  used_at     timestamptz,
  used_by     uuid REFERENCES users(id) ON DELETE RESTRICT,
  revoked_at  timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT invitations_used_pair CHECK ((used_at IS NULL) = (used_by IS NULL))
);
CREATE INDEX invitations_created_by_idx ON invitations (created_by, created_at DESC);
CREATE INDEX invitations_open_idx ON invitations (expires_at) WHERE used_at IS NULL AND revoked_at IS NULL;
SELECT attach_updated_at_trigger('invitations');

-- Rotasi (Requirement 8.3, 8.4, 8.11): baris token dikunci FOR UPDATE saat refresh. Token dengan revoked_at terisi
-- (termasuk 'rotated') yang dipakai lagi selalu dianggap reuse, tanpa grace window berapa pun selisih waktunya.
-- Reuse me-revoke seluruh baris dengan family_id yang sama (revoked_reason = 'reuse_detected').
CREATE TABLE refresh_tokens (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  family_id      uuid NOT NULL,
  token_hash     bytea NOT NULL UNIQUE,                 -- SHA-256 dari token opaque 256-bit
  expires_at     timestamptz NOT NULL,
  revoked_at     timestamptz,
  revoked_reason text CHECK (revoked_reason IN
                   ('rotated','logout','reuse_detected','user_deactivated','password_changed')),
  replaced_by    uuid REFERENCES refresh_tokens(id) ON DELETE SET NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT refresh_revoked_pair CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL))
);
SELECT attach_updated_at_trigger('refresh_tokens');
CREATE INDEX refresh_tokens_user_idx   ON refresh_tokens (user_id) WHERE revoked_at IS NULL;
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens (family_id);
CREATE INDEX refresh_tokens_expiry_idx ON refresh_tokens (expires_at);
