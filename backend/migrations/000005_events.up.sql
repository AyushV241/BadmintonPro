-- Venues, their courts, and the events an admin runs on pre-booked courts.

CREATE TABLE venues (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    address    TEXT NOT NULL DEFAULT '',
    -- Lowercase slug ("bangalore"): what "events near me" filters on for now.
    city       TEXT NOT NULL CHECK (city ~ '^[a-z0-9-]+$'),
    -- Optional until "nearest to me" needs them; both or neither.
    lat        DOUBLE PRECISION CHECK (lat BETWEEN -90 AND 90),
    lng        DOUBLE PRECISION CHECK (lng BETWEEN -180 AND 180),
    -- IANA zone ("Asia/Kolkata"). Event times are shown in the venue's zone.
    timezone   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL) = (lng IS NULL))
);
CREATE INDEX venues_city_idx ON venues (city);

CREATE TABLE courts (
    id       TEXT PRIMARY KEY,
    venue_id TEXT NOT NULL REFERENCES venues (id),
    label    TEXT NOT NULL,
    active   BOOLEAN NOT NULL DEFAULT true,
    UNIQUE (venue_id, label),
    -- Lets event_courts check that a court belongs to the event's venue.
    UNIQUE (id, venue_id)
);

CREATE TABLE events (
    id         TEXT PRIMARY KEY,
    venue_id   TEXT NOT NULL REFERENCES venues (id),
    title      TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    format     TEXT NOT NULL CHECK (format IN ('singles', 'doubles')),
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL,
    -- Money in the currency's minor unit (paise), never a float.
    fee_paise  INTEGER NOT NULL CHECK (fee_paise >= 0),
    currency   TEXT NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
    -- Players the event takes.
    capacity   INTEGER NOT NULL CHECK (capacity > 0),
    -- Optional rating band; the rating scale isn't final, so nothing enforces it yet.
    rating_min NUMERIC(3, 1),
    rating_max NUMERIC(3, 1),
    -- Events are never deleted, only cancelled: registrations and payments
    -- will refer to them.
    status     TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'published', 'cancelled', 'completed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at),
    CHECK (rating_min IS NULL OR rating_max IS NULL OR rating_min <= rating_max),
    UNIQUE (id, venue_id)
);
-- The public list: published events in start order, paged on (starts_at, id).
CREATE INDEX events_published_idx ON events (starts_at, id) WHERE status = 'published';

-- The pre-booked courts an event uses. Both composite keys make sure the
-- court is at the event's venue.
CREATE TABLE event_courts (
    event_id TEXT NOT NULL,
    court_id TEXT NOT NULL,
    venue_id TEXT NOT NULL,
    PRIMARY KEY (event_id, court_id),
    FOREIGN KEY (event_id, venue_id) REFERENCES events (id, venue_id) ON DELETE CASCADE,
    FOREIGN KEY (court_id, venue_id) REFERENCES courts (id, venue_id)
);
