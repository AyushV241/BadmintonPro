package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// foreignKeyViolation is the SQLSTATE code for a missing referenced row.
const foreignKeyViolation = "23503"

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation
}

func newID(prefix string) (string, error) {
	id, err := randomHex(12)
	if err != nil {
		return "", err
	}
	return prefix + "_" + id, nil
}

const venueColumns = `v.id, v.name, v.address, v.city, v.lat, v.lng, v.timezone`

func scanVenue(row pgx.Row) (AdminVenue, error) {
	var v AdminVenue
	err := row.Scan(&v.ID, &v.Name, &v.Address, &v.City, &v.Lat, &v.Lng, &v.Timezone)
	v.Courts = []AdminCourt{}
	return v, err
}

// attachCourts fills in each venue's courts, in label order.
func (s *PostgresStore) attachCourts(ctx context.Context, venues []AdminVenue) error {
	if len(venues) == 0 {
		return nil
	}
	ids := make([]string, len(venues))
	byID := make(map[string]*AdminVenue, len(venues))
	for i := range venues {
		ids[i] = venues[i].ID
		byID[venues[i].ID] = &venues[i]
	}
	rows, err := s.pool.Query(ctx, `
		SELECT venue_id, id, label, active FROM courts
		WHERE venue_id = ANY($1)
		ORDER BY length(label), label`, ids)
	if err != nil {
		return fmt.Errorf("venue courts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var venueID string
		var c AdminCourt
		if err := rows.Scan(&venueID, &c.ID, &c.Label, &c.Active); err != nil {
			return err
		}
		byID[venueID].Courts = append(byID[venueID].Courts, c)
	}
	return rows.Err()
}

func (s *PostgresStore) ListVenues(ctx context.Context) ([]AdminVenue, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+venueColumns+` FROM venues v ORDER BY v.city, v.name`)
	if err != nil {
		return nil, fmt.Errorf("list venues: %w", err)
	}
	venues, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminVenue, error) { return scanVenue(row) })
	if err != nil {
		return nil, fmt.Errorf("list venues: %w", err)
	}
	return venues, s.attachCourts(ctx, venues)
}

func (s *PostgresStore) GetVenue(ctx context.Context, id string) (AdminVenue, error) {
	v, err := scanVenue(s.pool.QueryRow(ctx, `SELECT `+venueColumns+` FROM venues v WHERE v.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminVenue{}, ErrNoSuchVenue
	}
	if err != nil {
		return AdminVenue{}, fmt.Errorf("get venue: %w", err)
	}
	venues := []AdminVenue{v}
	if err := s.attachCourts(ctx, venues); err != nil {
		return AdminVenue{}, err
	}
	return venues[0], nil
}

func (s *PostgresStore) CreateVenue(ctx context.Context, in VenueInput) (AdminVenue, error) {
	id, err := newID("ven")
	if err != nil {
		return AdminVenue{}, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO venues (id, name, address, city, lat, lng, timezone)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, in.Name, in.Address, in.City, in.Lat, in.Lng, in.Timezone,
		); err != nil {
			return err
		}
		for n := 1; n <= in.Courts; n++ {
			courtID, err := newID("crt")
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO courts (id, venue_id, label) VALUES ($1, $2, $3)`,
				courtID, id, fmt.Sprintf("Court %d", n)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return AdminVenue{}, fmt.Errorf("create venue: %w", err)
	}
	return s.GetVenue(ctx, id)
}

func (s *PostgresStore) AddCourt(ctx context.Context, venueID, label string) (AdminCourt, error) {
	id, err := newID("crt")
	if err != nil {
		return AdminCourt{}, err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO courts (id, venue_id, label) VALUES ($1, $2, $3)`, id, venueID, label)
	switch {
	case isUniqueViolation(err):
		return AdminCourt{}, ErrCourtLabelTaken
	case isForeignKeyViolation(err):
		return AdminCourt{}, ErrNoSuchVenue
	case err != nil:
		return AdminCourt{}, fmt.Errorf("add court: %w", err)
	}
	return AdminCourt{ID: id, Label: label, Active: true}, nil
}

// adminEventsLimit caps the admin list until it needs paging.
const adminEventsLimit = 200

func (s *PostgresStore) ListAllEvents(ctx context.Context, status string) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+eventColumns+`
		FROM events e JOIN venues v ON v.id = e.venue_id
		WHERE $1::text = '' OR e.status = $1
		ORDER BY e.starts_at DESC, e.id
		LIMIT $2`, status, adminEventsLimit)
	if err != nil {
		return nil, fmt.Errorf("list all events: %w", err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) { return scanEvent(row) })
	if err != nil {
		return nil, fmt.Errorf("list all events: %w", err)
	}
	return events, nil
}

// eventValues are EventInput's columns for INSERT and UPDATE, from $2 on.
func eventValues(in EventInput) []any {
	slot := func(f string) int {
		if m, ok := in.SlotMinutes[f]; ok {
			return m
		}
		return defaultSlotMinutes[f]
	}
	return []any{in.VenueID, in.Title, in.Formats,
		slot(FormatSingles), slot(FormatDoubles), slot(FormatMixed),
		in.StartsAt, in.EndsAt, in.RegistrationClosesAt,
		in.FeePaise, in.Capacity, in.MaxMatches, in.RatingMin, in.RatingMax}
}

func insertEventCourts(ctx context.Context, tx pgx.Tx, eventID string, in EventInput) error {
	for _, courtID := range in.CourtIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO event_courts (event_id, court_id, venue_id) VALUES ($1, $2, $3)`,
			eventID, courtID, in.VenueID); err != nil {
			return fmt.Errorf("event court: %w", err)
		}
	}
	return nil
}

// checkCourtsFree returns a *CourtBusyError if an event other than eventID
// that isn't cancelled (drafts included) uses one of courtIDs at a time
// overlapping [startsAt, endsAt). Events that only touch, one ending as the
// next starts, don't overlap.
//
// It locks the court rows first, in ID order so two saves can't deadlock.
// Two events saved at once on the same court then take turns, and the second
// sees the first. Callers that also lock an events row lock it before this.
func checkCourtsFree(ctx context.Context, tx pgx.Tx, eventID string, courtIDs []string, startsAt, endsAt time.Time) error {
	if _, err := tx.Exec(ctx, `SELECT id FROM courts WHERE id = ANY($1) ORDER BY id FOR UPDATE`, courtIDs); err != nil {
		return fmt.Errorf("lock courts: %w", err)
	}

	var clash CourtBusyError
	err := tx.QueryRow(ctx, `
		SELECT c.label, e.title
		FROM event_courts ec
		JOIN events e ON e.id = ec.event_id
		JOIN courts c ON c.id = ec.court_id
		WHERE ec.court_id = ANY($1)
		  AND e.id <> $2
		  AND e.status <> 'cancelled'
		  AND e.starts_at < $4 AND e.ends_at > $3
		ORDER BY e.starts_at, length(c.label), c.label
		LIMIT 1`, courtIDs, eventID, startsAt, endsAt).Scan(&clash.Court, &clash.Event)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("court overlap: %w", err)
	}
	return &clash
}

func (s *PostgresStore) CreateEvent(ctx context.Context, in EventInput, createdBy string) (string, error) {
	id, err := newID("evt")
	if err != nil {
		return "", err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := checkCourtsFree(ctx, tx, id, in.CourtIDs, in.StartsAt, in.EndsAt); err != nil {
			return err
		}
		args := append([]any{id}, eventValues(in)...)
		if _, err := tx.Exec(ctx, `
			INSERT INTO events (id, venue_id, title, formats,
				singles_slot_minutes, doubles_slot_minutes, mixed_slot_minutes,
				starts_at, ends_at, registration_closes_at,
				fee_paise, capacity, max_matches, rating_min, rating_max, created_by, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, 'draft')`,
			append(args, createdBy)...); err != nil {
			return err
		}
		return insertEventCourts(ctx, tx, id, in)
	})
	if err != nil {
		return "", fmt.Errorf("create event: %w", err)
	}
	return id, nil
}

// statusOf returns ErrNoSuchEvent for a missing event, else its status.
func statusOf(ctx context.Context, q querier, id string) (string, error) {
	var status string
	err := q.QueryRow(ctx, `SELECT status FROM events WHERE id = $1`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSuchEvent
	}
	return status, err
}

func (s *PostgresStore) UpdateEvent(ctx context.Context, id string, in EventInput) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Locks the row so a publish can't slip in between check and update.
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM events WHERE id = $1 FOR UPDATE`, id).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchEvent
		}
		if err != nil {
			return err
		}
		if status != EventDraft {
			return ErrEventNotEditable
		}
		if err := checkCourtsFree(ctx, tx, id, in.CourtIDs, in.StartsAt, in.EndsAt); err != nil {
			return err
		}

		// The courts go first: their keys tie them to the event's venue,
		// which the update may change.
		if _, err := tx.Exec(ctx, `DELETE FROM event_courts WHERE event_id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE events SET venue_id = $2, title = $3, formats = $4,
				singles_slot_minutes = $5, doubles_slot_minutes = $6, mixed_slot_minutes = $7,
				starts_at = $8, ends_at = $9, registration_closes_at = $10,
				fee_paise = $11, capacity = $12, max_matches = $13, rating_min = $14, rating_max = $15,
				updated_at = now()
			WHERE id = $1`, append([]any{id}, eventValues(in)...)...); err != nil {
			return fmt.Errorf("update event: %w", err)
		}
		return insertEventCourts(ctx, tx, id, in)
	})
}

func (s *PostgresStore) SetEventStatus(ctx context.Context, id string, from []string, to string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Locks the row so the status can't change between check and update.
		var status string
		var startsAt, endsAt time.Time
		err := tx.QueryRow(ctx, `SELECT status, starts_at, ends_at FROM events WHERE id = $1 FOR UPDATE`, id).
			Scan(&status, &startsAt, &endsAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchEvent
		}
		if err != nil {
			return fmt.Errorf("set event status: %w", err)
		}
		if !slices.Contains(from, status) {
			return ErrStatusChange
		}

		// Saving already checked the courts; checking again on publish keeps
		// rows written some other way (SQL, older data) from going live
		// double-booked.
		if to == EventPublished {
			rows, err := tx.Query(ctx, `SELECT court_id FROM event_courts WHERE event_id = $1`, id)
			if err != nil {
				return fmt.Errorf("event courts: %w", err)
			}
			courtIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return fmt.Errorf("event courts: %w", err)
			}
			if err := checkCourtsFree(ctx, tx, id, courtIDs, startsAt, endsAt); err != nil {
				return err
			}
		}

		if _, err := tx.Exec(ctx, `UPDATE events SET status = $2, updated_at = now() WHERE id = $1`, id, to); err != nil {
			return fmt.Errorf("set event status: %w", err)
		}
		return nil
	})
}

func (s *PostgresStore) ListEventJudges(ctx context.Context, eventID string) ([]Judge, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.name, COALESCE(u.username, '')
		FROM event_judges j JOIN users u ON u.id = j.user_id
		WHERE j.event_id = $1
		ORDER BY j.added_at, u.id`, eventID)
	if err != nil {
		return nil, fmt.Errorf("list judges: %w", err)
	}
	judges, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Judge])
	if err != nil {
		return nil, fmt.Errorf("list judges: %w", err)
	}
	if judges == nil {
		judges = []Judge{}
	}
	return judges, nil
}

func (s *PostgresStore) AddEventJudge(ctx context.Context, eventID, username string) (Judge, error) {
	if _, err := statusOf(ctx, s.pool, eventID); err != nil {
		return Judge{}, err
	}
	var j Judge
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, username FROM users WHERE lower(username) = $1`,
		strings.ToLower(strings.TrimSpace(username)),
	).Scan(&j.UserID, &j.Name, &j.Username)
	if errors.Is(err, pgx.ErrNoRows) {
		return Judge{}, ErrNoSuchUser
	}
	if err != nil {
		return Judge{}, fmt.Errorf("find judge: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO event_judges (event_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		eventID, j.UserID); err != nil {
		return Judge{}, fmt.Errorf("add judge: %w", err)
	}
	return j, nil
}

func (s *PostgresStore) RemoveEventJudge(ctx context.Context, eventID, userID string) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM event_judges WHERE event_id = $1 AND user_id = $2`, eventID, userID); err != nil {
		return fmt.Errorf("remove judge: %w", err)
	}
	return nil
}
