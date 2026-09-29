-- Profile details collected on the "Set up your profile" screen after the
-- first sign-in.

-- Unique regardless of case. NULL until the profile is set up; Postgres
-- treats NULLs as distinct, so any number of accounts can be unset.
ALTER TABLE users ADD COLUMN username TEXT;
CREATE UNIQUE INDEX users_username_lower_key ON users (lower(username));

-- Contact phone. A phone account's is its sign-in number; a Google account
-- may add one, verified by a code. Not unique: each sign-in method is its own
-- account, so two accounts may list the same number.
ALTER TABLE users ADD COLUMN phone TEXT;
ALTER TABLE users ADD COLUMN phone_verified BOOLEAN NOT NULL DEFAULT false;

-- How the account signs in: 'phone', 'google', ...
ALTER TABLE users ADD COLUMN sign_in_method TEXT;

UPDATE users u
SET sign_in_method = (SELECT min(provider) FROM user_identities i WHERE i.user_id = u.id);

UPDATE users u
SET phone = i.subject, phone_verified = true
FROM user_identities i
WHERE i.user_id = u.id AND i.provider = 'phone';
