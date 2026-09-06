-- Accounts, sessions and API tokens, per docs/03_data_model.md.
--
-- app_user comes first because every other table's audit columns reference it.

CREATE TABLE app_user (
    id             uuid        PRIMARY KEY,
    name           text        NOT NULL,
    display_name   text,
    password_hash  text        NOT NULL,
    email          text,
    is_admin       boolean     NOT NULL DEFAULT false,
    last_login_at  timestamptz,

    created_at     timestamptz NOT NULL DEFAULT now(),
    created_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT app_user_name_length
        CHECK (char_length(name) BETWEEN 3 AND 64),
    CONSTRAINT app_user_display_name_length
        CHECK (display_name IS NULL OR char_length(display_name) BETWEEN 1 AND 64),
    -- Deliberately loose. The address is optional, never verified and never
    -- used to send anything, so anything beyond "looks like an address" would
    -- reject valid addresses for no benefit.
    CONSTRAINT app_user_email_shape
        CHECK (email IS NULL OR (position('@' IN email) > 1 AND char_length(email) <= 254))
);

COMMENT ON TABLE app_user IS 'Registered users. Registration is open to anyone who can reach the application.';
COMMENT ON COLUMN app_user.name IS 'Login name. Unique case-insensitively.';
COMMENT ON COLUMN app_user.display_name IS 'Shown in the UI. Falls back to name when null.';
COMMENT ON COLUMN app_user.password_hash IS 'Argon2id PHC string.';
COMMENT ON COLUMN app_user.is_admin IS 'True only for the built-in root administrator.';

-- Case-insensitive uniqueness: "Anna" and "anna" are the same person trying to
-- register twice, not two people.
CREATE UNIQUE INDEX app_user_name_key ON app_user (lower(name));

CREATE TRIGGER app_user_set_updated_at
    BEFORE UPDATE ON app_user
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The deleted-user placeholder owns order items whose user has been deleted, so
-- that an expired order still reads "1x Döner, no onions" without naming
-- anyone. Its id is fixed and referenced from application code.
--
-- The password hash is not a valid PHC string, so no password can ever verify
-- against it and the account cannot be logged into.
INSERT INTO app_user (id, name, display_name, password_hash, is_admin)
VALUES (
    '00000000-0000-7000-8000-000000000000',
    'deleted',
    'deleted user',
    '*',
    false
);

-- Two rows must survive any deletion: the placeholder, because order history
-- points at it, and the administrator, because docs/05_auth_and_permissions.md
-- says root cannot be deleted. Enforcing it here means no code path can get it
-- wrong, including a manual psql session at three in the morning.
CREATE OR REPLACE FUNCTION prevent_protected_user_delete() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.id = '00000000-0000-7000-8000-000000000000'::uuid THEN
        RAISE EXCEPTION 'the deleted-user placeholder cannot be deleted'
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.is_admin THEN
        RAISE EXCEPTION 'the administrator account cannot be deleted'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN OLD;
END;
$$;

CREATE TRIGGER app_user_protect_delete
    BEFORE DELETE ON app_user
    FOR EACH ROW EXECUTE FUNCTION prevent_protected_user_delete();


-- Server-side companion to the JWT. A stateless token can express neither an
-- idle timeout nor single-session enforcement; see
-- docs/adr/0004-jwt-with-server-side-sessions.md.
CREATE TABLE session (
    id                uuid        PRIMARY KEY,
    user_id           uuid        NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    issued_at         timestamptz NOT NULL DEFAULT now(),
    last_seen_at      timestamptz NOT NULL DEFAULT now(),
    absolute_expires  timestamptz NOT NULL,
    remote_addr       text,
    user_agent        text,

    created_at        timestamptz NOT NULL DEFAULT now(),
    created_by        uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    updated_by        uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT session_expires_after_issue
        CHECK (absolute_expires > issued_at),

    -- One active session per user. A new login replaces the row, and the
    -- previously logged-in browser is rejected on its next request. This is
    -- also what makes the absence of optimistic locking safe.
    CONSTRAINT session_one_per_user UNIQUE (user_id)
);

COMMENT ON TABLE session IS 'Active browser sessions. At most one per user.';
COMMENT ON COLUMN session.id IS 'Carried in the JWT jti claim.';

CREATE INDEX session_absolute_expires_idx ON session (absolute_expires);

CREATE TRIGGER session_set_updated_at
    BEFORE UPDATE ON session
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE api_token (
    id            uuid        PRIMARY KEY,
    user_id       uuid        NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    name          text        NOT NULL,
    token_hash    text        NOT NULL,
    token_prefix  text        NOT NULL,
    expires_at    timestamptz,
    last_used_at  timestamptz,

    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT api_token_name_length
        CHECK (char_length(name) BETWEEN 1 AND 64),
    CONSTRAINT api_token_name_unique_per_user UNIQUE (user_id, name),
    CONSTRAINT api_token_hash_unique UNIQUE (token_hash)
);

COMMENT ON TABLE api_token IS 'Long-lived tokens for third-party and scripted access.';
COMMENT ON COLUMN api_token.token_hash IS 'SHA-256 of the token. The token itself is never stored.';
COMMENT ON COLUMN api_token.token_prefix IS 'First 8 characters, so a token is identifiable in the UI.';

CREATE INDEX api_token_user_idx ON api_token (user_id);

CREATE TRIGGER api_token_set_updated_at
    BEFORE UPDATE ON api_token
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
