DROP INDEX entries_one_per_challenge_player;
ALTER TABLE entries DROP CONSTRAINT entries_locked_has_challenge;
ALTER TABLE entries DROP COLUMN challenge_id;
DROP TABLE challenges;
