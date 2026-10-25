-- Live scoring (design doc, section 8): the judge starts the match and scores
-- it point by point; players watch the score update in real time.

-- Who serves the first rally, chosen by the judge at the start (the toss).
-- Later serves follow from the points: the rally winner serves next.
ALTER TABLE matches ADD COLUMN first_server TEXT CHECK (first_server IN ('A', 'B'));
ALTER TABLE matches ADD COLUMN started_at   TIMESTAMPTZ;
ALTER TABLE matches ADD COLUMN completed_at TIMESTAMPTZ;

-- The point log. Append-only: the score is always computed from these rows,
-- never stored as a counter. An undo is a row of its own, so nothing is ever
-- deleted and the log replays to the same score.
CREATE TABLE match_points (
    match_id   TEXT        NOT NULL REFERENCES matches (id),
    -- 1, 2, 3 … with no gaps: the judge's app sends points in order, and a
    -- point that arrives out of turn is refused so the order is never wrong.
    seq        INTEGER     NOT NULL CHECK (seq > 0),
    kind       TEXT        NOT NULL CHECK (kind IN ('point', 'undo')),
    -- Which side won the rally; NULL for an undo.
    side       TEXT        CHECK (side IN ('A', 'B')),
    -- Chosen by the judge's app for each point, so a resent point (after a
    -- dropped connection) is recognised instead of counted twice.
    client_id  TEXT        NOT NULL CHECK (length(client_id) BETWEEN 1 AND 64),
    judge_id   TEXT        NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (match_id, seq),
    UNIQUE (match_id, client_id),
    CHECK ((kind = 'point') = (side IS NOT NULL))
);
