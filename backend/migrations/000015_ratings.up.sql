-- Ratings and how matches end (design doc, section 9): finished matches
-- change ratings, retirements are rated from the score so far, walkovers
-- change nothing, and an admin can void a match, which reverses anything it
-- changed.

-- Two more ways a match can end.
--   retired: a player stopped mid-match (injury). The other side wins; the
--            score at the time stands and is rated by the share of points.
--   void:    an admin struck the match off. No rating change survives it.
ALTER TABLE matches DROP CONSTRAINT matches_status_check;
ALTER TABLE matches ADD CONSTRAINT matches_status_check CHECK (status IN
    ('scheduled', 'ready', 'live', 'completed', 'walkover', 'retired', 'void', 'cancelled'));

-- The winner rule was an unnamed table check, so find it by what it says
-- rather than by its generated name.
DO $$
DECLARE c text;
BEGIN
    FOR c IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'matches'::regclass AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%winner_side IS NULL%'
    LOOP
        EXECUTE format('ALTER TABLE matches DROP CONSTRAINT %I', c);
    END LOOP;
END $$;
-- A void match keeps the result it had, for the record.
ALTER TABLE matches ADD CONSTRAINT matches_winner_status_check CHECK
    (winner_side IS NULL OR status IN ('completed', 'walkover', 'retired', 'void'));

-- Which side retired.
ALTER TABLE matches ADD COLUMN retired_side TEXT CHECK (retired_side IN ('A', 'B'));
ALTER TABLE matches ADD CONSTRAINT matches_retired_check CHECK
    (retired_side IS NULL OR status IN ('retired', 'void'));

-- Who voided it, when, why, and what it was before.
ALTER TABLE matches ADD COLUMN voided_at   TIMESTAMPTZ;
ALTER TABLE matches ADD COLUMN voided_by   TEXT REFERENCES users (id);
ALTER TABLE matches ADD COLUMN void_reason TEXT;
ALTER TABLE matches ADD COLUMN voided_from TEXT;
ALTER TABLE matches ADD CONSTRAINT matches_void_check CHECK
    ((status = 'void') = (voided_at IS NOT NULL));

-- Each rating system's state for each player and discipline (design doc,
-- section 9). Every system rates every match; player_ratings keeps the one
-- players see (the default system's, rounded), so everything that reads
-- ratings stays as it is. Exact values, not rounded: Glicko's changes are
-- small and its RD has to shrink smoothly.
CREATE TABLE rating_states (
    user_id        TEXT             NOT NULL REFERENCES users (id),
    discipline     TEXT             NOT NULL CHECK (discipline IN ('singles', 'doubles', 'mixed')),
    -- The system and version, e.g. 'glicko-1' or 'elo-1'.
    system         TEXT             NOT NULL,
    rating         DOUBLE PRECISION NOT NULL,
    -- How unsure the rating is, for a system that models it; NULL for Elo.
    rd             DOUBLE PRECISION CHECK (rd > 0),
    matches_played INTEGER          NOT NULL DEFAULT 0 CHECK (matches_played >= 0),
    -- When the system last rated them; Glicko's RD grows with the time since.
    last_rated_at  TIMESTAMPTZ,
    updated_at     TIMESTAMPTZ      NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, discipline, system)
);

-- The rating history: every change to a rating_states row, in order.
-- Append-only. Each row holds the exact state before and after, so a void
-- can restore it, or replay the matches since from it, and every rating can
-- be traced to the matches that made it.
CREATE TABLE rating_changes (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    match_id        TEXT             NOT NULL REFERENCES matches (id),
    user_id         TEXT             NOT NULL REFERENCES users (id),
    discipline      TEXT             NOT NULL CHECK (discipline IN ('singles', 'doubles', 'mixed')),
    system          TEXT             NOT NULL,
    -- match:  the match was rated when it ended.
    -- void:   the match was voided; this player was in it.
    -- adjust: a void replayed the matches since, and this player's rating
    --         moved though they weren't in the voided match.
    kind            TEXT             NOT NULL CHECK (kind IN ('match', 'void', 'adjust')),
    rating_before   DOUBLE PRECISION NOT NULL,
    rating_after    DOUBLE PRECISION NOT NULL,
    change          DOUBLE PRECISION GENERATED ALWAYS AS (rating_after - rating_before) STORED,
    rd_before       DOUBLE PRECISION,
    rd_after        DOUBLE PRECISION,
    matches_before  INTEGER          NOT NULL,
    matches_after   INTEGER          NOT NULL,
    rated_at_before TIMESTAMPTZ,
    rated_at_after  TIMESTAMPTZ,
    -- The working: expected result, win probability, K or RD terms, margin.
    details         JSONB            NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ      NOT NULL DEFAULT now(),
    -- A match is rated once per system, and voided at most once.
    UNIQUE (match_id, user_id, system, kind)
);
-- A player's history.
CREATE INDEX rating_changes_user_idx ON rating_changes (user_id, discipline, system, id);
-- Replaying a discipline from a point in its history.
CREATE INDEX rating_changes_replay_idx ON rating_changes (discipline, system, id);
