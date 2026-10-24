-- Challenges: one player asks a named opponent to play at an event, and the
-- two locked entries that come of it (design doc, sections 11 and 12).
--
-- Singles only in version 1. From sending, the opponent must accept and both
-- must pay before expires_at (4 hours, or the event's registration cutoff if
-- sooner); then the match is fixed at once.

CREATE TABLE challenges (
    id            TEXT PRIMARY KEY,
    -- Events, users and matches are never deleted (rule 5), so no ON DELETE.
    event_id      TEXT NOT NULL REFERENCES events (id),
    format        TEXT NOT NULL DEFAULT 'singles' CHECK (format = 'singles'),
    challenger_id TEXT NOT NULL REFERENCES users (id),
    opponent_id   TEXT NOT NULL REFERENCES users (id),
    -- The start the challenger asked for; the match takes the free slot
    -- nearest to it.
    preferred_start TIMESTAMPTZ NOT NULL,
    -- pending → accepted → scheduled, or it ends declined, withdrawn,
    -- expired (not both paid by expires_at) or failed (no slot left).
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'scheduled', 'declined', 'withdrawn', 'expired', 'failed')),
    -- The one deadline: accepted and both paid by then, or it expires.
    expires_at   TIMESTAMPTZ NOT NULL,
    -- When the opponent accepted or declined.
    responded_at TIMESTAMPTZ,
    -- The fixed match, once both have paid.
    match_id     TEXT UNIQUE REFERENCES matches (id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (challenger_id <> opponent_id),
    CHECK (status <> 'scheduled' OR match_id IS NOT NULL),
    CHECK (status NOT IN ('accepted', 'declined', 'scheduled', 'failed') OR responded_at IS NOT NULL)
);
-- At most one open challenge between the same two players for one event,
-- whoever sent it.
CREATE UNIQUE INDEX challenges_one_open_per_pair ON challenges
    (event_id, least(challenger_id, opponent_id), greatest(challenger_id, opponent_id))
    WHERE status IN ('pending', 'accepted');
-- A player's challenges, sent and received, newest first.
CREATE INDEX challenges_challenger_idx ON challenges (challenger_id, created_at DESC);
CREATE INDEX challenges_opponent_idx ON challenges (opponent_id, created_at DESC);
-- The expiry sweep.
CREATE INDEX challenges_due_idx ON challenges (expires_at) WHERE status IN ('pending', 'accepted');

-- A locked entry belongs to its challenge; an open one to none. Every entry
-- so far is open, so the check holds for existing rows.
ALTER TABLE entries ADD COLUMN challenge_id TEXT REFERENCES challenges (id);
ALTER TABLE entries ADD CONSTRAINT entries_locked_has_challenge
    CHECK ((kind = 'locked') = (challenge_id IS NOT NULL));
-- One locked entry per player per challenge.
CREATE UNIQUE INDEX entries_one_per_challenge_player ON entries (challenge_id, user_id)
    WHERE challenge_id IS NOT NULL;
