-- WARNING: lossy. Dropped password hashes can't be restored, so the table
-- comes back empty and password logins stay impossible. Recreating the
-- unique email index fails if two accounts now share an email.

CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

CREATE TABLE password_credentials (
    user_id       TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    password_hash BYTEA       NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
