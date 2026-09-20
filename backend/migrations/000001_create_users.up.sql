CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    name          TEXT        NOT NULL,
    email         TEXT        NOT NULL,
    password_hash BYTEA       NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Logins normalise the address before looking it up, so uniqueness has to be
-- enforced on the normalised form too.
CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
