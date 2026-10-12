package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- the schedule, the desk and player matches, against Postgres ---

// addCourts gives an insertEvent event n courts at its venue.
func addCourts(t *testing.T, store *PostgresStore, eventID string, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("crt_t_entries_%d", i)
		if _, err := store.pool.Exec(ctx, `
			INSERT INTO courts (id, venue_id, label) VALUES ($1, 'ven_t_entries', $2) ON CONFLICT (id) DO NOTHING`,
			id, fmt.Sprintf("Court %d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx,
			`INSERT INTO event_courts (event_id, court_id, venue_id) VALUES ($1, $2, 'ven_t_entries')`, eventID, id); err != nil {
			t.Fatal(err)
		}
	}
}

func setRating(t *testing.T, store *PostgresStore, userID, discipline string, rating int) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `
		INSERT INTO player_ratings (user_id, discipline, rating) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, discipline) DO UPDATE SET rating = $3`, userID, discipline, rating); err != nil {
		t.Fatal(err)
	}
}

func addJudge(t *testing.T, store *PostgresStore, eventID, userID string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(),
		`INSERT INTO event_judges (event_id, user_id) VALUES ($1, $2)`, eventID, userID); err != nil {
		t.Fatal(err)
	}
}

// joinPaid joins with an open entry and has admin mark it paid.
func joinPaid(t *testing.T, c, admin adminClient, eventID string, body any) AdminEntry {
	t.Helper()
	var created entryResponse
	if code := join(c, eventID, body, &created); code != http.StatusCreated {
		t.Fatalf("join: %d", code)
	}
	var paid adminEntryResponse
	if code := admin.do("POST", "/api/admin/entries/"+created.Entry.ID+"/mark-paid", nil, &paid); code != http.StatusOK {
		t.Fatalf("mark paid: %d", code)
	}
	return paid.Entry
}

func countRows(t *testing.T, store *PostgresStore, query string, args ...any) int {
	t.Helper()
	var n int
	if err := store.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestScheduleRoutesNeedASession(t *testing.T) {
	_, srv := entriesTestServer(t)
	anon := adminClient{t: t, srv: srv}
	for _, route := range [][2]string{
		{"GET", "/api/admin/events/evt_x/schedule"},
		{"POST", "/api/admin/events/evt_x/schedule/preview"},
		{"POST", "/api/admin/events/evt_x/schedule/publish"},
		{"GET", "/api/admin/events/evt_x/entries"},
		{"POST", "/api/admin/entries/ent_x/mark-paid"},
		{"GET", "/api/me/matches"},
	} {
		if code := anon.do(route[0], route[1], nil, nil); code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want 401", route[0], route[1], code)
		}
	}
}

func TestPreviewAndPublishSchedule(t *testing.T) {
	store, srv := entriesTestServer(t)
	ctx := context.Background()
	insertEvent(t, store, "evt_t_sched", testEvent{capacity: 20, maxMatches: 8})
	addCourts(t, store, "evt_t_sched", 2)
	admin, adminUser := player(t, store, srv, 99, "desk")
	_, judgeUser := player(t, store, srv, 98, "umpire")
	addJudge(t, store, "evt_t_sched", judgeUser.ID)

	// Four singles players, the top two 300 apart from the bottom two, and
	// one more who will sit out.
	var clients []adminClient
	var users []User
	for i, name := range []string{"asha", "ravi", "kiran", "neha", "late"} {
		c, u := player(t, store, srv, i+1, name)
		clients, users = append(clients, c), append(users, u)
	}
	for i, r := range []int{1700, 1650, 1300, 1250, 1240} {
		setRating(t, store, users[i].ID, FormatSingles, r)
	}
	var entries []AdminEntry
	for i := range clients {
		entries = append(entries, joinPaid(t, clients[i], admin, "evt_t_sched", singles))
	}
	// An unpaid entry isn't in the pool.
	var unpaid entryResponse
	join(clients[0], "evt_t_sched", singles, &unpaid)

	// The admin's entry list: everyone, in join order, with ratings.
	var list adminEntriesResponse
	if code := admin.do("GET", "/api/admin/events/evt_t_sched/entries", nil, &list); code != http.StatusOK {
		t.Fatalf("list entries: %d", code)
	}
	if len(list.Entries) != 6 {
		t.Fatalf("entries = %d, want 6", len(list.Entries))
	}
	first := list.Entries[0]
	if first.ID != entries[0].ID || first.User.Username != "asha" || first.User.Name != "Player asha" ||
		first.Status != EntryConfirmed || first.Ratings[FormatSingles] != 1700 ||
		first.ConfirmedBy == nil || first.ConfirmedBy.ID != adminUser.ID || first.ConfirmedAt == nil {
		t.Errorf("first entry = %+v", first)
	}
	if last := list.Entries[5]; last.ID != unpaid.Entry.ID || last.Status != EntryPendingPayment || last.ConfirmedBy != nil {
		t.Errorf("unpaid entry = %+v", last)
	}

	// Preview: two matches, the late joiner on standby, nothing saved.
	var preview scheduleResponse
	if code := admin.do("POST", "/api/admin/events/evt_t_sched/schedule/preview", nil, &preview); code != http.StatusOK {
		t.Fatalf("preview: %d", code)
	}
	p := preview.Schedule
	if p.EventID != "evt_t_sched" || p.PublishedAt != nil || len(p.Matches) != 2 || len(p.Standby) != 1 {
		t.Fatalf("preview = %+v", p)
	}
	top := p.Matches[0]
	if top.ID != nil || top.Format != FormatSingles || top.Court.Label != "Court 1" || top.Judge == nil ||
		top.Judge.Username != "umpire" || len(top.Sides) != 2 || top.Sides[0].Side != "A" || top.Sides[1].Side != "B" {
		t.Errorf("top match = %+v", top)
	}
	if a := top.Sides[0].Players[0]; a.UserID != users[0].ID || a.Name != "Player asha" || a.Username != "asha" ||
		a.Rating != 1700 || a.EntryID != entries[0].ID || top.Sides[0].Rating != 1700 {
		t.Errorf("side A = %+v", top.Sides[0])
	}
	if top.Sides[1].Players[0].Username != "ravi" || len(top.Flags) != 0 {
		t.Errorf("top match = %+v", top)
	}
	// One judge, two matches at the same time: the second goes without.
	if other := p.Matches[1]; other.Judge != nil || !strings.Contains(strings.Join(other.Flags, ","), FlagNoJudge) {
		t.Errorf("second match = %+v", other)
	}
	if sb := p.Standby[0]; sb.EntryID != entries[4].ID || sb.User.Username != "late" || sb.Reason != StandbyNoOpponent {
		t.Errorf("standby = %+v", sb)
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "joining is still open") {
		t.Errorf("warnings = %v", p.Warnings)
	}
	if n := countRows(t, store, `SELECT count(*) FROM matches`); n != 0 {
		t.Errorf("preview saved %d matches", n)
	}

	if code := admin.do("GET", "/api/admin/events/evt_t_sched/schedule", nil, nil); code != http.StatusNotFound {
		t.Errorf("schedule before publishing: %d, want 404", code)
	}

	// Publish: saved, entries moved on, recorded.
	var published scheduleResponse
	if code := admin.do("POST", "/api/admin/events/evt_t_sched/schedule/publish", nil, &published); code != http.StatusCreated {
		t.Fatalf("publish: %d", code)
	}
	pub := published.Schedule
	if pub.PublishedAt == nil || len(pub.Matches) != 2 || pub.Matches[0].ID == nil || !strings.HasPrefix(*pub.Matches[0].ID, "mat_") {
		t.Fatalf("published = %+v", pub)
	}
	if n := countRows(t, store, `SELECT count(*) FROM matches WHERE event_id = 'evt_t_sched' AND status = 'scheduled'`); n != 2 {
		t.Errorf("matches = %d", n)
	}
	if n := countRows(t, store, `SELECT count(*) FROM match_sides`); n != 4 {
		t.Errorf("match sides = %d", n)
	}
	var status, matchID string
	if err := store.pool.QueryRow(ctx, `SELECT status, match_id FROM entries WHERE id = $1`, entries[0].ID).Scan(&status, &matchID); err != nil ||
		status != EntryScheduled || matchID != *pub.Matches[0].ID {
		t.Errorf("asha's entry: %s %s %v", status, matchID, err)
	}
	var reason string
	if err := store.pool.QueryRow(ctx, `SELECT status, standby_reason FROM entries WHERE id = $1`, entries[4].ID).Scan(&status, &reason); err != nil ||
		status != EntryStandby || reason != StandbyNoOpponent {
		t.Errorf("late entry: %s %s %v", status, reason, err)
	}
	if n := countRows(t, store, `SELECT count(*) FROM entries WHERE id = $1 AND status = 'pending_payment'`, unpaid.Entry.ID); n != 1 {
		t.Error("publishing touched an unpaid entry")
	}
	if n := countRows(t, store, `SELECT count(*) FROM events WHERE id = 'evt_t_sched' AND schedule_published_by = $1`, adminUser.ID); n != 1 {
		t.Error("publish not recorded")
	}

	// The saved schedule reads back as it was published.
	var saved scheduleResponse
	if code := admin.do("GET", "/api/admin/events/evt_t_sched/schedule", nil, &saved); code != http.StatusOK {
		t.Fatalf("get schedule: %d", code)
	}
	sv := saved.Schedule
	if sv.PublishedAt == nil || !sv.PublishedAt.Equal(*pub.PublishedAt) || len(sv.Matches) != 2 || len(sv.Standby) != 1 {
		t.Fatalf("saved schedule = %+v", sv)
	}
	for i, m := range sv.Matches {
		want := pub.Matches[i]
		if *m.ID != *want.ID || m.Court != want.Court || !m.StartsAt.Equal(want.StartsAt) ||
			strings.Join(m.Flags, ",") != strings.Join(want.Flags, ",") ||
			m.Sides[0].Players[0].EntryID != want.Sides[0].Players[0].EntryID || m.Sides[1].Rating != want.Sides[1].Rating {
			t.Errorf("saved match %d = %+v, want %+v", i, m, want)
		}
	}
	if sv.Matches[0].Judge == nil || sv.Matches[0].Judge.Username != "umpire" || sv.Matches[1].Judge != nil {
		t.Errorf("saved judges = %+v, %+v", sv.Matches[0].Judge, sv.Matches[1].Judge)
	}
	if sv.Standby[0].EntryID != entries[4].ID || sv.Standby[0].Reason != StandbyNoOpponent || sv.Standby[0].User.Username != "late" {
		t.Errorf("saved standby = %+v", sv.Standby)
	}
	if code := admin.do("GET", "/api/admin/events/evt_nope/schedule", nil, nil); code != http.StatusNotFound {
		t.Errorf("unknown event: %d", code)
	}

	// Version 1 publishes once; a preview now says so.
	if code := admin.do("POST", "/api/admin/events/evt_t_sched/schedule/publish", nil, nil); code != http.StatusConflict {
		t.Errorf("second publish: %d", code)
	}
	admin.do("POST", "/api/admin/events/evt_t_sched/schedule/preview", nil, &preview)
	if preview.Schedule.PublishedAt == nil || len(preview.Schedule.Matches) != 0 ||
		!strings.Contains(strings.Join(preview.Schedule.Warnings, ","), "already published") {
		t.Errorf("preview after publishing = %+v", preview.Schedule)
	}

	// Each player sees their own match.
	var mine matchesResponse
	if code := clients[2].do("GET", "/api/me/matches", nil, &mine); code != http.StatusOK || len(mine.Matches) != 1 {
		t.Fatalf("kiran's matches: %d %+v", code, mine.Matches)
	}
	m := mine.Matches[0]
	if m.Event == nil || m.Event.Title != "Event evt_t_sched" || m.Venue.Name != "Entry Club" || m.Court.Label == "" ||
		m.Status != MatchScheduled || m.Format != FormatSingles || m.MySide != "A" || m.WinnerSide != nil ||
		!m.EndsAt.Equal(m.StartsAt.Add(30*time.Minute)) {
		t.Errorf("kiran's match = %+v", m)
	}
	if len(m.Sides) != 2 || m.Sides[0].Players[0].Username != "kiran" || m.Sides[0].Players[0].Rating != 1300 ||
		m.Sides[1].Players[0].Username != "neha" || m.Sides[1].Rating != 1250 || m.Sides[0].Players[0].EntryID != "" {
		t.Errorf("sides = %+v", m.Sides)
	}
	clients[0].do("GET", "/api/me/matches", nil, &mine)
	if len(mine.Matches) != 1 || mine.Matches[0].Judge == nil || mine.Matches[0].Judge.Name != "Player umpire" {
		t.Errorf("asha's matches = %+v", mine.Matches)
	}
	clients[4].do("GET", "/api/me/matches", nil, &mine)
	if mine.Matches == nil || len(mine.Matches) != 0 {
		t.Errorf("the standby player's matches = %+v", mine.Matches)
	}
}

func TestPublishScheduleRejects(t *testing.T) {
	store, srv := entriesTestServer(t)
	insertEvent(t, store, "evt_t_draft", testEvent{status: EventDraft})
	insertEvent(t, store, "evt_t_empty", testEvent{})
	insertEvent(t, store, "evt_t_lonely", testEvent{})
	admin, _ := player(t, store, srv, 99, "desk")
	asha, _ := player(t, store, srv, 1, "asha")
	joinPaid(t, asha, admin, "evt_t_lonely", singles)

	for event, want := range map[string]int{
		"evt_nope":     http.StatusNotFound,
		"evt_t_draft":  http.StatusConflict, // not published
		"evt_t_empty":  http.StatusConflict, // nobody to schedule
		"evt_t_lonely": http.StatusConflict, // one entry makes no match
	} {
		if code := admin.do("POST", "/api/admin/events/"+event+"/schedule/publish", nil, nil); code != want {
			t.Errorf("publish %s: %d, want %d", event, code, want)
		}
	}
	if code := admin.do("POST", "/api/admin/events/evt_nope/schedule/preview", nil, nil); code != http.StatusNotFound {
		t.Errorf("preview unknown: %d", code)
	}
	if code := admin.do("GET", "/api/admin/events/evt_nope/entries", nil, nil); code != http.StatusNotFound {
		t.Errorf("entries of unknown: %d", code)
	}
	// Nothing was published, so the lonely event can still be scheduled later.
	if n := countRows(t, store, `SELECT count(*) FROM events WHERE schedule_published_at IS NOT NULL`); n != 0 {
		t.Errorf("%d events marked published", n)
	}
}

func TestMarkEntryPaid(t *testing.T) {
	store, srv := entriesTestServer(t)
	ctx := context.Background()
	insertEvent(t, store, "evt_t_desk", testEvent{capacity: 2})
	insertEvent(t, store, "evt_t_called_off", testEvent{})
	admin, _ := player(t, store, srv, 99, "desk")
	_, asha := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")
	_, kiran := player(t, store, srv, 3, "kiran")

	// Asha joined 11 minutes ago; her hold has expired but there's room.
	stale, err := store.CreateEntry(ctx, EntryInput{
		EventID: "evt_t_desk", UserID: asha.ID, Formats: []string{FormatSingles}, Now: time.Now().Add(-11 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	var paid adminEntryResponse
	if code := admin.do("POST", "/api/admin/entries/"+stale.ID+"/mark-paid", nil, &paid); code != http.StatusOK {
		t.Fatalf("mark an expired hold paid: %d", code)
	}
	if paid.Entry.Status != EntryConfirmed || paid.Entry.HoldExpired || paid.Entry.ConfirmedBy == nil || paid.Entry.ConfirmedBy.Username != "desk" {
		t.Errorf("entry = %+v", paid.Entry)
	}
	if code := admin.do("POST", "/api/admin/entries/"+stale.ID+"/mark-paid", nil, nil); code != http.StatusConflict {
		t.Errorf("twice: %d", code)
	}
	if code := admin.do("POST", "/api/admin/entries/ent_nope/mark-paid", nil, nil); code != http.StatusNotFound {
		t.Errorf("unknown: %d", code)
	}

	// Kiran's hold expires too, then Ravi takes the last place, so Kiran's
	// entry finds no room.
	late, err := store.CreateEntry(ctx, EntryInput{
		EventID: "evt_t_desk", UserID: kiran.ID, Formats: []string{FormatSingles}, Now: time.Now().Add(-11 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	join(ravi, "evt_t_desk", singles, nil)
	if _, err := store.MarkEntryPaid(ctx, late.ID, kiran.ID, time.Now()); !errors.Is(err, ErrEventFull) {
		t.Errorf("no room: err = %v", err)
	}
	// A live hold already counts, so it always fits.
	var raviEntry entriesResponse
	ravi.do("GET", "/api/me/entries", nil, &raviEntry)
	if code := admin.do("POST", "/api/admin/entries/"+raviEntry.Entries[0].ID+"/mark-paid", nil, nil); code != http.StatusOK {
		t.Errorf("a held entry in a full event: %d", code)
	}

	// A cancelled or finished event takes no payments.
	held, err := store.CreateEntry(ctx, EntryInput{EventID: "evt_t_called_off", UserID: kiran.ID, Formats: []string{FormatSingles}, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE events SET status = 'cancelled' WHERE id = 'evt_t_called_off'`); err != nil {
		t.Fatal(err)
	}
	if code := admin.do("POST", "/api/admin/entries/"+held.ID+"/mark-paid", nil, nil); code != http.StatusConflict {
		t.Errorf("cancelled event: %d", code)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE events SET status = 'published' WHERE id = 'evt_t_called_off'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkEntryPaid(ctx, held.ID, kiran.ID, time.Now().Add(80*time.Hour)); !errors.Is(err, ErrEventOver) {
		t.Errorf("after the event: err = %v", err)
	}
}

// Two desks marking expired holds paid for the last place: one wins.
func TestMarkEntryPaidLastSpotConcurrently(t *testing.T) {
	store, srv := entriesTestServer(t)
	ctx := context.Background()
	insertEvent(t, store, "evt_t_race_desk", testEvent{capacity: 1})
	var ids []string
	for i := range 10 {
		_, u := player(t, store, srv, i+1, fmt.Sprintf("racer%d", i))
		en, err := store.CreateEntry(ctx, EntryInput{
			// 11 minutes apart, so each hold has expired before the next joins.
			EventID: "evt_t_race_desk", UserID: u.ID, Formats: []string{FormatSingles}, Now: time.Now().Add(-time.Duration(11*(11-i)) * time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, en.ID)
	}
	_, admin := player(t, store, srv, 99, "desk")

	errs := make(chan error, len(ids))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			_, err := store.MarkEntryPaid(ctx, id, admin.ID, time.Now())
			errs <- err
		}(id)
	}
	close(start)
	wg.Wait()
	close(errs)
	won := 0
	for err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, ErrEventFull):
			t.Errorf("unexpected error %v", err)
		}
	}
	if won != 1 {
		t.Errorf("%d marked paid, want 1", won)
	}
}

func TestJoinUsesStoredRatings(t *testing.T) {
	store, srv := entriesTestServer(t)
	insertEvent(t, store, "evt_t_band", testEvent{ratingMax: intp(1600)})
	asha, ashaUser := player(t, store, srv, 1, "asha")
	setRating(t, store, ashaUser.ID, FormatSingles, 1700)

	// Every chosen format's rating must be in the band; doubles is still 1200.
	if code := join(asha, "evt_t_band", singles, nil); code != http.StatusConflict {
		t.Errorf("singles at 1700: %d", code)
	}
	if code := join(asha, "evt_t_band", map[string]any{"formats": []string{"singles", "doubles"}}, nil); code != http.StatusConflict {
		t.Errorf("singles and doubles: %d", code)
	}
	if code := join(asha, "evt_t_band", map[string]any{"formats": []string{"doubles"}}, nil); code != http.StatusCreated {
		t.Errorf("doubles at 1200: %d", code)
	}
}

func TestMatchesTablesRejectBadRows(t *testing.T) {
	store, srv := entriesTestServer(t)
	ctx := context.Background()
	insertEvent(t, store, "evt_t_rows", testEvent{})
	insertEvent(t, store, "evt_t_other_rows", testEvent{})
	addCourts(t, store, "evt_t_rows", 1)
	_, asha := player(t, store, srv, 1, "asha")
	_, ravi := player(t, store, srv, 2, "ravi")
	for _, en := range [][2]string{{"ent_t_a", asha.ID}, {"ent_t_r", ravi.ID}} {
		if _, err := store.pool.Exec(ctx, `
			INSERT INTO entries (id, event_id, user_id, formats, status) VALUES ($1, 'evt_t_rows', $2, '{singles}', 'confirmed')`,
			en[0], en[1]); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now().Add(72 * time.Hour)
	match := func(id, event string, offset time.Duration, status string) error {
		_, err := store.pool.Exec(ctx, `
			INSERT INTO matches (id, event_id, format, court_id, starts_at, ends_at, status)
			VALUES ($1, $2, 'singles', 'crt_t_entries_1', $3, $4, $5)`,
			id, event, start.Add(offset), start.Add(offset+30*time.Minute), status)
		return err
	}
	side := func(matchID, s, userID, entryID string) error {
		_, err := store.pool.Exec(ctx, `INSERT INTO match_sides (match_id, side, user_id, entry_id) VALUES ($1, $2, $3, $4)`,
			matchID, s, userID, entryID)
		return err
	}
	if err := match("mat_t_1", "evt_t_rows", 0, "scheduled"); err != nil {
		t.Fatal(err)
	}
	if err := side("mat_t_1", "A", asha.ID, "ent_t_a"); err != nil {
		t.Fatal(err)
	}

	for name, err := range map[string]error{
		"court double-booked":   match("mat_t_2", "evt_t_rows", 15*time.Minute, "scheduled"),
		"court not the event's": match("mat_t_3", "evt_t_other_rows", 2*time.Hour, "scheduled"),
		"entry used twice":      side("mat_t_1", "B", asha.ID, "ent_t_a"),
		"someone else's entry":  side("mat_t_1", "B", ravi.ID, "ent_t_a"),
		"unknown side":          side("mat_t_1", "C", ravi.ID, "ent_t_r"),
		"scheduled without match": func() error {
			_, err := store.pool.Exec(ctx, `UPDATE entries SET status = 'scheduled' WHERE id = 'ent_t_r'`)
			return err
		}(),
		"winner of a live match": func() error {
			_, err := store.pool.Exec(ctx, `UPDATE matches SET winner_side = 'A' WHERE id = 'mat_t_1'`)
			return err
		}(),
		"rating in an unknown one": func() error {
			_, err := store.pool.Exec(ctx, `INSERT INTO player_ratings (user_id, discipline) VALUES ($1, 'tennis')`, asha.ID)
			return err
		}(),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// Back to back on one court is fine, and a cancelled match frees its slot.
	if err := match("mat_t_4", "evt_t_rows", 30*time.Minute, "scheduled"); err != nil {
		t.Errorf("back to back: %v", err)
	}
	if err := match("mat_t_5", "evt_t_rows", 15*time.Minute, "cancelled"); err != nil {
		t.Errorf("a cancelled overlap: %v", err)
	}
	// No event at all (rule 1): the court key isn't checked.
	if err := match("mat_t_6", "", 5*time.Hour, "scheduled"); err == nil {
		t.Error("an empty event ID was accepted")
	}
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO matches (id, format, court_id, starts_at, ends_at) VALUES ('mat_t_7', 'singles', 'crt_t_entries_1', $1, $2)`,
		start.Add(5*time.Hour), start.Add(5*time.Hour+30*time.Minute)); err != nil {
		t.Errorf("a match without an event: %v", err)
	}
}

func TestSeedDemoPoolAndPublishedSchedule(t *testing.T) {
	store := seededStore(t, testNow)
	ctx := context.Background()
	if err := seedDemo(ctx, store.pool, testNow); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if n := countRows(t, store, `SELECT count(*) FROM users WHERE id LIKE 'usr_demo_%'`); n != len(demoPlayers)+len(demoJudges) {
		t.Errorf("demo users = %d", n)
	}
	if n := countRows(t, store, `SELECT count(*) FROM player_ratings WHERE user_id LIKE 'usr_demo_%'`); n != 3*len(demoPlayers) {
		t.Errorf("demo ratings = %d", n)
	}
	if n := countRows(t, store, `SELECT count(*) FROM entries WHERE event_id = $1 AND status = 'confirmed'`, demoEntryEvent); n != len(demoEntries) {
		t.Errorf("demo entries = %d", n)
	}
	if n := countRows(t, store, `SELECT count(*) FROM event_judges WHERE event_id = $1`, demoEntryEvent); n != 2 {
		t.Errorf("demo judges = %d", n)
	}

	// The preview has a real schedule: partners kept, one on standby.
	view, err := store.PreviewSchedule(ctx, demoEntryEvent, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Matches) != 6 || len(view.Standby) != 1 {
		t.Errorf("preview: %d matches, standby %+v", len(view.Matches), view.Standby)
	}
	together, judged := false, 0
	for _, m := range view.Matches {
		for _, s := range m.Sides {
			if len(s.Players) == 2 && s.Players[0].Username == "demo_priya" && s.Players[1].Username == "demo_meera" {
				together = true
			}
		}
		if m.Judge != nil {
			judged++
		}
	}
	if !together {
		t.Error("Priya and Meera aren't a team")
	}
	// Three courts and two judges: the matches at the start can't all have
	// one, which shows the "no judge" flag.
	if judged < 4 {
		t.Errorf("only %d of %d matches have a judge", judged, len(view.Matches))
	}

	// Once published, re-seeding leaves the event and its entries alone.
	if _, err := store.PublishSchedule(ctx, demoEntryEvent, "usr_demo_sameer", testNow); err != nil {
		t.Fatal(err)
	}
	var startsAt time.Time
	if err := store.pool.QueryRow(ctx, `SELECT starts_at FROM events WHERE id = $1`, demoEntryEvent).Scan(&startsAt); err != nil {
		t.Fatal(err)
	}
	if err := seedDemo(ctx, store.pool, testNow.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("seed after publishing: %v", err)
	}
	var after time.Time
	if err := store.pool.QueryRow(ctx, `SELECT starts_at FROM events WHERE id = $1`, demoEntryEvent).Scan(&after); err != nil || !after.Equal(startsAt) {
		t.Errorf("the scheduled event moved: %v → %v (%v)", startsAt, after, err)
	}
	if n := countRows(t, store, `SELECT count(*) FROM entries WHERE event_id = $1 AND status = 'confirmed'`, demoEntryEvent); n != 0 {
		t.Errorf("%d demo entries were reset to confirmed", n)
	}
}
