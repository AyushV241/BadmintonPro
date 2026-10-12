-- Player ratings, matches and their sides, and what the matcher and the
-- walk-in desk record on entries (design doc, sections 6, 7, 9 and 11).
--
-- Ratings don't change yet: rating_changes and the Elo update come with live
-- scoring and results (build steps 8 and 9).

-- One Elo rating per player per discipline (section 9). Rows are created
-- lazily: a missing row means the starting rating, 1200.
CREATE TABLE player_ratings (
    user_id        TEXT NOT NULL REFERENCES users (id),
    discipline     TEXT NOT NULL CHECK (discipline IN ('singles', 'doubles', 'mixed')),
    rating         SMALLINT NOT NULL DEFAULT 1200 CHECK (rating > 0),
    matches_played INTEGER NOT NULL DEFAULT 0 CHECK (matches_played >= 0),
    -- True for the first 10 rated matches, while the K-factor is higher.
    provisional    BOOLEAN NOT NULL DEFAULT true,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, discipline)
);

-- The schedule is published once per event in version 1. Later runs that
-- only fill gaps (walk-ins, no-shows) are future work.
ALTER TABLE events ADD COLUMN schedule_published_at TIMESTAMPTZ;
ALTER TABLE events ADD COLUMN schedule_published_by TEXT REFERENCES users (id);

-- The exclusion constraint below needs equality on text in a GiST index.
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- A match is two sides playing in one slot. It never assumes an event
-- (rule 1): event_id is NULL for future matches arranged without one.
CREATE TABLE matches (
    id         TEXT PRIMARY KEY,
    event_id   TEXT REFERENCES events (id),
    format     TEXT NOT NULL CHECK (format IN ('singles', 'doubles', 'mixed')),
    court_id   TEXT NOT NULL REFERENCES courts (id),
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL,
    -- NULL when no judge was free; the admin is asked to assign one.
    judge_id   TEXT REFERENCES users (id),
    -- Matches are never deleted, only cancelled (rule 5).
    status     TEXT NOT NULL DEFAULT 'scheduled'
        CHECK (status IN ('scheduled', 'ready', 'live', 'completed', 'walkover', 'cancelled')),
    winner_side TEXT CHECK (winner_side IN ('A', 'B')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at),
    CHECK (winner_side IS NULL OR status IN ('completed', 'walkover')),
    -- An event's match uses one of the event's courts. Without an event
    -- (event_id NULL) the key isn't checked.
    FOREIGN KEY (event_id, court_id) REFERENCES event_courts (event_id, court_id),
    -- A court never holds two live matches at overlapping times. Ranges are
    -- half-open, so a match may start as the previous one ends.
    EXCLUDE USING gist (court_id WITH =, tstzrange(starts_at, ends_at) WITH &&)
        WHERE (status <> 'cancelled')
);
CREATE INDEX matches_event_idx ON matches (event_id, starts_at);
CREATE INDEX matches_judge_idx ON matches (judge_id, starts_at) WHERE judge_id IS NOT NULL;

-- Lets match_sides check that an entry belongs to the player on that side.
ALTER TABLE entries ADD CONSTRAINT entries_id_user_key UNIQUE (id, user_id);

-- Who plays on each side: one row per player, one (singles) or two
-- (doubles, mixed) per side.
CREATE TABLE match_sides (
    match_id TEXT NOT NULL REFERENCES matches (id),
    side     TEXT NOT NULL CHECK (side IN ('A', 'B')),
    user_id  TEXT NOT NULL REFERENCES users (id),
    -- The entry that paid for this player's place. Unique: an entry is used
    -- for at most one match (rule 9). NULL is for future matches without an
    -- event.
    entry_id TEXT UNIQUE,
    PRIMARY KEY (match_id, user_id),
    FOREIGN KEY (entry_id, user_id) REFERENCES entries (id, user_id)
);
-- A player's matches (GET /api/me/matches).
CREATE INDEX match_sides_user_idx ON match_sides (user_id);

-- The match an entry was scheduled into, or why it is on standby.
ALTER TABLE entries ADD COLUMN match_id TEXT REFERENCES matches (id);
ALTER TABLE entries ADD COLUMN standby_reason TEXT;
-- Who confirmed an entry by hand, and when: the walk-in desk's offline
-- payment (section 7). The payment webhook (step 5) will set confirmed_at
-- only. NULL for entries confirmed before this migration or by -seed-demo.
ALTER TABLE entries ADD COLUMN confirmed_by TEXT REFERENCES users (id);
ALTER TABLE entries ADD COLUMN confirmed_at TIMESTAMPTZ;
ALTER TABLE entries ADD CONSTRAINT entries_scheduled_has_match
    CHECK (status <> 'scheduled' OR match_id IS NOT NULL);
