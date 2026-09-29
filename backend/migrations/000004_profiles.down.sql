-- Drops the profile columns; usernames and contact phones are lost.
ALTER TABLE users
    DROP COLUMN sign_in_method,
    DROP COLUMN phone_verified,
    DROP COLUMN phone,
    DROP COLUMN username;
