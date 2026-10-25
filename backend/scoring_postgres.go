package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// liveChannel is the Postgres NOTIFY channel a match's ID is sent on whenever
// its live state changes. Every server instance LISTENs (live.go), so a
// point scored through one reaches viewers connected to another.
const liveChannel = "match_live"

func notifyLive(ctx context.Context, tx pgx.Tx, matchID string) error {
	// Delivered when the transaction commits, and not at all if it rolls back.
	_, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, liveChannel, matchID)
	return err
}

func (s *PostgresStore) LiveMatch(ctx context.Context, matchID string) (LiveMatch, error) {
	var (
		m                        LiveMatch
		eventID, eventTitle      *string
		judgeID                  *string
		judgeName, judgeUsername string
		voidedAt                 *time.Time
		voidReason, voidedFrom   string
	)
	v := &m.Venue
	err := s.pool.QueryRow(ctx, `
		SELECT m.id, m.status, m.format, m.starts_at, m.ends_at, m.started_at, m.completed_at,
			COALESCE(m.first_server, ''), m.winner_side, m.retired_side,
			m.voided_at, COALESCE(m.void_reason, ''), COALESCE(m.voided_from, ''),
			e.id, e.title,
			c.id, c.label,
			`+venueColumns+`,
			j.id, COALESCE(j.name, ''), COALESCE(j.username, '')
		FROM matches m
		LEFT JOIN events e ON e.id = m.event_id
		JOIN courts c ON c.id = m.court_id
		JOIN venues v ON v.id = c.venue_id
		LEFT JOIN users j ON j.id = m.judge_id
		WHERE m.id = $1`, matchID,
	).Scan(&m.ID, &m.Status, &m.Format, &m.StartsAt, &m.EndsAt, &m.StartedAt, &m.CompletedAt,
		&m.FirstServer, &m.WinnerSide, &m.RetiredSide,
		&voidedAt, &voidReason, &voidedFrom,
		&eventID, &eventTitle,
		&m.Court.ID, &m.Court.Label,
		&v.ID, &v.Name, &v.Address, &v.City, &v.Lat, &v.Lng, &v.Timezone,
		&judgeID, &judgeName, &judgeUsername)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return LiveMatch{}, ErrNoSuchMatch
	case err != nil:
		return LiveMatch{}, fmt.Errorf("load live match: %w", err)
	}
	if eventID != nil {
		m.Event = &MatchEvent{ID: *eventID, Title: *eventTitle}
	}
	if judgeID != nil {
		m.Judge = &Judge{UserID: *judgeID, Name: judgeName, Username: judgeUsername}
	}
	if voidedAt != nil {
		m.Void = &MatchVoidInfo{At: voidedAt.UTC(), Reason: voidReason, From: voidedFrom}
	}
	m.StartsAt, m.EndsAt = m.StartsAt.UTC(), m.EndsAt.UTC()
	m.CanStartFrom = m.StartsAt.Add(-startEarly)
	for _, t := range []**time.Time{&m.StartedAt, &m.CompletedAt} {
		if *t != nil {
			u := (**t).UTC()
			*t = &u
		}
	}

	// Both sides, through the same loader as a judge's match list.
	sides := []PlayerMatch{{ID: m.ID, Sides: []MatchSide{}}}
	if err := s.loadMatchSides(ctx, sides); err != nil {
		return LiveMatch{}, err
	}
	m.Sides = sides[0].Sides

	events, err := loadPoints(ctx, s.pool, matchID)
	if err != nil {
		return LiveMatch{}, err
	}
	m.Score = computeScore(m.FirstServer, events)
	m.NextSeq = len(events) + 1

	m.RatingSystem = s.ratings.primary.Name()
	if m.RatingChanges, err = s.matchRatingChanges(ctx, matchID); err != nil {
		return LiveMatch{}, err
	}
	return m, nil
}

// pointsQuerier is satisfied by both the pool and a transaction.
type pointsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func loadPoints(ctx context.Context, q pointsQuerier, matchID string) ([]PointEvent, error) {
	rows, err := q.Query(ctx, `
		SELECT seq, kind, COALESCE(side, ''), created_at
		FROM match_points WHERE match_id = $1 ORDER BY seq`, matchID)
	if err != nil {
		return nil, fmt.Errorf("load points: %w", err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PointEvent, error) {
		var ev PointEvent
		err := row.Scan(&ev.Seq, &ev.Kind, &ev.Side, &ev.At)
		ev.At = ev.At.UTC()
		return ev, err
	})
	if err != nil {
		return nil, fmt.Errorf("load points: %w", err)
	}
	return events, nil
}

// lockedMatch is what the scoring writes check, read under the match's row
// lock. Fixture editing locks the event, the match, then entries; scoring
// locks the match, then entries or ratings, so the two can't deadlock, and
// a move racing a start waits for it.
type lockedMatch struct {
	judgeID     *string
	status      string
	startsAt    time.Time
	firstServer string
}

// lockMatch locks a match for its judge: ErrNotYourMatch for anyone else.
func lockMatch(ctx context.Context, tx pgx.Tx, matchID, judgeID string) (lockedMatch, error) {
	m, err := lockMatchRow(ctx, tx, matchID)
	if err != nil {
		return lockedMatch{}, err
	}
	if m.judgeID == nil || *m.judgeID != judgeID {
		return lockedMatch{}, ErrNotYourMatch
	}
	return m, nil
}

// lockMatchRow locks a match for anyone; admins' actions use it directly.
func lockMatchRow(ctx context.Context, tx pgx.Tx, matchID string) (lockedMatch, error) {
	var m lockedMatch
	err := tx.QueryRow(ctx, `
		SELECT judge_id, status, starts_at, COALESCE(first_server, '')
		FROM matches WHERE id = $1 FOR UPDATE`, matchID,
	).Scan(&m.judgeID, &m.status, &m.startsAt, &m.firstServer)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return lockedMatch{}, ErrNoSuchMatch
	case err != nil:
		return lockedMatch{}, fmt.Errorf("lock match: %w", err)
	}
	return m, nil
}

func (s *PostgresStore) StartMatch(ctx context.Context, matchID, judgeID, firstServer string, now time.Time) (LiveMatch, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		m, err := lockMatch(ctx, tx, matchID, judgeID)
		if err != nil {
			return err
		}
		if m.status != MatchScheduled && m.status != MatchReady {
			return ErrMatchNotStartable
		}
		if from := m.startsAt.Add(-startEarly); now.Before(from) {
			return &TooEarlyError{From: from}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE matches SET status = $2, first_server = $3, started_at = $4, updated_at = now()
			WHERE id = $1`, matchID, MatchLive, firstServer, now); err != nil {
			return fmt.Errorf("start match: %w", err)
		}
		return notifyLive(ctx, tx, matchID)
	})
	if err != nil {
		return LiveMatch{}, err
	}
	return s.LiveMatch(ctx, matchID)
}

func (s *PostgresStore) AddPoint(ctx context.Context, matchID, judgeID string, in PointInput) (LiveMatch, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		m, err := lockMatch(ctx, tx, matchID, judgeID)
		if err != nil {
			return err
		}
		// A resend of a point already logged: nothing to add, even if the
		// match has been finished since.
		var seen bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM match_points WHERE match_id = $1 AND client_id = $2)`,
			matchID, in.ClientID,
		).Scan(&seen); err != nil {
			return fmt.Errorf("check point: %w", err)
		}
		if seen {
			return nil
		}
		if m.status != MatchLive {
			return ErrMatchNotLive
		}

		events, err := loadPoints(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if next := len(events) + 1; in.Seq != next {
			return &SeqMismatchError{Expected: next}
		}
		score := computeScore(m.firstServer, events)
		switch {
		case in.Kind == PointRally && score.Winner != "":
			return ErrMatchDecided
		case in.Kind == PointUndo && score.points == 0:
			return ErrNothingToUndo
		}

		var side any // NULL for an undo
		if in.Kind == PointRally {
			side = in.Side
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO match_points (match_id, seq, kind, side, client_id, judge_id)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			matchID, in.Seq, in.Kind, side, in.ClientID, judgeID); err != nil {
			return fmt.Errorf("add point: %w", err)
		}
		return notifyLive(ctx, tx, matchID)
	})
	if err != nil {
		return LiveMatch{}, err
	}
	return s.LiveMatch(ctx, matchID)
}

func (s *PostgresStore) FinishMatch(ctx context.Context, matchID, judgeID string, now time.Time) (LiveMatch, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		m, err := lockMatch(ctx, tx, matchID, judgeID)
		if err != nil {
			return err
		}
		if m.status != MatchLive {
			return ErrMatchNotLive
		}
		events, err := loadPoints(ctx, tx, matchID)
		if err != nil {
			return err
		}
		score := computeScore(m.firstServer, events)
		if score.Winner == "" {
			return ErrMatchNotDecided
		}
		if _, err := tx.Exec(ctx, `
			UPDATE matches SET status = $2, winner_side = $3, completed_at = $4, updated_at = now()
			WHERE id = $1`, matchID, MatchCompleted, score.Winner, now); err != nil {
			return fmt.Errorf("finish match: %w", err)
		}
		if err := s.applyRatings(ctx, tx, matchID); err != nil {
			return err
		}
		return notifyLive(ctx, tx, matchID)
	})
	if err != nil {
		return LiveMatch{}, err
	}
	return s.LiveMatch(ctx, matchID)
}

func (s *PostgresStore) RetireMatch(ctx context.Context, matchID, judgeID, retiredSide string, now time.Time) (LiveMatch, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		m, err := lockMatch(ctx, tx, matchID, judgeID)
		if err != nil {
			return err
		}
		if m.status != MatchLive {
			return ErrMatchNotLive
		}
		events, err := loadPoints(ctx, tx, matchID)
		if err != nil {
			return err
		}
		score := computeScore(m.firstServer, events)
		if score.Winner != "" {
			return ErrMatchDecided // it was played out: finish it
		}
		winner := otherSide(retiredSide)
		if _, err := tx.Exec(ctx, `
			UPDATE matches SET status = $2, winner_side = $3, retired_side = $4, completed_at = $5, updated_at = now()
			WHERE id = $1`, matchID, MatchRetired, winner, retiredSide, now); err != nil {
			return fmt.Errorf("retire match: %w", err)
		}
		// Rated on the points played; not at all if there were none.
		if err := s.applyRatings(ctx, tx, matchID); err != nil {
			return err
		}
		return notifyLive(ctx, tx, matchID)
	})
	if err != nil {
		return LiveMatch{}, err
	}
	return s.LiveMatch(ctx, matchID)
}

func (s *PostgresStore) Walkover(ctx context.Context, matchID, judgeID, winnerSide string, now time.Time) (LiveMatch, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var m lockedMatch
		var err error
		if judgeID == "" {
			m, err = lockMatchRow(ctx, tx, matchID)
		} else {
			m, err = lockMatch(ctx, tx, matchID, judgeID)
		}
		if err != nil {
			return err
		}
		if m.status != MatchScheduled && m.status != MatchReady {
			return ErrNoWalkover
		}
		// Nobody can be missing before they were due.
		if from := m.startsAt.Add(-startEarly); now.Before(from) {
			return &TooEarlyError{From: from}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE matches SET status = $2, winner_side = $3, completed_at = $4, updated_at = now()
			WHERE id = $1`, matchID, MatchWalkover, winnerSide, now); err != nil {
			return fmt.Errorf("walkover: %w", err)
		}
		// The absent players keep no refund, as when the desk swaps out a
		// no-show (section 4.4).
		if _, err := tx.Exec(ctx, `
			UPDATE entries SET status = $3, updated_at = now()
			WHERE status = $4 AND id IN (
				SELECT entry_id FROM match_sides WHERE match_id = $1 AND side = $2 AND entry_id IS NOT NULL)`,
			matchID, otherSide(winnerSide), EntryNoShow, EntryScheduled); err != nil {
			return fmt.Errorf("mark no-shows: %w", err)
		}
		return notifyLive(ctx, tx, matchID)
	})
	if err != nil {
		return LiveMatch{}, err
	}
	return s.LiveMatch(ctx, matchID)
}

func (s *PostgresStore) VoidMatch(ctx context.Context, matchID, adminID, reason string, now time.Time) (LiveMatch, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		m, err := lockMatchRow(ctx, tx, matchID)
		if err != nil {
			return err
		}
		switch m.status {
		case MatchLive, MatchCompleted, MatchRetired, MatchWalkover:
		default:
			return ErrNotVoidable
		}
		// The result stays on the row (winner, retired side) as a record of
		// what was struck off; the status says it doesn't count.
		if _, err := tx.Exec(ctx, `
			UPDATE matches SET status = $2, voided_at = $3, voided_by = $4, void_reason = $5, voided_from = $6,
				updated_at = now()
			WHERE id = $1`, matchID, MatchVoid, now, adminID, reason, m.status); err != nil {
			return fmt.Errorf("void match: %w", err)
		}
		if err := s.voidRatings(ctx, tx, matchID); err != nil {
			return err
		}
		if m.status == MatchWalkover {
			// Nobody was absent after all, or it doesn't matter now. The
			// match_sides rows name the entries: one the desk swapped out as
			// a no-show isn't there any more, and stays no_show.
			if _, err := tx.Exec(ctx, `
				UPDATE entries SET status = $2, updated_at = now()
				WHERE status = $3 AND id IN (
					SELECT entry_id FROM match_sides WHERE match_id = $1 AND entry_id IS NOT NULL)`,
				matchID, EntryScheduled, EntryNoShow); err != nil {
				return fmt.Errorf("restore no-shows: %w", err)
			}
		}
		return notifyLive(ctx, tx, matchID)
	})
	if err != nil {
		return LiveMatch{}, err
	}
	return s.LiveMatch(ctx, matchID)
}

func otherSide(side string) string {
	if side == "A" {
		return "B"
	}
	return "A"
}
