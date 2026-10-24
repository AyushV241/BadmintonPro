package main

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Demo data for `go run . -seed-demo`: three Bangalore venues with courts and
// a few events in the coming week, and demo players with ratings and paid
// entries in "Midweek Rally Night" so the schedule preview has a pool to
// work on. Fixed IDs make it safe to re-run: each run rewrites the same rows
// and moves the events to the next matching days.
//
// An event whose schedule has been published (it has matches) is left
// exactly as it is, entries included: moving it would strand its matches,
// and matches are never deleted. To start the demo afresh, reset the local
// database.

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
		[]string{FormatSingles, FormatDoubles}, time.Wednesday, 20, 0, 2, 50, 24, 12, nil, elo(1800), []int{1, 2, 3}},
	// Never listed: shows that drafts stay hidden.
	{"evt_demo_draft_open", "ven_demo_koramangala", "Monsoon Open (draft)", EventDraft,
		[]string{FormatSingles, FormatDoubles, FormatMixed}, time.Friday, 18, 0, 4, 50, 32, 24, nil, nil, []int{1, 2}},
}

// demoPlayer is a demo account. Demo accounts have a complete profile but no
// sign-in identity, so nobody can sign in as one.
type demoPlayer struct {
	id, username, name string
	singles, doubles   int // ratings; mixed is doubles - 20
}

var demoPlayers = []demoPlayer{
	{"usr_demo_rohan", "demo_rohan", "Rohan Mehta", 1750, 1700},
	{"usr_demo_asha", "demo_asha", "Asha Rao", 1680, 1650},
	{"usr_demo_vikram", "demo_vikram", "Vikram Shetty", 1620, 1600},
	{"usr_demo_priya", "demo_priya", "Priya Nair", 1580, 1560},
	{"usr_demo_arjun", "demo_arjun", "Arjun Iyer", 1540, 1500},
	{"usr_demo_meera", "demo_meera", "Meera Pillai", 1500, 1520},
	{"usr_demo_kabir", "demo_kabir", "Kabir Singh", 1460, 1440},
	{"usr_demo_sneha", "demo_sneha", "Sneha Kulkarni", 1420, 1450},
	{"usr_demo_dev", "demo_dev", "Dev Malhotra", 1380, 1400},
	{"usr_demo_ananya", "demo_ananya", "Ananya Bose", 1350, 1330},
	{"usr_demo_ishaan", "demo_ishaan", "Ishaan Gupta", 1300, 1320},
	{"usr_demo_kavya", "demo_kavya", "Kavya Reddy", 1260, 1280},
	{"usr_demo_rahul", "demo_rahul", "Rahul Menon", 1220, 1200},
	{"usr_demo_nisha", "demo_nisha", "Nisha Joshi", 1180, 1210},
	{"usr_demo_aditya", "demo_aditya", "Aditya Rao", 1140, 1150},
	{"usr_demo_tara", "demo_tara", "Tara D'Souza", 1100, 1120},
}

// demoJudges judge "Midweek Rally Night". They have no ratings rows.
var demoJudges = []demoPlayer{
	{id: "usr_demo_sameer", username: "demo_sameer", name: "Sameer Khan"},
	{id: "usr_demo_lata", username: "demo_lata", name: "Lata Krishnan"},
}

// demoJudgePhones are the numbers the demo judges are registered with, so
// "Log in as judge" works on them (OTP_PROVIDER=console prints the code).
var demoJudgePhones = map[string]string{
	"usr_demo_sameer": "+919999900001",
	"usr_demo_lata":   "+919999900002",
}

// demoEntry is a confirmed open entry in demoEntryEvent. They join in this
// order, a minute apart.
type demoEntry struct {
	id, userID string
	formats    []string
	partnerID  string
}

const demoEntryEvent = "evt_demo_midweek_rally"

var (
	singlesOnly = []string{FormatSingles}
	doublesOnly = []string{FormatDoubles}
	either      = []string{FormatSingles, FormatDoubles}

	// Singles-only, doubles-only and flexible entries; two partner pairs
	// (one mutual, one named by one side only); and Dev with two entries.
	demoEntries = []demoEntry{
		{"ent_demo_rohan", "usr_demo_rohan", singlesOnly, ""},
		{"ent_demo_vikram", "usr_demo_vikram", singlesOnly, ""},
		{"ent_demo_arjun", "usr_demo_arjun", singlesOnly, ""},
		{"ent_demo_kabir", "usr_demo_kabir", singlesOnly, ""},
		{"ent_demo_dev_1", "usr_demo_dev", singlesOnly, ""},
		{"ent_demo_dev_2", "usr_demo_dev", singlesOnly, ""},
		{"ent_demo_aditya", "usr_demo_aditya", singlesOnly, ""},
		{"ent_demo_priya", "usr_demo_priya", doublesOnly, "usr_demo_meera"},
		{"ent_demo_meera", "usr_demo_meera", doublesOnly, "usr_demo_priya"},
		{"ent_demo_ishaan", "usr_demo_ishaan", doublesOnly, "usr_demo_kavya"},
		{"ent_demo_kavya", "usr_demo_kavya", doublesOnly, ""},
		{"ent_demo_sneha", "usr_demo_sneha", doublesOnly, ""},
		{"ent_demo_ananya", "usr_demo_ananya", doublesOnly, ""},
		{"ent_demo_asha", "usr_demo_asha", either, ""},
		{"ent_demo_rahul", "usr_demo_rahul", either, ""},
		{"ent_demo_nisha", "usr_demo_nisha", either, ""},
		{"ent_demo_tara", "usr_demo_tara", either, ""},
	}
)

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

		for _, p := range append(slices.Clone(demoPlayers), demoJudges...) {
			if _, err := tx.Exec(ctx, `
				INSERT INTO users (id, name, username) VALUES ($1, $2, $3)
				ON CONFLICT (id) DO UPDATE SET name = $2, username = $3`,
				p.id, p.name, p.username,
			); err != nil {
				return fmt.Errorf("user %s: %w", p.id, err)
			}
		}
		// The demo judges are phone accounts registered as judges.
		for _, j := range demoJudges {
			phone := demoJudgePhones[j.id]
			for _, q := range []struct {
				sql  string
				args []any
			}{
				{`UPDATE users SET phone = $2, phone_verified = true, sign_in_method = 'phone' WHERE id = $1`,
					[]any{j.id, phone}},
				{`INSERT INTO user_identities (provider, subject, user_id) VALUES ('phone', $2, $1) ON CONFLICT DO NOTHING`,
					[]any{j.id, phone}},
				{`INSERT INTO judges (user_id, name, phone) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
					[]any{j.id, j.name, phone}},
			} {
				if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
					return fmt.Errorf("judge %s: %w", j.id, err)
				}
			}
		}
		for _, p := range demoPlayers {
			for discipline, rating := range map[string]int{
				FormatSingles: p.singles, FormatDoubles: p.doubles, FormatMixed: p.doubles - 20,
			} {
				if _, err := tx.Exec(ctx, `
					INSERT INTO player_ratings (user_id, discipline, rating, matches_played, provisional)
					VALUES ($1, $2, $3, 12, false)
					ON CONFLICT (user_id, discipline) DO UPDATE SET rating = $3, matches_played = 12,
						provisional = false, updated_at = now()`,
					p.id, discipline, rating,
				); err != nil {
					return fmt.Errorf("rating %s: %w", p.id, err)
				}
			}
		}

		for _, e := range demoEvents {
			var scheduled bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM matches WHERE event_id = $1)`, e.id).Scan(&scheduled); err != nil {
				return fmt.Errorf("event %s: %w", e.id, err)
			}
			if scheduled {
				log.Printf("demo event %s has a published schedule; leaving it and its entries as they are", e.id)
				continue
			}
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
			if e.id == demoEntryEvent {
				if err := seedDemoPool(ctx, tx, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// seedDemoPool adds demoEntryEvent's judges and resets its demo entries to
// confirmed, ready for the matcher. Only called while the event has no
// matches.
func seedDemoPool(ctx context.Context, tx pgx.Tx, now time.Time) error {
	for _, j := range demoJudges {
		if _, err := tx.Exec(ctx,
			`INSERT INTO event_judges (event_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			demoEntryEvent, j.id,
		); err != nil {
			return fmt.Errorf("judge %s: %w", j.id, err)
		}
	}
	for i, en := range demoEntries {
		var partner any // nil: no partner
		if en.partnerID != "" {
			partner = en.partnerID
		}
		// created_at is only set on the first run, so join order stays put.
		if _, err := tx.Exec(ctx, `
			INSERT INTO entries (id, event_id, user_id, kind, formats, partner_user_id, source, status, created_at)
			VALUES ($1, $2, $3, 'open', $4, $5, 'online', 'confirmed', $6)
			ON CONFLICT (id) DO UPDATE SET formats = $4, partner_user_id = $5, status = 'confirmed',
				hold_expires_at = NULL, match_id = NULL, standby_reason = NULL, updated_at = now()`,
			en.id, demoEntryEvent, en.userID, en.formats, partner,
			now.Add(-time.Duration(len(demoEntries)-i)*time.Minute),
		); err != nil {
			return fmt.Errorf("entry %s: %w", en.id, err)
		}
	}
	return nil
}
