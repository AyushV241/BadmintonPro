package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// eventColumns is an events row joined to its venue, aliased e and v.
// scanEvent reads them in this order.
const eventColumns = `e.id, e.title, e.format, e.status, e.starts_at, e.ends_at,
	e.fee_paise, e.currency, e.capacity, e.rating_min, e.rating_max,
	v.id, v.name, v.address, v.city, v.lat, v.lng, v.timezone`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.Title, &e.Format, &e.Status, &e.StartsAt, &e.EndsAt,
		&e.FeePaise, &e.Currency, &e.Capacity, &e.RatingMin, &e.RatingMax,
		&e.Venue.ID, &e.Venue.Name, &e.Venue.Address, &e.Venue.City, &e.Venue.Lat, &e.Venue.Lng, &e.Venue.Timezone)
	e.StartsAt, e.EndsAt = e.StartsAt.UTC(), e.EndsAt.UTC()
	// No registrations yet, so every place is free.
	e.SpotsLeft = e.Capacity
	return e, err
}

func (s *PostgresStore) ListEvents(ctx context.Context, q EventQuery) ([]Event, bool, error) {
	if q.Limit < 1 {
		return nil, false, errors.New("limit must be positive")
	}

	var afterStart any // nil: first page
	var afterID string
	if q.After != nil {
		afterStart, afterID = q.After.StartsAt, q.After.ID
	}

	// One row past the limit says whether another page follows.
	rows, err := s.pool.Query(ctx, `
		SELECT `+eventColumns+`
		FROM events e JOIN venues v ON v.id = e.venue_id
		WHERE e.status = 'published'
		  AND e.ends_at > $1
		  AND ($2::text = '' OR v.city = $2)
		  AND ($3::timestamptz IS NULL OR (e.starts_at, e.id) > ($3, $4::text))
		ORDER BY e.starts_at, e.id
		LIMIT $5`,
		q.Now, q.City, afterStart, afterID, q.Limit+1,
	)
	if err != nil {
		return nil, false, fmt.Errorf("list events: %w", err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) { return scanEvent(row) })
	if err != nil {
		return nil, false, fmt.Errorf("list events: %w", err)
	}

	more := len(events) > q.Limit
	if more {
		events = events[:q.Limit]
	}
	return events, more, nil
}

func (s *PostgresStore) GetEvent(ctx context.Context, id string) (Event, error) {
	e, err := scanEvent(s.pool.QueryRow(ctx,
		`SELECT `+eventColumns+` FROM events e JOIN venues v ON v.id = e.venue_id WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNoSuchEvent
	}
	if err != nil {
		return Event{}, fmt.Errorf("get event: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.label
		FROM event_courts ec JOIN courts c ON c.id = ec.court_id
		WHERE ec.event_id = $1
		ORDER BY c.label`, id)
	if err != nil {
		return Event{}, fmt.Errorf("event courts: %w", err)
	}
	e.Courts, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Court])
	if err != nil {
		return Event{}, fmt.Errorf("event courts: %w", err)
	}
	return e, nil
}
