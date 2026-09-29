-- Sign-in is by phone code or Google only, and each sign-in method is its own
-- account, so passwords and email-based account matching both go.

DROP TABLE password_credentials;

-- Email is contact information now. Accounts are never looked up by it, and
-- two accounts (say, one per sign-in method) may list the same address.
DROP INDEX users_email_lower_key;
