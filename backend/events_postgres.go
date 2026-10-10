package main

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
)

// eventColumns is an events row joined to its venue, aliased e and v.
// scanEvent reads them in this order.
const eventColumns = `e.id, e.title, e.formats,
	e.singles_slot_minutes, e.doubles_slot_minutes, e.mixed_slot_minutes,
	e.status, e.starts_at, e.ends_at, e.registration_closes_at,
	e.fee_paise, e.currency, e.capacity, e.max_matches, e.rating_min, e.rating_max,
	v.id, v.name, v.address, v.city, v.lat, v.lng, v.timezone`

// scanEvent reads eventColumns, then any extra columns into extra.
func scanEvent(row pgx.Row, extra ...any) (Event, error) {
	var e Event
	var singles, doubles, mixed int
	dest := append([]any{&e.ID, &e.Title, &e.Formats,
		&singles, &doubles, &mixed,
		&e.Status, &e.StartsAt, &e.EndsAt, &e.RegistrationClosesAt,
		&e.FeePaise, &e.Currency, &e.Capacity, &e.MaxMatches, &e.RatingMin, &e.RatingMax,
		&e.Venue.ID, &e.Venue.Name, &e.Venue.Address, &e.Venue.City, &e.Venue.Lat, &e.Venue.Lng, &e.Venue.Timezone,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		return Event{}, err
	}

	e.StartsAt, e.EndsAt, e.RegistrationClosesAt = e.StartsAt.UTC(), e.EndsAt.UTC(), e.RegistrationClosesAt.UTC()
	slot := map[string]int{FormatSingles: singles, FormatDoubles: doubles, FormatMixed: mixed}
	e.SlotMinutes = make(map[string]int, len(e.Formats))
	for _, f := range e.Formats {
		e.SlotMinutes[f] = slot[f]
	}
	// No registrations yet, so every place is free.
	e.SpotsLeft = e.Capacity
	return e, nil
}

// distanceSQL is the great-circle (haversine) distance in km from the point
// ($lat, $lng) to the venue v; NULL when either is missing. LEAST guards asin
// against rounding just above 1.
const distanceSQL = `6371 * 2 * asin(least(1, sqrt(
	power(sin(radians(v.lat - $6::float8) / 2), 2) +
	cos(radians($6::float8)) * cos(radians(v.lat)) * power(sin(radians(v.lng - $7::float8) / 2), 2))))`

func (s *PostgresStore) ListEvents(ctx context.Context, q EventQuery) ([]Event, bool, error) {
	if q.Limit < 1 {
		return nil, false, errors.New("limit must be positive")
	}

	var afterStart any // nil: first page
	var afterID string
	if q.After != nil {
		afterStart, afterID = q.After.StartsAt, q.After.ID
	}
	var lat, lng any // nil: no location search
	if q.Near != nil {
		lat, lng = q.Near.Lat, q.Near.Lng
	}

	// One row past the limit says whether another page follows.
	rows, err := s.pool.Query(ctx, `
		SELECT `+eventColumns+`, d.km
		FROM events e
		JOIN venues v ON v.id = e.venue_id
		CROSS JOIN LATERAL (SELECT CASE WHEN $6::float8 IS NULL THEN NULL ELSE `+distanceSQL+` END AS km) d
		WHERE e.status = 'published'
		  AND e.ends_at > $1
		  AND ($2::text = '' OR v.city = $2)
		  AND ($3::timestamptz IS NULL OR (e.starts_at, e.id) > ($3, $4::text))
		  AND ($6::float8 IS NULL OR d.km <= $8)
		ORDER BY e.starts_at, e.id
		LIMIT $5`,
		q.Now, q.City, afterStart, afterID, q.Limit+1, lat, lng, q.RadiusKm,
	)
	if err != nil {
		return nil, false, fmt.Errorf("list events: %w", err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
		var km *float64
		e, err := scanEvent(row, &km)
		if km != nil {
			rounded := math.Round(*km*10) / 10
			e.DistanceKm = &rounded
		}
		return e, err
	})
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
