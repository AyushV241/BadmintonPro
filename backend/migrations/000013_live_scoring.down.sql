-- WARNING: lossy. Every point scored is deleted.
DROP TABLE match_points;
ALTER TABLE matches DROP COLUMN completed_at;
ALTER TABLE matches DROP COLUMN started_at;
ALTER TABLE matches DROP COLUMN first_server;
