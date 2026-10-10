package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

// eventsTestStore returns a PostgresStore on TEST_DATABASE_URL with empty
// events tables. Like the store contract test it TRUNCATES, so point it at a
// throwaway database.
func eventsTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if err := runMigrations(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := NewPostgresStore(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if _, err := store.pool.Exec(context.Background(),
		`TRUNCATE event_courts, events, courts, venues CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return store
}

// Monday 5 October 2026, 10:00 in Bangalore. With the demo data the
// published events then fall on Wed 7, Sat 10 and Sun 11 October.
var testNow = time.Date(2026, 10, 5, 10, 0, 0, 0, mustLoad(demoTimezone))

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func seededStore(t *testing.T, now time.Time) *PostgresStore {
	t.Helper()
	store := eventsTestStore(t)
	if err := seedDemo(context.Background(), store.pool, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return store
}

func eventIDs(events []Event) []string {
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	return ids
}

func assertIDs(t *testing.T, got []Event, want ...string) {
	t.Helper()
	ids := eventIDs(got)
	if len(ids) != len(want) {
		t.Fatalf("events = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("events = %v, want %v", ids, want)
		}
	}
}

func TestListEventsPublishedUpcomingInStartOrder(t *testing.T) {
	store := seededStore(t, testNow)
	ctx := context.Background()

	events, more, err := store.ListEvents(ctx, EventQuery{Now: testNow, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	// The draft is left out.
	assertIDs(t, events, "evt_demo_midweek_rally", "evt_demo_saturday_smash", "evt_demo_sunday_ladder")
	if more {
		t.Error("more = true on the only page")
	}

	smash := events[1]
	if smash.Venue.Name != "Koramangala Sports Arena" || smash.Venue.Timezone != demoTimezone {
		t.Errorf("venue = %+v", smash.Venue)
	}
	if smash.FeePaise != 5000 || smash.Currency != "INR" || smash.SpotsLeft != smash.Capacity {
		t.Errorf("fee/spots = %d %s %d/%d", smash.FeePaise, smash.Currency, smash.SpotsLeft, smash.Capacity)
	}
	if smash.MaxMatches != 20 {
		t.Errorf("maxMatches = %d", smash.MaxMatches)
	}
	if smash.RatingMin == nil || *smash.RatingMin != 1300 || smash.RatingMax == nil || *smash.RatingMax != 1700 {
		t.Errorf("rating band = %v–%v", smash.RatingMin, smash.RatingMax)
	}
	if len(smash.Formats) != 1 || smash.Formats[0] != FormatSingles || len(smash.SlotMinutes) != 1 || smash.SlotMinutes[FormatSingles] != 30 {
		t.Errorf("formats = %v, slots %v", smash.Formats, smash.SlotMinutes)
	}
	wantStart := time.Date(2026, 10, 10, 19, 30, 0, 0, mustLoad(demoTimezone))
	if !smash.StartsAt.Equal(wantStart) || smash.StartsAt.Location() != time.UTC {
		t.Errorf("startsAt = %v, want %v in UTC", smash.StartsAt, wantStart)
	}
	if !smash.RegistrationClosesAt.Equal(wantStart.Add(-2 * time.Hour)) {
		t.Errorf("registrationClosesAt = %v", smash.RegistrationClosesAt)
	}
	if smash.Courts != nil || smash.DistanceKm != nil {
		t.Errorf("list includes courts %v / distance %v", smash.Courts, smash.DistanceKm)
	}

	// Slot lengths are listed only for the formats on offer.
	ladder := events[2]
	if len(ladder.SlotMinutes) != 2 || ladder.SlotMinutes[FormatDoubles] != 40 || ladder.SlotMinutes[FormatMixed] != 40 {
		t.Errorf("ladder slots = %v", ladder.SlotMinutes)
	}
}

// Demo venues are about 3.6 km (HSR) and 5.1 km (Indiranagar) from
// Koramangala.
func TestListEventsNearAPoint(t *testing.T) {
	store := seededStore(t, testNow)
	ctx := context.Background()
	koramangala := &GeoPoint{Lat: 12.9352, Lng: 77.6245}

	events, _, err := store.ListEvents(ctx, EventQuery{Now: testNow, Near: koramangala, RadiusKm: 2, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, events, "evt_demo_saturday_smash")
	if d := events[0].DistanceKm; d == nil || *d != 0 {
		t.Errorf("distance at the venue = %v", d)
	}

	events, _, _ = store.ListEvents(ctx, EventQuery{Now: testNow, Near: koramangala, RadiusKm: 4, Limit: 10})
	assertIDs(t, events, "evt_demo_saturday_smash", "evt_demo_sunday_ladder")
	if d := events[1].DistanceKm; d == nil || *d < 3 || *d > 4 {
		t.Errorf("HSR distance = %v, want about 3.6", d)
	}

	events, _, _ = store.ListEvents(ctx, EventQuery{Now: testNow, Near: koramangala, RadiusKm: 6, Limit: 10})
	assertIDs(t, events, "evt_demo_midweek_rally", "evt_demo_saturday_smash", "evt_demo_sunday_ladder")

	// A venue that isn't on the map drops out of location searches only.
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO venues (id, name, city, timezone) VALUES ('ven_t_nomap', 'No Map Club', 'bangalore', 'Asia/Kolkata');
		INSERT INTO events (id, venue_id, title, formats, starts_at, ends_at, registration_closes_at, fee_paise, capacity, max_matches, status)
		VALUES ('evt_t_nomap', 'ven_t_nomap', 'Unmapped', '{singles}', '2026-10-09T12:00:00Z', '2026-10-09T14:00:00Z',
		        '2026-10-09T10:00:00Z', 5000, 8, 4, 'published');`); err != nil {
		t.Fatal(err)
	}
	events, _, _ = store.ListEvents(ctx, EventQuery{Now: testNow, Near: koramangala, RadiusKm: 100, Limit: 10})
	if len(events) != 3 {
		t.Errorf("location search = %v, want the 3 mapped events", eventIDs(events))
	}
	events, _, _ = store.ListEvents(ctx, EventQuery{Now: testNow, City: "bangalore", Limit: 10})
	if len(events) != 4 {
		t.Errorf("city search = %v, want all 4", eventIDs(events))
	}
}

func TestEventsTableRejectsBadRows(t *testing.T) {
	store := seededStore(t, testNow)
	insert := func(formats, closes string, maxMatches int) error {
		_, err := store.pool.Exec(context.Background(), `
			INSERT INTO events (id, venue_id, title, formats, starts_at, ends_at, registration_closes_at, fee_paise, capacity, max_matches)
			VALUES ('evt_t_bad', 'ven_demo_hsr', 'Bad', $1, '2026-10-09T12:00:00Z', '2026-10-09T14:00:00Z', $2, 5000, 8, $3)`,
			formats, closes, maxMatches)
		return err
	}
	for name, err := range map[string]error{
		"unknown format":         insert("{tennis}", "2026-10-09T10:00:00Z", 4),
		"no formats":             insert("{}", "2026-10-09T10:00:00Z", 4),
		"closes after the start": insert("{singles}", "2026-10-09T13:00:00Z", 4),
		"no matches":             insert("{singles}", "2026-10-09T10:00:00Z", 0),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestListEventsFiltersCityAndPastAndCancelled(t *testing.T) {
	store := seededStore(t, testNow)
	ctx := context.Background()

	events, _, err := store.ListEvents(ctx, EventQuery{Now: testNow, City: "mumbai", Limit: 10})
	if err != nil || len(events) != 0 {
		t.Fatalf("mumbai: %v %v", eventIDs(events), err)
	}

	// Once Wednesday's event has ended it drops off; one still running stays.
	thursday := time.Date(2026, 10, 8, 9, 0, 0, 0, mustLoad(demoTimezone))
	events, _, err = store.ListEvents(ctx, EventQuery{Now: thursday, City: "bangalore", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, events, "evt_demo_saturday_smash", "evt_demo_sunday_ladder")

	duringSmash := time.Date(2026, 10, 10, 20, 0, 0, 0, mustLoad(demoTimezone))
	events, _, _ = store.ListEvents(ctx, EventQuery{Now: duringSmash, Limit: 10})
	assertIDs(t, events, "evt_demo_saturday_smash", "evt_demo_sunday_ladder")

	if _, err := store.pool.Exec(ctx, `UPDATE events SET status = 'cancelled' WHERE id = 'evt_demo_sunday_ladder'`); err != nil {
		t.Fatal(err)
	}
	events, _, _ = store.ListEvents(ctx, EventQuery{Now: thursday, Limit: 10})
	assertIDs(t, events, "evt_demo_saturday_smash")
}

func TestListEventsPaging(t *testing.T) {
	store := seededStore(t, testNow)
	ctx := context.Background()

	first, more, err := store.ListEvents(ctx, EventQuery{Now: testNow, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, first, "evt_demo_midweek_rally", "evt_demo_saturday_smash")
	if !more {
		t.Fatal("more = false with a third event left")
	}

	// Through the opaque cursor, as a client would.
	cursor, err := decodeCursor(encodeCursor(first[1]))
	if err != nil {
		t.Fatal(err)
	}
	second, more, err := store.ListEvents(ctx, EventQuery{Now: testNow, Limit: 2, After: cursor})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, second, "evt_demo_sunday_ladder")
	if more {
		t.Error("more = true on the last page")
	}
}

func TestGetEvent(t *testing.T) {
	store := seededStore(t, testNow)
	ctx := context.Background()

	e, err := store.GetEvent(ctx, "evt_demo_midweek_rally")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Courts) != 3 || e.Courts[0].Label != "Court 1" || e.Courts[2].Label != "Court 3" {
		t.Errorf("courts = %+v", e.Courts)
	}

	// Any status, drafts included: hiding them is the handler's job.
	if d, err := store.GetEvent(ctx, "evt_demo_draft_open"); err != nil || d.Status != EventDraft {
		t.Errorf("draft = %+v, %v", d, err)
	}

	if _, err := store.GetEvent(ctx, "evt_nope"); !errors.Is(err, ErrNoSuchEvent) {
		t.Errorf("unknown id: err = %v", err)
	}
}

func TestEventCourtMustBeAtTheEventsVenue(t *testing.T) {
	store := seededStore(t, testNow)
	// An HSR court on a Koramangala event.
	_, err := store.pool.Exec(context.Background(),
		`INSERT INTO event_courts (event_id, court_id, venue_id) VALUES ('evt_demo_saturday_smash', 'crt_demo_hsr_1', 'ven_demo_koramangala')`)
	if err == nil {
		t.Fatal("court from another venue was accepted")
	}
}

func TestSeedDemoIsRepeatable(t *testing.T) {
	store := seededStore(t, testNow)
	ctx := context.Background()
	if err := seedDemo(ctx, store.pool, testNow.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	var venues, courts, events, links int
	err := store.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM venues), (SELECT count(*) FROM courts),
		(SELECT count(*) FROM events), (SELECT count(*) FROM event_courts)`).Scan(&venues, &courts, &events, &links)
	if err != nil {
		t.Fatal(err)
	}
	if venues != 3 || courts != 9 || events != 4 || links != 11 {
		t.Errorf("rows = %d venues, %d courts, %d events, %d links", venues, courts, events, links)
	}
}

func TestEventsEndpoints(t *testing.T) {
	store := seededStore(t, time.Now())
	srv := httptest.NewServer(NewAPI(NewMemoryStore(), oauth.NewRegistry(), nil, false).WithEvents(store).Routes())
	t.Cleanup(srv.Close)

	get := func(path string, into any) int {
		t.Helper()
		// No session cookie: the events endpoints are public.
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if into != nil && res.StatusCode == http.StatusOK {
			if err := json.NewDecoder(res.Body).Decode(into); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode
	}

	var page struct {
		Events     []Event `json:"events"`
		NextCursor *string `json:"nextCursor"`
	}
	if code := get("/api/events?city=Bangalore&limit=2", &page); code != http.StatusOK {
		t.Fatalf("list: %d", code)
	}
	if len(page.Events) != 2 || page.NextCursor == nil {
		t.Fatalf("first page = %v, cursor %v", eventIDs(page.Events), page.NextCursor)
	}
	var rest struct {
		Events     []Event `json:"events"`
		NextCursor *string `json:"nextCursor"`
	}
	get("/api/events?limit=2&cursor="+*page.NextCursor, &rest)
	if len(rest.Events) != 1 || rest.NextCursor != nil {
		t.Errorf("second page = %v, cursor %v", eventIDs(rest.Events), rest.NextCursor)
	}

	var near struct {
		Events []Event `json:"events"`
	}
	if code := get("/api/events?lat=12.9352&lng=77.6245&radiusKm=4", &near); code != http.StatusOK {
		t.Fatalf("near: %d", code)
	}
	if len(near.Events) != 2 || near.Events[0].DistanceKm == nil {
		t.Errorf("near = %v", eventIDs(near.Events))
	}
	// Without a location, distanceKm isn't in the JSON at all.
	if res, err := http.Get(srv.URL + "/api/events"); err != nil {
		t.Fatal(err)
	} else {
		var raw struct {
			Events []map[string]any `json:"events"`
		}
		_ = json.NewDecoder(res.Body).Decode(&raw)
		_ = res.Body.Close()
		if _, ok := raw.Events[0]["distanceKm"]; ok {
			t.Error("distanceKm present without a location")
		}
	}

	var one struct {
		Event Event `json:"event"`
	}
	if code := get("/api/events/evt_demo_saturday_smash", &one); code != http.StatusOK || len(one.Event.Courts) != 4 {
		t.Errorf("get: %d, courts %v", code, one.Event.Courts)
	}

	for path, want := range map[string]int{
		"/api/events/evt_demo_draft_open":        http.StatusNotFound,
		"/api/events/evt_nope":                   http.StatusNotFound,
		"/api/events?cursor=bm90LWEtY3Vy":        http.StatusBadRequest,
		"/api/events?limit=0":                    http.StatusBadRequest,
		"/api/events?limit=51":                   http.StatusBadRequest,
		"/api/events?city=new%20york":            http.StatusBadRequest,
		"/api/events?lat=12.9":                   http.StatusBadRequest,
		"/api/events?lat=NaN&lng=77":             http.StatusBadRequest,
		"/api/events?lat=91&lng=77":              http.StatusBadRequest,
		"/api/events?radiusKm=5":                 http.StatusBadRequest,
		"/api/events?lat=12&lng=77&radiusKm=0":   http.StatusBadRequest,
		"/api/events?lat=12&lng=77&radiusKm=101": http.StatusBadRequest,
	} {
		if code := get(path, nil); code != want {
			t.Errorf("GET %s = %d, want %d", path, code, want)
		}
	}
}

func TestNextOccurrence(t *testing.T) {
	loc := mustLoad(demoTimezone)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, loc) }

	for _, tc := range []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"later this week", at(5, 10, 0), at(10, 19, 30)},
		{"later today", at(10, 9, 0), at(10, 19, 30)},
		{"less than an hour away: next week", at(10, 19, 0), at(17, 19, 30)},
		{"already started: next week", at(10, 20, 0), at(17, 19, 30)},
	} {
		if got := nextOccurrence(tc.now, loc, time.Saturday, 19, 30); !got.Equal(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsLocalDatabase(t *testing.T) {
	for url, want := range map[string]bool{
		"postgres://u:p@localhost:5432/db":                   true,
		"postgres://u:p@127.0.0.1/db":                        true,
		"host=/tmp dbname=db":                                true,
		"postgres://u:p@ep-cool-name.aws.neon.tech/db":       false,
		"postgres://u:p@db.internal.example.com:5432/db?x=1": false,
	} {
		if got, err := isLocalDatabase(url); err != nil || got != want {
			t.Errorf("isLocalDatabase(%q) = %v, %v; want %v", url, got, err, want)
		}
	}
}

func TestCursorRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "!!", "bm8tc2VwYXJhdG9y", "MjAyNi0xMC0xMHxldnQ"} {
		if _, err := decodeCursor(s); err == nil {
			t.Errorf("decodeCursor(%q) accepted", s)
		}
	}
}
