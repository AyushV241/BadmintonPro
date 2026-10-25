-- Player search matches names as well as usernames (design doc, section 13).
-- The same kind of trigram index as users_username_trgm_idx (000007), so
-- "contains" and fuzzy matches on names don't scan every user.
CREATE INDEX users_name_trgm_idx ON users USING gin (lower(name) gin_trgm_ops);
