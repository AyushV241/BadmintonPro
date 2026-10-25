package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const judgeColumns = `j.user_id, j.name, j.phone, COALESCE(u.username, ''), j.created_at`

func scanJudge(row pgx.Row) (RegisteredJudge, error) {
	var j RegisteredJudge
	err := row.Scan(&j.UserID, &j.Name, &j.Phone, &j.Username, &j.CreatedAt)
	j.CreatedAt = j.CreatedAt.UTC()
	return j, err
}

func (s *PostgresStore) ListJudges(ctx context.Context) ([]RegisteredJudge, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+judgeColumns+`
		FROM judges j
		JOIN users u ON u.id = j.user_id
		ORDER BY lower(j.name), j.user_id`)
	if err != nil {
		return nil, fmt.Errorf("list judges: %w", err)
	}
	judges, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RegisteredJudge, error) {
		return scanJudge(row)
	})
	if err != nil {
		return nil, fmt.Errorf("list judges: %w", err)
	}
	return judges, nil
}

func (s *PostgresStore) AddJudge(ctx context.Context, userID, name, phone, createdBy string) (RegisteredJudge, error) {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO judges (user_id, name, phone, created_by) VALUES ($1, $2, $3, NULLIF($4, ''))`,
		userID, name, phone, createdBy)
	switch {
	case isUniqueViolation(err):
		return RegisteredJudge{}, ErrJudgeExists
	case isForeignKeyViolation(err):
		return RegisteredJudge{}, ErrNoSuchUser
	case err != nil:
		return RegisteredJudge{}, fmt.Errorf("add judge: %w", err)
	}
	return s.registeredJudge(ctx, s.pool, userID)
}

func (s *PostgresStore) registeredJudge(ctx context.Context, q querier, userID string) (RegisteredJudge, error) {
	j, err := scanJudge(q.QueryRow(ctx, `
		SELECT `+judgeColumns+`
		FROM judges j
		JOIN users u ON u.id = j.user_id
		WHERE j.user_id = $1`, userID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return RegisteredJudge{}, ErrNotAJudge
	case err != nil:
		return RegisteredJudge{}, fmt.Errorf("load judge: %w", err)
	}
	return j, nil
}

func (s *PostgresStore) RemoveJudge(ctx context.Context, userID string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM judges WHERE user_id = $1`, userID)
		if err != nil {
			return fmt.Errorf("remove judge: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotAJudge
		}
		// Off the judge list of events still to come, so the matcher stops
		// picking them. Matches already scheduled are never moved.
		if _, err := tx.Exec(ctx, `
			DELETE FROM event_judges ej
			USING events e
			WHERE e.id = ej.event_id AND ej.user_id = $1 AND e.ends_at > now()`, userID); err != nil {
			return fmt.Errorf("remove judge from events: %w", err)
		}
		// Their open judge sessions fall back to the player home page.
		if _, err := tx.Exec(ctx,
			`UPDATE sessions SET mode = $2 WHERE user_id = $1 AND mode = $3`,
			userID, SessionPlayer, SessionJudge); err != nil {
			return fmt.Errorf("reset judge sessions: %w", err)
		}
		return nil
	})
}

func (s *PostgresStore) JudgeForPhone(ctx context.Context, phone string) (User, error) {
	// The number is matched through its sign-in identity, the same lookup a
	// normal phone sign-in uses, so judge and player sign-ins with one
	// number always reach the same account.
	u, err := scanUser(s.pool.QueryRow(ctx, `
		SELECT `+userColumns+`
		FROM user_identities ui
		JOIN judges j ON j.user_id = ui.user_id
		JOIN users u ON u.id = ui.user_id
		WHERE ui.provider = $1 AND ui.subject = $2`,
		phoneIdentityProvider, phone))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return User{}, ErrNotAJudge
	case err != nil:
		return User{}, fmt.Errorf("judge for phone: %w", err)
	}
	return u, nil
}

func (s *PostgresStore) IsJudge(ctx context.Context, userID string) (bool, error) {
	var ok bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM judges WHERE user_id = $1)`, userID,
	).Scan(&ok); err != nil {
		return false, fmt.Errorf("is judge: %w", err)
	}
	return ok, nil
}

const (
	judgeEventsLimit  = 50
	judgeMatchesLimit = 100
	// judgeRecentMatches keeps the day's finished matches on the page.
	judgeRecentMatches = 24 * time.Hour
)

func (s *PostgresStore) JudgeHome(ctx context.Context, userID string, now time.Time) (JudgeHome, error) {
	judge, err := s.registeredJudge(ctx, s.pool, userID)
	if err != nil {
		return JudgeHome{}, err
	}
	home := JudgeHome{Judge: judge, Events: []JudgeEvent{}, Matches: []PlayerMatch{}}

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.title, e.status, e.starts_at, e.ends_at, `+venueColumns+`
		FROM event_judges ej
		JOIN events e ON e.id = ej.event_id
		JOIN venues v ON v.id = e.venue_id
		WHERE ej.user_id = $1 AND e.status <> $2 AND e.ends_at > $3
		ORDER BY e.starts_at, e.id
		LIMIT $4`, userID, EventCancelled, now, judgeEventsLimit)
	if err != nil {
		return JudgeHome{}, fmt.Errorf("judge events: %w", err)
	}
	home.Events, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (JudgeEvent, error) {
		var e JudgeEvent
		v := &e.Venue
		err := row.Scan(&e.ID, &e.Title, &e.Status, &e.StartsAt, &e.EndsAt,
			&v.ID, &v.Name, &v.Address, &v.City, &v.Lat, &v.Lng, &v.Timezone)
		e.StartsAt, e.EndsAt = e.StartsAt.UTC(), e.EndsAt.UTC()
		return e, err
	})
	if err != nil {
		return JudgeHome{}, fmt.Errorf("judge events: %w", err)
	}

	rows, err = s.pool.Query(ctx, `
		SELECT m.id, m.status, m.format, m.starts_at, m.ends_at, m.winner_side, m.source,
			e.id, e.title,
			c.id, c.label,
			`+venueColumns+`,
			ch.id
		FROM matches m
		LEFT JOIN events e ON e.id = m.event_id
		JOIN courts c ON c.id = m.court_id
		JOIN venues v ON v.id = c.venue_id
		LEFT JOIN challenges ch ON ch.match_id = m.id
		WHERE m.judge_id = $1 AND m.status <> $2 AND m.ends_at > $3
		ORDER BY m.starts_at, m.id
		LIMIT $4`, userID, MatchCancelled, now.Add(-judgeRecentMatches), judgeMatchesLimit)
	if err != nil {
		return JudgeHome{}, fmt.Errorf("judge matches: %w", err)
	}
	self := &Judge{UserID: judge.UserID, Name: judge.Name, Username: judge.Username}
	matches, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PlayerMatch, error) {
		var m PlayerMatch
		var eventID, eventTitle *string
		v := &m.Venue
		err := row.Scan(&m.ID, &m.Status, &m.Format, &m.StartsAt, &m.EndsAt, &m.WinnerSide, &m.Source,
			&eventID, &eventTitle,
			&m.Court.ID, &m.Court.Label,
			&v.ID, &v.Name, &v.Address, &v.City, &v.Lat, &v.Lng, &v.Timezone,
			&m.ChallengeID)
		if eventID != nil {
			m.Event = &MatchEvent{ID: *eventID, Title: *eventTitle}
		}
		m.Judge = self
		m.StartsAt, m.EndsAt = m.StartsAt.UTC(), m.EndsAt.UTC()
		m.Sides = []MatchSide{}
		return m, err
	})
	if err != nil {
		return JudgeHome{}, fmt.Errorf("judge matches: %w", err)
	}
	if err := s.loadMatchSides(ctx, matches); err != nil {
		return JudgeHome{}, err
	}
	home.Matches = matches
	return home, nil
}

// loadMatchSides fills in both sides of each match, with each player's
// current rating in the match's format.
func (s *PostgresStore) loadMatchSides(ctx context.Context, matches []PlayerMatch) error {
	if len(matches) == 0 {
		return nil
	}
	ids := make([]string, len(matches))
	byID := make(map[string]*PlayerMatch, len(matches))
	for i := range matches {
		ids[i] = matches[i].ID
		byID[matches[i].ID] = &matches[i]
	}
	rows, err := s.pool.Query(ctx, `
		SELECT ms.match_id, ms.side, u.id, u.name, COALESCE(u.username, ''), COALESCE(r.rating, $2)
		FROM match_sides ms
		JOIN matches m ON m.id = ms.match_id
		JOIN users u ON u.id = ms.user_id
		LEFT JOIN player_ratings r ON r.user_id = ms.user_id AND r.discipline = m.format
		WHERE ms.match_id = ANY($1)
		ORDER BY ms.match_id, ms.side, COALESCE(r.rating, $2) DESC, u.id`, ids, startingRating)
	if err != nil {
		return fmt.Errorf("match sides: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var matchID, side string
		var p MatchPlayer
		if err := rows.Scan(&matchID, &side, &p.UserID, &p.Name, &p.Username, &p.Rating); err != nil {
			return fmt.Errorf("match sides: %w", err)
		}
		m := byID[matchID]
		if n := len(m.Sides); n == 0 || m.Sides[n-1].Side != side {
			m.Sides = append(m.Sides, MatchSide{Side: side})
		}
		last := &m.Sides[len(m.Sides)-1]
		last.Players = append(last.Players, p)
		last.Rating = sideAverage(last.Players)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("match sides: %w", err)
	}
	return nil
}
