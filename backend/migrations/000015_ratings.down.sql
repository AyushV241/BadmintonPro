-- WARNING: lossy. The rating history and every system's state go, and
-- retired and void matches don't fit the earlier statuses: retired ones
-- become completed, void ones cancelled (losing their result). The ratings
-- players see (player_ratings) are left as they are.
DROP TABLE rating_changes;
DROP TABLE rating_states;

UPDATE matches SET status = 'completed' WHERE status = 'retired';
UPDATE matches SET status = 'cancelled', winner_side = NULL WHERE status = 'void';

ALTER TABLE matches DROP CONSTRAINT matches_void_check;
ALTER TABLE matches DROP COLUMN voided_from;
ALTER TABLE matches DROP COLUMN void_reason;
ALTER TABLE matches DROP COLUMN voided_by;
ALTER TABLE matches DROP COLUMN voided_at;
ALTER TABLE matches DROP CONSTRAINT matches_retired_check;
ALTER TABLE matches DROP COLUMN retired_side;

ALTER TABLE matches DROP CONSTRAINT matches_winner_status_check;
ALTER TABLE matches ADD CHECK (winner_side IS NULL OR status IN ('completed', 'walkover'));
ALTER TABLE matches DROP CONSTRAINT matches_status_check;
ALTER TABLE matches ADD CONSTRAINT matches_status_check CHECK (status IN
    ('scheduled', 'ready', 'live', 'completed', 'walkover', 'cancelled'));
