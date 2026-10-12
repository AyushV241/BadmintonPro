DROP INDEX users_username_trgm_idx;
-- Nothing else uses pg_trgm.
DROP EXTENSION IF EXISTS pg_trgm;
DROP TABLE entries;
