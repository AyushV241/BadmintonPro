-- Judges (design doc, section 10, "Judge accounts"): an admin registers a
-- person by phone number and name, and they sign in with "Log in as judge".
--
-- A judge is an ordinary phone account plus a row here, so the same person
-- can also play. The role is held by the account (user_id), not the number:
-- the number only finds the account when the admin registers it.

CREATE TABLE judges (
    user_id    TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- As the admin entered them. The account's own name may differ once the
    -- person sets up a player profile.
    name       TEXT        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    phone      TEXT        NOT NULL,
    created_by TEXT REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Which home page a session lands on. It only chooses the view: every judge
-- endpoint checks the judges table on each request, so removing a judge takes
-- effect at once whatever their session says.
ALTER TABLE sessions ADD COLUMN mode TEXT NOT NULL DEFAULT 'player'
    CHECK (mode IN ('player', 'judge'));
