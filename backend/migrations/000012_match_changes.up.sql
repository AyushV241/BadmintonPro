-- Fixture editing (design doc, section 10, "Editing fixtures by hand"): an
-- audit row for every change an admin makes to a match by hand, and where
-- each match came from.

-- How a match was made: by the matcher (a publish), by an admin by hand, by
-- a challenge both players paid for, or (reserved, nothing sets them yet)
-- by matching on arrival and at the desk on the spot (section 6).
ALTER TABLE matches ADD COLUMN source TEXT NOT NULL DEFAULT 'matcher'
    CHECK (source IN ('matcher', 'admin', 'challenge', 'arrival', 'desk'));
-- Matches before this migration came from the matcher or a challenge.
UPDATE matches SET source = 'challenge'
WHERE id IN (SELECT match_id FROM challenges WHERE match_id IS NOT NULL);

CREATE TABLE match_changes (
    id         TEXT PRIMARY KEY,
    match_id   TEXT NOT NULL REFERENCES matches (id),
    -- Matches edited by hand always belong to an event; kept here so an
    -- event's changes list without a join.
    event_id   TEXT NOT NULL REFERENCES events (id),
    admin_id   TEXT NOT NULL REFERENCES users (id),
    action     TEXT NOT NULL CHECK (action IN ('create', 'replace', 'move', 'cancel')),
    -- What changed, before and after: court and times, entries in and out
    -- with the outcome, the judge.
    details    JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- An event's changes, newest first.
CREATE INDEX match_changes_event_idx ON match_changes (event_id, created_at);
