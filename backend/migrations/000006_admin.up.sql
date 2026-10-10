-- Admin mode: who created an event, and which users judge it.

-- NULL for events made by -seed-demo or before this migration.
ALTER TABLE events ADD COLUMN created_by TEXT REFERENCES users (id);

-- The people who may score an event's matches. The matcher assigns each
-- match a judge from this list.
CREATE TABLE event_judges (
    event_id TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users (id),
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, user_id)
);
