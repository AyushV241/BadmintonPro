ALTER TABLE entries DROP CONSTRAINT entries_scheduled_has_match;
ALTER TABLE entries DROP COLUMN confirmed_at;
ALTER TABLE entries DROP COLUMN confirmed_by;
ALTER TABLE entries DROP COLUMN standby_reason;
ALTER TABLE entries DROP COLUMN match_id;
DROP TABLE match_sides;
ALTER TABLE entries DROP CONSTRAINT entries_id_user_key;
DROP TABLE matches;
-- Nothing else uses btree_gist.
DROP EXTENSION IF EXISTS btree_gist;
ALTER TABLE events DROP COLUMN schedule_published_by;
ALTER TABLE events DROP COLUMN schedule_published_at;
DROP TABLE player_ratings;
