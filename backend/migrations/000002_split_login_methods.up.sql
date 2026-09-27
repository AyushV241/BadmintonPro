-- Separates who a user is (users) from how they sign in (password_credentials,
-- user_identities), so one account can have a password, a Google login, and
-- later Facebook or Apple logins.

ALTER TABLE users ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT false;

-- Some providers (Facebook) may not return an email, and an email a provider
-- does not vouch for is never stored here. The existing unique index on
-- lower(email) still applies; Postgres treats NULLs as distinct.
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;

CREATE TABLE password_credentials (
    user_id       TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    password_hash BYTEA       NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Carry existing passwords over before dropping the column.
INSERT INTO password_credentials (user_id, password_hash)
SELECT id, password_hash FROM users;

ALTER TABLE users DROP COLUMN password_hash;

CREATE TABLE user_identities (
    provider   TEXT        NOT NULL,
    subject    TEXT        NOT NULL,
    user_id    TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The email the provider reported when the identity was linked, kept for
    -- reference. Authoritative email lives on users.
    email      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);

CREATE INDEX user_identities_user_id_idx ON user_identities (user_id);
