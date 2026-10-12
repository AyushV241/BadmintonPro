-- Entries: one match for one player in an event (design doc, sections 4.2
-- and 11), and the trigram index behind username search (section 13).
--
-- Only open entries are made for now. Later steps add the rest:
-- challenge_id for locked entries (step 7), match_id once scheduled (step 6),
-- and the payments and event_checkins tables (steps 5 and 10).

CREATE TABLE entries (
    id       TEXT PRIMARY KEY,
    -- Events and users are never deleted, so no ON DELETE here.
    event_id TEXT NOT NULL REFERENCES events (id),
    user_id  TEXT NOT NULL REFERENCES users (id),
    -- open: the matcher picks the opponent. locked: a challenge fixed it.
    kind     TEXT NOT NULL DEFAULT 'open' CHECK (kind IN ('open', 'locked')),
    -- The formats the player will play; the entry gets one match in one of them.
    formats  TEXT[] NOT NULL
        CHECK (cardinality(formats) > 0 AND formats <@ ARRAY['singles', 'doubles', 'mixed']),
    -- An optional doubles or mixed partner chosen when joining.
    partner_user_id TEXT REFERENCES users (id),
    source   TEXT NOT NULL DEFAULT 'online' CHECK (source IN ('online', 'walk_in')),
    -- Entries are never deleted, only moved to cancelled, refunded or no_show.
    status   TEXT NOT NULL DEFAULT 'pending_payment'
        CHECK (status IN ('pending_payment', 'confirmed', 'standby', 'scheduled',
                          'cancelled', 'refunded', 'no_show')),
    -- How long a pending_payment entry keeps its place. An expired hold just
    -- stops counting towards the event's limits; nothing sweeps it.
    hold_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (status <> 'pending_payment' OR hold_expires_at IS NOT NULL),
    CHECK (partner_user_id IS NULL OR partner_user_id <> user_id),
    CHECK (partner_user_id IS NULL OR formats && ARRAY['doubles', 'mixed'])
    -- Deliberately not UNIQUE (event_id, user_id): a player may hold several
    -- entries in one event, one per match.
);
-- Counting an event's live entries (the capacity check, spots left).
CREATE INDEX entries_event_status_idx ON entries (event_id, status);
-- A player's own entries, newest first.
CREATE INDEX entries_user_idx ON entries (user_id, created_at DESC);

-- Username search: the trigram index serves both the "contains" tier
-- (LIKE '%q%') and the fuzzy tier (similarity). Exact and prefix matches can
-- use it too; users_username_lower_key also covers exact ones.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX users_username_trgm_idx ON users USING gin (lower(username) gin_trgm_ops);
