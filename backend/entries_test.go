package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

// --- validation (no database) ---

func TestValidateEntry(t *testing.T) {
	formats, partner, err := validateEntry(entryRequest{Formats: []string{"mixed", "singles", "mixed"}, PartnerUsername: "  @Asha "})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(formats, ",") != "singles,mixed" || partner != "asha" {
		t.Errorf("formats %v, partner %q", formats, partner)
	}

	for name, req := range map[string]entryRequest{
		"no formats":          {},
		"unknown format":      {Formats: []string{"tennis"}},
		"partner for singles": {Formats: []string{"singles"}, PartnerUsername: "asha"},
	} {
		if _, _, err := validateEntry(req); !errors.Is(err, errValidation) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
}

func TestInBand(t *testing.T) {
	for _, tc := range []struct {
		min, max *int
		want     bool
	}{
		{nil, nil, true},
		{intp(1200), nil, true}, // inclusive
		{nil, intp(1200), true},
		{intp(1300), intp(1700), false},
		{nil, intp(1100), false},
	} {
		if got := inBand(1200, tc.min, tc.max); got != tc.want {
			t.Errorf("inBand(1200, %v, %v) = %v", tc.min, tc.max, got)
		}
	}
}

// --- entries against Postgres ---

// entriesTestServer returns a store with no users, events or entries, and a
// server with every route.
func entriesTestServer(t *testing.T) (*PostgresStore, *httptest.Server) {
	t.Helper()
	store := eventsTestStore(t)
	if _, err := store.pool.Exec(context.Background(),
		`TRUNCATE users, sessions, user_identities, event_judges, entries CASCADE`); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewAPI(store, oauth.NewRegistry(), nil, false).
		WithEvents(store).WithAdmin(store).WithEntries(store).WithMatches(store).WithUserSearch(store).Routes())
	t.Cleanup(srv.Close)
	return store, srv
}

// player signs in a new phone account n with a complete profile.
func player(t *testing.T, store *PostgresStore, srv *httptest.Server, n int, username string) (adminClient, User) {
	t.Helper()
	cookie, user := signIn(t, store, fmt.Sprintf("+91980000%04d", n))
	user, err := store.UpdateProfile(context.Background(), user.ID, ProfileUpdate{Name: "Player " + username, Username: username})
	if err != nil {
		t.Fatal(err)
	}
	return adminClient{t: t, srv: srv, cookie: cookie}, user
}

// testEvent is an event inserted straight into the table. Zero fields take
// the defaults in insertEvent.
type testEvent struct {
	status               string
	formats              []string
	capacity, maxMatches int
	ratingMin, ratingMax *int
	// closesIn is from now; negative means joining has closed.
	closesIn time.Duration
}

// insertEvent adds an event three days from now, open for joining for a day
// unless e says otherwise.
func insertEvent(t *testing.T, store *PostgresStore, id string, e testEvent) {
	t.Helper()
	if e.status == "" {
		e.status = EventPublished
	}
	if e.formats == nil {
		e.formats = []string{FormatSingles, FormatDoubles}
	}
	if e.capacity == 0 {
		e.capacity = 16
	}
	if e.maxMatches == 0 {
		e.maxMatches = 8
	}
	if e.closesIn == 0 {
		e.closesIn = 24 * time.Hour
	}
	now := time.Now()
	start := now.Add(72 * time.Hour)
	if _, err := store.pool.Exec(context.Background(), `
		INSERT INTO venues (id, name, city, timezone) VALUES ('ven_t_entries', 'Entry Club', 'bangalore', 'Asia/Kolkata')
		ON CONFLICT (id) DO NOTHING;`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(context.Background(), `
		INSERT INTO events (id, venue_id, title, formats, starts_at, ends_at, registration_closes_at,
			fee_paise, capacity, max_matches, rating_min, rating_max, status)
		VALUES ($1, 'ven_t_entries', $2, $3, $4, $5, $6, 5000, $7, $8, $9, $10, $11)`,
		id, "Event "+id, e.formats, start, start.Add(2*time.Hour), now.Add(e.closesIn),
		e.capacity, e.maxMatches, e.ratingMin, e.ratingMax, e.status); err != nil {
		t.Fatal(err)
	}
}

func join(c adminClient, eventID string, body any, out *entryResponse) int {
	c.t.Helper()
	if out == nil { // a nil *entryResponse in an any isn't a nil any
		return c.do("POST", "/api/events/"+eventID+"/entries", body, nil)
	}
	return c.do("POST", "/api/events/"+eventID+"/entries", body, out)
}

var singles = map[string]any{"formats": []string{"singles"}}

func spotsLeft(t *testing.T, c adminClient, eventID string) int {
	t.Helper()
	var got eventResponse
	if code := c.do("GET", "/api/events/"+eventID, nil, &got); code != http.StatusOK {
		t.Fatalf("get event: %d", code)
	}
	return got.Event.SpotsLeft
}

func TestJoinListAndCancel(t *testing.T) {
	store, srv := entriesTestServer(t)
	insertEvent(t, store, "evt_t_join", testEvent{capacity: 10})
	asha, ashaUser := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")
	anon := adminClient{t: t, srv: srv}

	for _, route := range [][2]string{
		{"POST", "/api/events/evt_t_join/entries"}, {"GET", "/api/me/entries"}, {"DELETE", "/api/me/entries/ent_x"},
	} {
		if code := anon.do(route[0], route[1], nil, nil); code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want 401", route[0], route[1], code)
		}
	}

	before := time.Now()
	var created entryResponse
	if code := join(asha, "evt_t_join", map[string]any{"formats": []string{"doubles", "singles"}}, &created); code != http.StatusCreated {
		t.Fatalf("join: %d", code)
	}
	en := created.Entry
	if !strings.HasPrefix(en.ID, "ent_") || len(en.ID) != len("ent_")+24 {
		t.Errorf("id = %q", en.ID)
	}
	if en.Kind != EntryOpen || en.Source != SourceOnline || en.Status != EntryPendingPayment || en.HoldExpired || en.Partner != nil {
		t.Errorf("entry = %+v", en)
	}
	if strings.Join(en.Formats, ",") != "singles,doubles" {
		t.Errorf("formats = %v", en.Formats)
	}
	if h := en.HoldExpiresAt; h == nil || h.Before(before.Add(entryHold)) || h.After(time.Now().Add(entryHold)) || h.Location() != time.UTC {
		t.Errorf("holdExpiresAt = %v, want 10 minutes from now in UTC", h)
	}
	if en.Event.ID != "evt_t_join" || en.Event.Title != "Event evt_t_join" || en.Event.FeePaise != 5000 || en.Event.Venue.Name != "Entry Club" {
		t.Errorf("event = %+v", en.Event)
	}

	// A held place counts, in the detail and the list.
	if n := spotsLeft(t, anon, "evt_t_join"); n != 9 {
		t.Errorf("spotsLeft = %d, want 9", n)
	}
	var list eventsResponse
	anon.do("GET", "/api/events", nil, &list)
	if len(list.Events) != 1 || list.Events[0].SpotsLeft != 9 {
		t.Errorf("list = %+v", list.Events)
	}

	// Newest first, and only the caller's.
	var second entryResponse
	join(asha, "evt_t_join", singles, &second)
	join(ravi, "evt_t_join", singles, nil)
	var mine entriesResponse
	if code := asha.do("GET", "/api/me/entries", nil, &mine); code != http.StatusOK {
		t.Fatalf("list mine: %d", code)
	}
	if len(mine.Entries) != 2 || mine.Entries[0].ID != second.Entry.ID || mine.Entries[1].ID != en.ID {
		t.Errorf("mine = %+v", mine.Entries)
	}

	// Cancelling keeps the row and frees the place.
	if code := ravi.do("DELETE", "/api/me/entries/"+en.ID, nil, nil); code != http.StatusNotFound {
		t.Errorf("cancel someone else's: %d", code)
	}
	var cancelled entryResponse
	if code := asha.do("DELETE", "/api/me/entries/"+en.ID, nil, &cancelled); code != http.StatusOK || cancelled.Entry.Status != EntryCancelled {
		t.Fatalf("cancel: %d %+v", code, cancelled.Entry)
	}
	if code := asha.do("DELETE", "/api/me/entries/"+en.ID, nil, nil); code != http.StatusConflict {
		t.Errorf("cancel twice: %d", code)
	}
	if code := asha.do("DELETE", "/api/me/entries/ent_nope", nil, nil); code != http.StatusNotFound {
		t.Errorf("cancel unknown: %d", code)
	}
	if n := spotsLeft(t, anon, "evt_t_join"); n != 8 {
		t.Errorf("spotsLeft after cancel = %d, want 8", n)
	}
	var rows int
	if err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM entries WHERE user_id = $1`, ashaUser.ID).Scan(&rows); err != nil || rows != 2 {
		t.Errorf("asha's rows = %d, %v; cancelling must not delete", rows, err)
	}

	// A paid entry can't be cancelled here yet.
	if _, err := store.pool.Exec(context.Background(), `UPDATE entries SET status = 'confirmed' WHERE id = $1`, second.Entry.ID); err != nil {
		t.Fatal(err)
	}
	if code := asha.do("DELETE", "/api/me/entries/"+second.Entry.ID, nil, nil); code != http.StatusConflict {
		t.Errorf("cancel confirmed: %d", code)
	}
}

func TestJoinRejects(t *testing.T) {
	store, srv := entriesTestServer(t)
	insertEvent(t, store, "evt_t_singles", testEvent{formats: []string{FormatSingles}})
	insertEvent(t, store, "evt_t_draft", testEvent{status: EventDraft})
	insertEvent(t, store, "evt_t_cancelled", testEvent{status: EventCancelled})
	insertEvent(t, store, "evt_t_closed", testEvent{closesIn: -time.Minute})
	asha, _ := player(t, store, srv, 1, "asha")

	for name, tc := range map[string]struct {
		event string
		body  any
		want  int
	}{
		"unknown field":        {"evt_t_singles", `{"formats":["singles"],"paid":true}`, http.StatusBadRequest},
		"no formats":           {"evt_t_singles", map[string]any{"formats": []string{}}, http.StatusBadRequest},
		"format not offered":   {"evt_t_singles", map[string]any{"formats": []string{"singles", "mixed"}}, http.StatusBadRequest},
		"unknown event":        {"evt_nope", singles, http.StatusNotFound},
		"draft":                {"evt_t_draft", singles, http.StatusNotFound},
		"cancelled":            {"evt_t_cancelled", singles, http.StatusConflict},
		"registration closed":  {"evt_t_closed", singles, http.StatusConflict},
		"partner with singles": {"evt_t_singles", map[string]any{"formats": []string{"singles"}, "partnerUsername": "ravi"}, http.StatusBadRequest},
	} {
		if code := join(asha, tc.event, tc.body, nil); code != tc.want {
			t.Errorf("%s: %d, want %d", name, code, tc.want)
		}
	}

	// An account that hasn't finished the profile step can't join.
	cookie, _ := signIn(t, store, "+919800009999")
	if code := join(adminClient{t: t, srv: srv, cookie: cookie}, "evt_t_singles", singles, nil); code != http.StatusForbidden {
		t.Errorf("incomplete profile: %d", code)
	}

	// The store applies its own now to the cutoff too.
	_, err := store.CreateEntry(context.Background(), EntryInput{
		EventID: "evt_t_singles", UserID: "usr_x", Formats: []string{FormatSingles}, Now: time.Now().Add(25 * time.Hour),
	})
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Errorf("after the cutoff: err = %v", err)
	}
}

func TestJoinCapacityAndMatchLimit(t *testing.T) {
	store, srv := entriesTestServer(t)
	insertEvent(t, store, "evt_t_cap", testEvent{capacity: 3, maxMatches: 8})
	// 1 match is room for 2 open entries, well under the capacity.
	insertEvent(t, store, "evt_t_matches", testEvent{capacity: 10, maxMatches: 1})

	var players []adminClient
	for i := range 4 {
		c, _ := player(t, store, srv, i+1, fmt.Sprintf("player%d", i+1))
		players = append(players, c)
	}

	for i := range 3 {
		if code := join(players[i], "evt_t_cap", singles, nil); code != http.StatusCreated {
			t.Fatalf("join %d: %d", i+1, code)
		}
	}
	if code := join(players[3], "evt_t_cap", singles, nil); code != http.StatusConflict {
		t.Errorf("over capacity: %d", code)
	}
	if n := spotsLeft(t, players[0], "evt_t_cap"); n != 0 {
		t.Errorf("spotsLeft when full = %d", n)
	}

	if n := spotsLeft(t, players[0], "evt_t_matches"); n != 2 {
		t.Errorf("spotsLeft capped by matches = %d, want 2", n)
	}
	for i := range 2 {
		if code := join(players[i], "evt_t_matches", singles, nil); code != http.StatusCreated {
			t.Fatalf("join %d: %d", i+1, code)
		}
	}
	_, err := store.CreateEntry(context.Background(), EntryInput{
		EventID: "evt_t_matches", UserID: userID(t, store, "player3"), Formats: []string{FormatSingles}, Now: time.Now(),
	})
	if !errors.Is(err, ErrNoMatchesLeft) {
		t.Errorf("over max_matches: err = %v", err)
	}
	if n := spotsLeft(t, players[0], "evt_t_matches"); n != 0 {
		t.Errorf("spotsLeft with no matches left = %d", n)
	}
}

func userID(t *testing.T, store *PostgresStore, username string) string {
	t.Helper()
	var id string
	if err := store.pool.QueryRow(context.Background(), `SELECT id FROM users WHERE username = $1`, username).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestJoinEntryLimitPerPlayer(t *testing.T) {
	store, srv := entriesTestServer(t)
	insertEvent(t, store, "evt_t_limit", testEvent{})
	insertEvent(t, store, "evt_t_other", testEvent{})
	asha, _ := player(t, store, srv, 1, "asha")

	var first entryResponse
	join(asha, "evt_t_limit", singles, &first)
	for i := 2; i <= maxEntriesPerPlayer; i++ {
		if code := join(asha, "evt_t_limit", singles, nil); code != http.StatusCreated {
			t.Fatalf("entry %d: %d", i, code)
		}
	}
	if code := join(asha, "evt_t_limit", singles, nil); code != http.StatusConflict {
		t.Errorf("fourth entry: %d", code)
	}
	// The limit is per event, and a cancelled entry stops counting.
	if code := join(asha, "evt_t_other", singles, nil); code != http.StatusCreated {
		t.Errorf("another event: %d", code)
	}
	asha.do("DELETE", "/api/me/entries/"+first.Entry.ID, nil, nil)
	if code := join(asha, "evt_t_limit", singles, nil); code != http.StatusCreated {
		t.Errorf("after cancelling one: %d", code)
	}
}

func TestExpiredHoldDoesNotCount(t *testing.T) {
	store, srv := entriesTestServer(t)
	insertEvent(t, store, "evt_t_hold", testEvent{capacity: 1})
	asha, ashaUser := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")
	ctx := context.Background()

	// Asha joined 11 minutes ago and never paid.
	stale, err := store.CreateEntry(ctx, EntryInput{
		EventID: "evt_t_hold", UserID: ashaUser.ID, Formats: []string{FormatSingles}, Now: time.Now().Add(-11 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := spotsLeft(t, ravi, "evt_t_hold"); n != 1 {
		t.Errorf("spotsLeft with an expired hold = %d, want 1", n)
	}
	if code := join(ravi, "evt_t_hold", singles, nil); code != http.StatusCreated {
		t.Fatalf("join over an expired hold: %d", code)
	}

	// The expired entry is still listed, as pending_payment with its hold run
	// out, and can be tidied away.
	var mine entriesResponse
	asha.do("GET", "/api/me/entries", nil, &mine)
	if len(mine.Entries) != 1 || mine.Entries[0].Status != EntryPendingPayment || !mine.Entries[0].HoldExpired {
		t.Errorf("mine = %+v", mine.Entries)
	}
	if code := asha.do("DELETE", "/api/me/entries/"+stale.ID, nil, nil); code != http.StatusOK {
		t.Errorf("cancel expired: %d", code)
	}

	// A live hold does count: ten minutes on, Ravi's still holds the place.
	_, err = store.CreateEntry(ctx, EntryInput{
		EventID: "evt_t_hold", UserID: ashaUser.ID, Formats: []string{FormatSingles}, Now: time.Now().Add(9 * time.Minute),
	})
	if !errors.Is(err, ErrEventFull) {
		t.Errorf("9 minutes on: err = %v, want full", err)
	}
	if _, err := store.CreateEntry(ctx, EntryInput{
		EventID: "evt_t_hold", UserID: ashaUser.ID, Formats: []string{FormatSingles}, Now: time.Now().Add(11 * time.Minute),
	}); err != nil {
		t.Errorf("11 minutes on: %v", err)
	}
}

func TestJoinRatingBand(t *testing.T) {
	store, srv := entriesTestServer(t)
	// Without a player_ratings row, everyone is at 1200.
	insertEvent(t, store, "evt_t_high", testEvent{ratingMin: intp(1300), ratingMax: intp(1700)})
	insertEvent(t, store, "evt_t_low", testEvent{ratingMax: intp(1100)})
	insertEvent(t, store, "evt_t_edge", testEvent{ratingMin: intp(1200), ratingMax: intp(1200)})
	asha, _ := player(t, store, srv, 1, "asha")

	for event, want := range map[string]int{
		"evt_t_high": http.StatusConflict,
		"evt_t_low":  http.StatusConflict,
		"evt_t_edge": http.StatusCreated,
	} {
		if code := join(asha, event, singles, nil); code != want {
			t.Errorf("%s: %d, want %d", event, code, want)
		}
	}
}

func TestJoinWithPartner(t *testing.T) {
	store, srv := entriesTestServer(t)
	insertEvent(t, store, "evt_t_pairs", testEvent{formats: []string{FormatDoubles, FormatMixed}})
	asha, _ := player(t, store, srv, 1, "asha")
	raviClient, ravi := player(t, store, srv, 2, "ravi")
	_, noName := player(t, store, srv, 3, "noname")
	if _, err := store.pool.Exec(context.Background(), `UPDATE users SET name = '' WHERE id = $1`, noName.ID); err != nil {
		t.Fatal(err)
	}
	withRavi := func(format string) map[string]any {
		return map[string]any{"formats": []string{format}, "partnerUsername": "ravi"}
	}

	// A partner pays for their own entry, so they must have joined first,
	// for the same team format.
	if code := join(asha, "evt_t_pairs", withRavi("doubles"), nil); code != http.StatusBadRequest {
		t.Errorf("partner without an entry: %d", code)
	}
	if code := join(raviClient, "evt_t_pairs", map[string]any{"formats": []string{"doubles"}}, nil); code != http.StatusCreated {
		t.Fatalf("ravi joins doubles: %d", code)
	}
	if code := join(asha, "evt_t_pairs", withRavi("mixed"), nil); code != http.StatusBadRequest {
		t.Errorf("partner joined for doubles only, asked for mixed: %d", code)
	}
	if code := join(raviClient, "evt_t_pairs", map[string]any{"formats": []string{"mixed"}}, nil); code != http.StatusCreated {
		t.Fatalf("ravi joins mixed: %d", code)
	}
	for name, tc := range map[string]struct {
		format, partner string
		want            int
	}{
		"unknown":         {"doubles", "nobody", http.StatusNotFound},
		"unknown with @":  {"doubles", "@nobody", http.StatusNotFound},
		"self":            {"doubles", "@ASHA", http.StatusBadRequest},
		"incomplete":      {"doubles", "noname", http.StatusBadRequest},
		"blank is no-one": {"doubles", "  ", http.StatusCreated},
		"doubles partner": {"doubles", "@Ravi", http.StatusCreated},
		"mixed partner":   {"mixed", "ravi", http.StatusCreated},
	} {
		var got entryResponse
		code := join(asha, "evt_t_pairs", map[string]any{"formats": []string{tc.format}, "partnerUsername": tc.partner}, &got)
		if code != tc.want {
			t.Errorf("%s: %d, want %d", name, code, tc.want)
			continue
		}
		if code != http.StatusCreated {
			continue
		}
		if strings.TrimSpace(tc.partner) == "" {
			if got.Entry.Partner != nil {
				t.Errorf("%s: partner = %+v", name, got.Entry.Partner)
			}
		} else if p := got.Entry.Partner; p == nil || p.ID != ravi.ID || p.Username != "ravi" || p.Name != "Player ravi" {
			t.Errorf("%s: partner = %+v", name, p)
		}
		// Make room: three entries is the most one player may hold.
		asha.do("DELETE", "/api/me/entries/"+got.Entry.ID, nil, nil)
	}
}

// Two players can't both get the last place: many joins at once, one wins.
func TestJoinLastSpotConcurrently(t *testing.T) {
	store, srv := entriesTestServer(t)
	insertEvent(t, store, "evt_t_race", testEvent{capacity: 3})
	first, _ := player(t, store, srv, 1, "first")
	second, _ := player(t, store, srv, 2, "second")
	join(first, "evt_t_race", singles, nil)
	join(second, "evt_t_race", singles, nil)

	const racers = 30
	var clients []adminClient
	for i := range racers {
		c, _ := player(t, store, srv, 100+i, fmt.Sprintf("racer%d", i))
		clients = append(clients, c)
	}

	codes := make(chan int, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, c := range clients {
		wg.Add(1)
		go func(c adminClient) {
			defer wg.Done()
			<-start
			req, err := http.NewRequest("POST", srv.URL+"/api/events/evt_t_race/entries", strings.NewReader(`{"formats":["singles"]}`))
			if err != nil {
				t.Error(err)
				return
			}
			req.AddCookie(c.cookie)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			_ = res.Body.Close()
			codes <- res.StatusCode
		}(c)
	}
	close(start)
	wg.Wait()
	close(codes)

	won, full := 0, 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			won++
		case http.StatusConflict:
			full++
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if won != 1 || full != racers-1 {
		t.Errorf("%d won and %d were turned away, want 1 and %d", won, full, racers-1)
	}
	var rows int
	if err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM entries WHERE event_id = 'evt_t_race'`).Scan(&rows); err != nil || rows != 3 {
		t.Errorf("entries = %d, %v; want 3", rows, err)
	}
}

func TestEntriesTableRejectsBadRows(t *testing.T) {
	store, srv := entriesTestServer(t)
	insertEvent(t, store, "evt_t_rows", testEvent{})
	_, asha := player(t, store, srv, 1, "asha")
	_, ravi := player(t, store, srv, 2, "ravi")
	insert := func(formats, status string, hold *time.Time, partner any) error {
		_, err := store.pool.Exec(context.Background(), `
			INSERT INTO entries (id, event_id, user_id, formats, partner_user_id, status, hold_expires_at)
			VALUES ('ent_t_bad', 'evt_t_rows', $1, $2, $3, $4, $5)`, asha.ID, formats, partner, status, hold)
		return err
	}
	soon := time.Now().Add(time.Minute)
	for name, err := range map[string]error{
		"unknown format":       insert("{tennis}", "confirmed", nil, nil),
		"no formats":           insert("{}", "confirmed", nil, nil),
		"unknown status":       insert("{singles}", "paid", nil, nil),
		"pending without hold": insert("{singles}", "pending_payment", nil, nil),
		"own partner":          insert("{doubles}", "pending_payment", &soon, asha.ID),
		"partner for singles":  insert("{singles}", "pending_payment", &soon, ravi.ID),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := insert("{singles,doubles}", "pending_payment", &soon, ravi.ID); err != nil {
		t.Errorf("a good row: %v", err)
	}
}
