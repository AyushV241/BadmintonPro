-- WARNING: lossy. The previous schema requires every user to have an email and
-- a password, so accounts that sign in only through an external provider are
-- deleted.

ALTER TABLE users ADD COLUMN password_hash BYTEA;

UPDATE users u
SET password_hash = pc.password_hash
FROM password_credentials pc
WHERE pc.user_id = u.id;

DELETE FROM users WHERE password_hash IS NULL OR email IS NULL;

ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;
ALTER TABLE users ALTER COLUMN email SET NOT NULL;

DROP TABLE user_identities;
DROP TABLE password_credentials;

ALTER TABLE users DROP COLUMN email_verified;
