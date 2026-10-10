package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Demo data for `go run . -seed-demo`: three Bangalore venues with courts and
// a few events in the coming week. Fixed IDs make it safe to re-run: each run
// rewrites the same rows and moves the events to the next matching days.

const demoTimezone = "Asia/Kolkata"

type demoVenue struct {
	id, name, address string
	lat, lng          float64
	courts            int
}

type demoEvent struct {
	id, venueID, title, status      string
	formats                         []string
	weekday                         time.Weekday
	hour, minute, hours             int
	feeRupees, capacity, maxMatches int
	ratingMin, ratingMax            *int  // Elo band
	courts                          []int // court numbers at the venue
}

func elo(v int) *int { return &v }

var demoVenues = []demoVenue{
	{"ven_demo_koramangala", "Koramangala Sports Arena", "80 Feet Road, Koramangala", 12.9352, 77.6245, 4},
	{"ven_demo_hsr", "HSR Shuttle Club", "27th Main Road, HSR Layout", 12.9116, 77.6474, 2},
	{"ven_demo_indiranagar", "Indiranagar Badminton Hub", "100 Feet Road, Indiranagar", 12.9784, 77.6408, 3},
}

// Each event's ₹50 slot fee secures a place in its pool. maxMatches stays
// within the courts' slots: e.g. 4 courts × 3 h of 30-minute singles = 24.
var demoEvents = []demoEvent{
	{"evt_demo_saturday_smash", "ven_demo_koramangala", "Saturday Smash Session", EventPublished,
		[]string{FormatSingles}, time.Saturday, 19, 30, 3, 50, 24, 20, elo(1300), elo(1700), []int{1, 2, 3, 4}},
	{"evt_demo_sunday_ladder", "ven_demo_hsr", "Sunday Doubles Ladder", EventPublished,
		[]string{FormatDoubles, FormatMixed}, time.Sunday, 6, 0, 3, 50, 16, 8, nil, nil, []int{1, 2}},
	{"evt_demo_midweek_rally", "ven_demo_indiranagar", "Midweek Rally Night", EventPublished,
		[]string{FormatSingles, FormatDoubles}, time.Wednesday, 20, 0, 2, 50, 16, 10, nil, elo(1600), []int{1, 2, 3}},
	// Never listed: shows that drafts stay hidden.
	{"evt_demo_draft_open", "ven_demo_koramangala", "Monsoon Open (draft)", EventDraft,
		[]string{FormatSingles, FormatDoubles, FormatMixed}, time.Friday, 18, 0, 4, 50, 32, 24, nil, nil, []int{1, 2}},
}

// demoRegistrationWindow is how long before the start joining closes.
const demoRegistrationWindow = 2 * time.Hour

func demoCourtID(venueID string, n int) string {
	return fmt.Sprintf("crt_%s_%d", strings.TrimPrefix(venueID, "ven_"), n)
}

// nextOccurrence is the first weekday/hour:minute in loc that is still at
// least an hour away.
func nextOccurrence(now time.Time, loc *time.Location, day time.Weekday, hour, minute int) time.Time {
	local := now.In(loc)
	t := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	t = t.AddDate(0, 0, (int(day)-int(local.Weekday())+7)%7)
	if t.Before(now.Add(time.Hour)) {
		t = t.AddDate(0, 0, 7)
	}
	return t
}

// isLocalDatabase reports whether databaseURL points at this machine.
func isLocalDatabase(databaseURL string) (bool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return false, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	switch host := cfg.ConnConfig.Host; {
	case host == "localhost", host == "127.0.0.1", host == "::1", strings.HasPrefix(host, "/"):
		return true, nil
	default:
		return false, nil
	}
}

func seedDemo(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	loc, err := time.LoadLocation(demoTimezone)
	if err != nil {
		return err
	}

	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for _, v := range demoVenues {
			if _, err := tx.Exec(ctx, `
				INSERT INTO venues (id, name, address, city, lat, lng, timezone)
				VALUES ($1, $2, $3, 'bangalore', $4, $5, $6)
				ON CONFLICT (id) DO UPDATE SET name = $2, address = $3, city = 'bangalore', lat = $4, lng = $5, timezone = $6`,
				v.id, v.name, v.address, v.lat, v.lng, demoTimezone,
			); err != nil {
				return fmt.Errorf("venue %s: %w", v.id, err)
			}
			for n := 1; n <= v.courts; n++ {
				if _, err := tx.Exec(ctx, `
					INSERT INTO courts (id, venue_id, label) VALUES ($1, $2, $3)
					ON CONFLICT (id) DO UPDATE SET label = $3, active = true`,
					demoCourtID(v.id, n), v.id, fmt.Sprintf("Court %d", n),
				); err != nil {
					return fmt.Errorf("court: %w", err)
				}
			}
		}

		for _, e := range demoEvents {
			start := nextOccurrence(now, loc, e.weekday, e.hour, e.minute)
			if _, err := tx.Exec(ctx, `
				INSERT INTO events (id, venue_id, title, formats, starts_at, ends_at, registration_closes_at,
					fee_paise, capacity, max_matches, rating_min, rating_max, status)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
				ON CONFLICT (id) DO UPDATE SET venue_id = $2, title = $3, formats = $4, starts_at = $5, ends_at = $6,
					registration_closes_at = $7, fee_paise = $8, capacity = $9, max_matches = $10,
					rating_min = $11, rating_max = $12, status = $13, updated_at = now()`,
				e.id, e.venueID, e.title, e.formats, start, start.Add(time.Duration(e.hours)*time.Hour),
				start.Add(-demoRegistrationWindow), e.feeRupees*100, e.capacity, e.maxMatches,
				e.ratingMin, e.ratingMax, e.status,
			); err != nil {
				return fmt.Errorf("event %s: %w", e.id, err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM event_courts WHERE event_id = $1`, e.id); err != nil {
				return err
			}
			for _, n := range e.courts {
				if _, err := tx.Exec(ctx,
					`INSERT INTO event_courts (event_id, court_id, venue_id) VALUES ($1, $2, $3)`,
					e.id, demoCourtID(e.venueID, n), e.venueID,
				); err != nil {
					return fmt.Errorf("event court: %w", err)
				}
			}
		}
		return nil
	})
}
