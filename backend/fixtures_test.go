package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- the pure slot logic ---

func TestFindSlotEarliestAndJudgingPlayers(t *testing.T) {
	r := baseSlotRequest()
	r.Preferred = r.StartsAt
	// Court 1 is taken at 19:00, so the earliest is Court 2 at 19:00.
	r.Taken = []FixedMatch{fixedAt("crt_1", 0, 30, "", "p", "q")}
	if got, ok := findSlot(r); !ok || got.CourtID != "crt_2" || !got.StartsAt.Equal(at(0)) {
		t.Errorf("earliest = %+v, %v", got, ok)
	}
	// x judges a match 19:00–19:30: they can't play then, but may play
	// straight after (no gap needed after judging).
	r.Taken = []FixedMatch{fixedAt("crt_9", 0, 30, "x", "p", "q")}
	if got, ok := findSlot(r); !ok || !got.StartsAt.Equal(at(30)) {
		t.Errorf("after judging = %+v, %v", got, ok)
	}
	if b := playerConflict(r, at(15)); b == nil || b.userID != "x" || !b.judging {
		t.Errorf("conflict at 19:15 = %+v", b)
	}
	if b := playerConflict(r, at(30)); b != nil {
		t.Errorf("conflict at 19:30 = %+v", b)
	}
}

func TestFreeSlots(t *testing.T) {
	r := baseSlotRequest()
	r.Taken = []FixedMatch{fixedAt("crt_1", 0, 30, "", "p", "q"), fixedAt("crt_2", 45, 75, "", "r", "s")}
	got := freeSlots(r)
	// 4 starts × 2 courts, less Court 1 at 19:00 and Court 2 at 19:30 and
	// 20:00 (the 19:45 match overlaps both).
	var want []string
	for _, s := range []struct {
		court string
		start int
	}{{"crt_2", 0}, {"crt_1", 30}, {"crt_1", 60}, {"crt_1", 90}, {"crt_2", 90}} {
		want = append(want, fmt.Sprintf("%s@%d", s.court, s.start))
	}
	var have []string
	for _, s := range got {
		have = append(have, fmt.Sprintf("%s@%d", s.CourtID, int(s.StartsAt.Sub(slotStart).Minutes())))
	}
	if !slices.Equal(have, want) {
		t.Errorf("free slots = %v, want %v", have, want)
	}
}

// --- fixture editing against Postgres ---

var kolkata = mustLoad("Asia/Kolkata")

// clockAt is t as the admin's messages show it.
func clockAt(t time.Time) string { return t.In(kolkata).Format("3:04 PM") }

// fixtureEnv is an event three days on, 19:00–21:00 UTC, with two courts,
// two judges (judges[0] has the lower ID), an admin and five players.
type fixtureEnv struct {
	store   *PostgresStore
	admin   adminClient
	adminU  User
	start   time.Time
	judges  []User
	players map[string]User
}

const fxEvent = "evt_t_fx"

func newFixtureEnv(t *testing.T, e testEvent, courts int) fixtureEnv {
	t.Helper()
	store, srv := entriesTestServer(t)
	base := adminClient{t: t, srv: srv}
	insertEvent(t, store, fxEvent, e)
	env := fixtureEnv{store: store, players: map[string]User{}}
	env.start = challengeEventStart(t, store, fxEvent)
	addCourts(t, store, fxEvent, courts)
	env.admin, env.adminU = player(t, store, base.srv, 99, "desk")
	for i, name := range []string{"umpire", "referee"} {
		_, u := player(t, store, base.srv, 90+i, name)
		env.judges = append(env.judges, u)
		addJudge(t, store, fxEvent, u.ID)
	}
	sort.Slice(env.judges, func(i, j int) bool { return env.judges[i].ID < env.judges[j].ID })
	for i, name := range []string{"asha", "ravi", "kiran", "neha", "mohan"} {
		_, u := player(t, store, base.srv, i+1, name)
		env.players[name] = u
	}
	return env
}

// entry adds a paid (confirmed) open entry for the user in fxEvent.
func (env fixtureEnv) entry(t *testing.T, userID string, formats ...string) string {
	t.Helper()
	if len(formats) == 0 {
		formats = []string{FormatSingles}
	}
	id, err := newID("ent")
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, env.store, `INSERT INTO entries (id, event_id, user_id, formats, status) VALUES ($1, $2, $3, $4, 'confirmed')`,
		id, fxEvent, userID, formats)
	return id
}

func (env fixtureEnv) at(minutes int) time.Time {
	return env.start.Add(time.Duration(minutes) * time.Minute)
}

// create posts a manual match and returns it with the status code.
func (env fixtureEnv) create(t *testing.T, body map[string]any) (ScheduleMatch, int) {
	t.Helper()
	if body["format"] == nil {
		body["format"] = FormatSingles
	}
	var out matchResponse
	code := env.admin.do("POST", "/api/admin/events/"+fxEvent+"/matches", body, &out)
	return out.Match, code
}

func (env fixtureEnv) mustCreate(t *testing.T, body map[string]any) ScheduleMatch {
	t.Helper()
	m, code := env.create(t, body)
	if code != http.StatusCreated {
		t.Fatalf("create %v: %d", body, code)
	}
	return m
}

// singlesAt is a create body for a singles match, at court/minutes when
// court isn't "".
func singlesAt(a, b, court string, minutes int, start time.Time) map[string]any {
	body := map[string]any{"format": FormatSingles, "sideA": []string{a}, "sideB": []string{b}}
	if court != "" {
		body["courtId"] = court
		body["startsAt"] = start.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339)
	}
	return body
}

// errorOf sends a request expected to fail and returns its code and message.
func errorOf(t *testing.T, c adminClient, method, path string, body any) (int, string) {
	t.Helper()
	var buf strings.Builder
	if body != nil {
		if s, ok := body.(string); ok {
			buf.WriteString(s)
		} else if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, c.srv.URL+path, strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(c.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var e errorResponse
	_ = json.NewDecoder(res.Body).Decode(&e)
	return res.StatusCode, e.Error
}

func entryRow(t *testing.T, store *PostgresStore, id string) (status string, matchID, reason *string) {
	t.Helper()
	if err := store.pool.QueryRow(context.Background(), `SELECT status, match_id, standby_reason FROM entries WHERE id = $1`, id).
		Scan(&status, &matchID, &reason); err != nil {
		t.Fatal(err)
	}
	return status, matchID, reason
}

func playerUsernames(m ScheduleMatch) []string {
	var out []string
	for _, s := range m.Sides {
		for _, p := range s.Players {
			out = append(out, s.Side+":"+p.Username)
		}
	}
	sort.Strings(out)
	return out
}

func TestFixtureRoutesNeedASession(t *testing.T) {
	_, srv := entriesTestServer(t)
	anon := adminClient{t: t, srv: srv}
	for _, route := range [][2]string{
		{"POST", "/api/admin/events/evt_x/matches"},
		{"POST", "/api/admin/events/evt_x/matches/suggest"},
		{"GET", "/api/admin/events/evt_x/free-slots?format=singles"},
		{"GET", "/api/admin/events/evt_x/changes"},
		{"POST", "/api/admin/matches/mat_x/replace"},
		{"POST", "/api/admin/matches/mat_x/move"},
		{"POST", "/api/admin/matches/mat_x/cancel"},
	} {
		if code := anon.do(route[0], route[1], nil, nil); code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want 401", route[0], route[1], code)
		}
	}
}

func TestCreateMatchByHand(t *testing.T) {
	env := newFixtureEnv(t, testEvent{}, 2)
	p := env.players
	setRating(t, env.store, p["asha"].ID, FormatSingles, 1500)
	asha, ravi := env.entry(t, p["asha"].ID), env.entry(t, p["ravi"].ID)
	kiran, neha := env.entry(t, p["kiran"].ID), env.entry(t, p["neha"].ID)
	mohan := env.entry(t, p["mohan"].ID, FormatSingles, FormatDoubles)
	// A standby entry can be picked too; its reason is cleared.
	execSQL(t, env.store, `UPDATE entries SET status = 'standby', standby_reason = 'no opponent' WHERE id = $1`, ravi)

	// Nothing is published and there are no matches yet: no schedule.
	if code := env.admin.do("GET", "/api/admin/events/"+fxEvent+"/schedule", nil, nil); code != http.StatusNotFound {
		t.Errorf("schedule with nothing: %d", code)
	}

	// Suggest, then create: the same slot and judge.
	var sug slotResponse
	if code := env.admin.do("POST", "/api/admin/events/"+fxEvent+"/matches/suggest",
		map[string]any{"format": "singles", "entryIds": []string{asha, ravi}}, &sug); code != http.StatusOK {
		t.Fatalf("suggest: %d", code)
	}
	s := sug.Slot
	if s.Court.Label != "Court 1" || !s.StartsAt.Equal(env.start) || !s.EndsAt.Equal(env.at(30)) ||
		s.Judge == nil || s.Judge.UserID != env.judges[0].ID || s.Judge.Username == "" ||
		!slices.Equal(s.Flags, []string{"rating gap 300"}) {
		t.Errorf("suggestion = %+v (judge %+v)", s, s.Judge)
	}
	if n := countRows(t, env.store, `SELECT count(*) FROM matches`); n != 0 {
		t.Errorf("suggest saved %d matches", n)
	}
	first := env.mustCreate(t, singlesAt(asha, ravi, "", 0, env.start))
	if first.ID == nil || !strings.HasPrefix(*first.ID, "mat_") || first.Status != MatchScheduled || first.ChallengeID != nil ||
		first.Court != s.Court || !first.StartsAt.Equal(s.StartsAt) || !first.EndsAt.Equal(s.EndsAt) ||
		first.Judge == nil || first.Judge.UserID != s.Judge.UserID || !slices.Equal(first.Flags, []string{"rating gap 300"}) {
		t.Errorf("created = %+v", first)
	}
	if got := playerUsernames(first); !slices.Equal(got, []string{"A:asha", "B:ravi"}) || first.Sides[0].Players[0].EntryID != asha {
		t.Errorf("sides = %v", got)
	}
	for _, id := range []string{asha, ravi} {
		if status, matchID, reason := entryRow(t, env.store, id); status != EntryScheduled || matchID == nil ||
			*matchID != *first.ID || reason != nil {
			t.Errorf("entry %s: %s %v %v", id, status, matchID, reason)
		}
	}

	// A given slot is kept exactly; the judge is the one with fewer
	// matches.
	given := env.mustCreate(t, singlesAt(kiran, neha, "crt_t_entries_2", 60, env.start))
	if given.Court.ID != "crt_t_entries_2" || !given.StartsAt.Equal(env.at(60)) ||
		given.Judge == nil || given.Judge.UserID != env.judges[1].ID {
		t.Errorf("given slot = %+v (judge %+v)", given, given.Judge)
	}

	// Auto again: Court 1 is taken at 19:00 and its judge busy, so Court 2
	// with the other judge.
	neha2 := env.entry(t, p["neha"].ID)
	auto := env.mustCreate(t, singlesAt(mohan, neha2, "", 0, env.start))
	if auto.Court.ID != "crt_t_entries_2" || !auto.StartsAt.Equal(env.start) ||
		auto.Judge == nil || auto.Judge.UserID != env.judges[1].ID {
		t.Errorf("auto = %+v (judge %+v)", auto, auto.Judge)
	}

	// The schedule shows them before any publish.
	var sched scheduleResponse
	if code := env.admin.do("GET", "/api/admin/events/"+fxEvent+"/schedule", nil, &sched); code != http.StatusOK {
		t.Fatalf("schedule: %d", code)
	}
	if sched.Schedule.PublishedAt != nil || len(sched.Schedule.Matches) != 3 || *sched.Schedule.Matches[0].ID != *first.ID {
		t.Errorf("schedule = %+v", sched.Schedule)
	}
	if n := countRows(t, env.store, `SELECT count(*) FROM match_changes WHERE action = 'create' AND admin_id = $1`, env.adminU.ID); n != 3 {
		t.Errorf("%d create audit rows", n)
	}
}

func TestCreateMatchRejects(t *testing.T) {
	env := newFixtureEnv(t, testEvent{}, 2)
	p := env.players
	asha, asha2 := env.entry(t, p["asha"].ID), env.entry(t, p["asha"].ID)
	ravi, kiran := env.entry(t, p["ravi"].ID), env.entry(t, p["kiran"].ID)
	neha := env.entry(t, p["neha"].ID, FormatDoubles)
	unpaid := env.entry(t, p["mohan"].ID)
	execSQL(t, env.store, `UPDATE entries SET status = 'pending_payment', hold_expires_at = now() + interval '1 hour' WHERE id = $1`, unpaid)
	insertEvent(t, env.store, "evt_t_other", testEvent{})
	var other string
	{
		id, _ := newID("ent")
		execSQL(t, env.store, `INSERT INTO entries (id, event_id, user_id, formats, status) VALUES ($1, 'evt_t_other', $2, '{singles}', 'confirmed')`,
			id, p["mohan"].ID)
		other = id
	}
	// Asha plays at 19:00 on Court 1.
	env.mustCreate(t, singlesAt(asha, ravi, "crt_t_entries_1", 0, env.start))

	path := "/api/admin/events/" + fxEvent + "/matches"
	for _, tc := range []struct {
		name string
		path string
		body any
		code int
		msg  string
	}{
		{"unknown event", "/api/admin/events/evt_nope/matches", singlesAt(asha2, kiran, "", 0, env.start), 404, "event not found"},
		{"not JSON", path, "{", 400, "valid JSON"},
		{"unknown field", path, map[string]any{"format": "singles", "sideA": []string{asha2}, "sideB": []string{kiran}, "judge": "x"}, 400, "valid JSON"},
		{"unknown format", path, map[string]any{"format": "tennis", "sideA": []string{asha2}, "sideB": []string{kiran}}, 400, "format must be"},
		{"format not offered", path, map[string]any{"format": "mixed", "sideA": []string{asha2}, "sideB": []string{kiran}}, 400, "doesn't offer mixed"},
		{"wrong side size", path, map[string]any{"format": "singles", "sideA": []string{asha2, kiran}, "sideB": []string{neha}}, 400, "one entry on each side"},
		{"an entry twice", path, singlesAt(kiran, kiran, "", 0, env.start), 400, "only be picked once"},
		{"unknown entry", path, singlesAt(kiran, "ent_nope", "", 0, env.start), 404, "entry not found"},
		{"another event's entry", path, singlesAt(kiran, other, "", 0, env.start), 404, "entry not found"},
		{"unpaid entry", path, singlesAt(kiran, unpaid, "", 0, env.start), 400, "Player mohan's entry isn't waiting for a match (it's pending payment)"},
		{"already scheduled", path, singlesAt(kiran, ravi, "", 0, env.start), 400, "Player ravi's entry isn't waiting for a match (it's scheduled)"},
		{"format not allowed", path, singlesAt(kiran, neha, "", 0, env.start), 400, "Player neha's entry doesn't allow singles"},
		{"one player both sides", path, map[string]any{"format": "singles", "sideA": []string{asha2}, "sideB": []string{env.entry(t, p["asha"].ID)}}, 400, "Player asha can't play on both sides"},
		{"court not the event's", path, map[string]any{"format": "singles", "sideA": []string{asha2}, "sideB": []string{kiran}, "courtId": "crt_nope"}, 400, "isn't one of this event's courts"},
		{"bad start", path, map[string]any{"format": "singles", "sideA": []string{asha2}, "sideB": []string{kiran}, "startsAt": "8pm"}, 400, "startsAt must be"},
		{"start outside the window", path, singlesAt(asha2, kiran, "crt_t_entries_2", 100, env.start), 400,
			fmt.Sprintf("pick a start between %s and %s", clockAt(env.start), clockAt(env.at(90)))},
		{"court taken", path, singlesAt(kiran, env.entry(t, p["neha"].ID), "crt_t_entries_1", 15, env.start), 409,
			fmt.Sprintf("Court 1 is taken at %s", clockAt(env.at(15)))},
		{"player gap", path, singlesAt(asha2, kiran, "crt_t_entries_2", 30, env.start), 409,
			fmt.Sprintf("Player asha already plays at %s", clockAt(env.start))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, msg := errorOf(t, env.admin, "POST", tc.path, tc.body)
			if code != tc.code || !strings.Contains(msg, tc.msg) {
				t.Errorf("got %d %q, want %d containing %q", code, msg, tc.code, tc.msg)
			}
		})
	}
	// Suggest says the same.
	code, msg := errorOf(t, env.admin, "POST", path+"/suggest",
		map[string]any{"format": "singles", "entryIds": []string{asha2, kiran}, "courtId": "crt_t_entries_2",
			"startsAt": env.at(30).Format(time.RFC3339)})
	if code != 409 || !strings.Contains(msg, "Player asha already plays") {
		t.Errorf("suggest: %d %q", code, msg)
	}
	if code, msg := errorOf(t, env.admin, "POST", path+"/suggest",
		map[string]any{"format": "singles", "entryIds": []string{asha2}}); code != 400 || !strings.Contains(msg, "needs 2 entries") {
		t.Errorf("suggest one entry: %d %q", code, msg)
	}
	// Every court taken at the start given.
	env.mustCreate(t, singlesAt(kiran, env.entry(t, p["neha"].ID), "crt_t_entries_2", 0, env.start))
	_, priya := player(t, env.store, env.admin.srv, 7, "priya")
	code, msg = errorOf(t, env.admin, "POST", path, map[string]any{"format": "singles", "sideA": []string{env.entry(t, p["mohan"].ID)},
		"sideB": []string{env.entry(t, priya.ID)}, "startsAt": env.start.Format(time.RFC3339)})
	if code != 409 || !strings.Contains(msg, "every court is taken at "+clockAt(env.start)) {
		t.Errorf("every court taken: %d %q", code, msg)
	}
	// A cancelled event takes no new matches.
	execSQL(t, env.store, `UPDATE events SET status = 'cancelled' WHERE id = $1`, fxEvent)
	if code, _ := errorOf(t, env.admin, "POST", path, singlesAt(asha2, env.entry(t, p["mohan"].ID), "", 0, env.start)); code != 409 {
		t.Errorf("cancelled event: %d", code)
	}
}

func TestCreateMatchRespectsMaxMatches(t *testing.T) {
	env := newFixtureEnv(t, testEvent{maxMatches: 1}, 2)
	p := env.players
	env.mustCreate(t, singlesAt(env.entry(t, p["asha"].ID), env.entry(t, p["ravi"].ID), "", 0, env.start))
	code, msg := errorOf(t, env.admin, "POST", "/api/admin/events/"+fxEvent+"/matches",
		singlesAt(env.entry(t, p["kiran"].ID), env.entry(t, p["neha"].ID), "", 0, env.start))
	if code != 409 || !strings.Contains(msg, "already has 1 match, its limit") {
		t.Errorf("over the limit: %d %q", code, msg)
	}
}

func TestCreateMatchKeepsAPlayersGap(t *testing.T) {
	env := newFixtureEnv(t, testEvent{}, 2)
	p := env.players
	asha1, asha2 := env.entry(t, p["asha"].ID), env.entry(t, p["asha"].ID)
	env.mustCreate(t, singlesAt(asha1, env.entry(t, p["ravi"].ID), "", 0, env.start))
	// Court 2 is free at 19:00 and 19:30, but asha needs a slot's gap: 20:00.
	second := env.mustCreate(t, singlesAt(asha2, env.entry(t, p["kiran"].ID), "", 0, env.start))
	if !second.StartsAt.Equal(env.at(60)) || second.Court.Label != "Court 1" {
		t.Errorf("asha's second match = %s at %v", second.Court.Label, second.StartsAt)
	}

	// Neha plays in another event, on another court, at 20:30: that counts
	// too.
	insertEvent(t, env.store, "evt_t_far", testEvent{})
	challengeEventStart(t, env.store, "evt_t_far")
	execSQL(t, env.store, `INSERT INTO courts (id, venue_id, label) VALUES ('crt_t_far', 'ven_t_entries', 'Far court')`)
	execSQL(t, env.store, `INSERT INTO event_courts (event_id, court_id, venue_id) VALUES ('evt_t_far', 'crt_t_far', 'ven_t_entries')`)
	insertMatch(t, env.store, "mat_t_far", "evt_t_far", "crt_t_far", env.at(90), env.at(120), nil, p["neha"].ID, p["mohan"].ID)
	code, msg := errorOf(t, env.admin, "POST", "/api/admin/events/"+fxEvent+"/matches",
		singlesAt(env.entry(t, p["neha"].ID), env.entry(t, p["ravi"].ID), "crt_t_entries_2", 60, env.start))
	if code != 409 || msg != "Player neha already plays at "+clockAt(env.at(90)) {
		t.Errorf("another event's match: %d %q", code, msg)
	}
}

func TestManualMatchJudgeNeverPlays(t *testing.T) {
	env := newFixtureEnv(t, testEvent{}, 2)
	// Only one judge, umpire, who also plays.
	execSQL(t, env.store, `DELETE FROM event_judges WHERE event_id = $1 AND user_id <> $2`, fxEvent, env.judges[0].ID)
	p := env.players
	ump := env.judges[0]
	m := env.mustCreate(t, singlesAt(env.entry(t, ump.ID), env.entry(t, p["asha"].ID), "", 0, env.start))
	if m.Judge != nil || !slices.Contains(m.Flags, FlagNoJudge) {
		t.Errorf("own match judged: %+v", m)
	}
	// Nor another match while they play.
	m2 := env.mustCreate(t, singlesAt(env.entry(t, p["kiran"].ID), env.entry(t, p["neha"].ID), "crt_t_entries_2", 0, env.start))
	if m2.Judge != nil {
		t.Errorf("judge while playing: %+v", m2.Judge)
	}
	// Free again an hour later.
	m3 := env.mustCreate(t, singlesAt(env.entry(t, p["mohan"].ID), env.entry(t, p["ravi"].ID), "crt_t_entries_2", 60, env.start))
	if m3.Judge == nil || m3.Judge.UserID != ump.ID {
		t.Errorf("free judge not used: %+v", m3.Judge)
	}
	// A judge can't be put in a match at a time they judge one.
	code, msg := errorOf(t, env.admin, "POST", "/api/admin/events/"+fxEvent+"/matches",
		singlesAt(env.entry(t, ump.ID), env.entry(t, p["neha"].ID), "crt_t_entries_1", 60, env.start))
	if code != 409 || !strings.Contains(msg, ump.Name+" is judging a match at "+clockAt(env.at(60))) {
		t.Errorf("judge playing while judging: %d %q", code, msg)
	}
}

func TestReplacePlayer(t *testing.T) {
	env := newFixtureEnv(t, testEvent{}, 2)
	p := env.players
	asha, ravi := env.entry(t, p["asha"].ID), env.entry(t, p["ravi"].ID)
	kiran, neha := env.entry(t, p["kiran"].ID), env.entry(t, p["neha"].ID)
	m := env.mustCreate(t, singlesAt(asha, ravi, "", 0, env.start))
	path := "/api/admin/matches/" + *m.ID + "/replace"
	if m.Judge == nil || m.Judge.UserID != env.judges[0].ID {
		t.Fatalf("judge = %+v", m.Judge)
	}

	// Ravi doesn't turn up: no_show, keeps no refund; kiran plays.
	var out matchResponse
	if code := env.admin.do("POST", path, map[string]string{"outEntryId": ravi, "inEntryId": kiran, "outcome": "no_show"}, &out); code != http.StatusOK {
		t.Fatalf("replace: %d", code)
	}
	if got := playerUsernames(out.Match); !slices.Equal(got, []string{"A:asha", "B:kiran"}) {
		t.Errorf("after no_show: %v", got)
	}
	if status, matchID, _ := entryRow(t, env.store, ravi); status != EntryNoShow || matchID == nil || *matchID != *m.ID {
		t.Errorf("ravi: %s %v", status, matchID)
	}
	if status, matchID, _ := entryRow(t, env.store, kiran); status != EntryScheduled || matchID == nil || *matchID != *m.ID {
		t.Errorf("kiran: %s %v", status, matchID)
	}

	// Kiran swapped for neha, back to the pool.
	if code := env.admin.do("POST", path, map[string]string{"outEntryId": kiran, "inEntryId": neha, "outcome": "back_to_pool"}, &out); code != http.StatusOK {
		t.Fatalf("replace back to pool: %d", code)
	}
	if status, matchID, _ := entryRow(t, env.store, kiran); status != EntryConfirmed || matchID != nil {
		t.Errorf("kiran back in the pool: %s %v", status, matchID)
	}
	// Kiran's entry can be used again.
	env.mustCreate(t, singlesAt(kiran, env.entry(t, p["mohan"].ID), "crt_t_entries_2", 60, env.start))

	// The judge comes in as a player: someone else judges.
	ump := env.entry(t, env.judges[0].ID)
	if code := env.admin.do("POST", path, map[string]string{"outEntryId": neha, "inEntryId": ump, "outcome": "back_to_pool"}, &out); code != http.StatusOK {
		t.Fatalf("replace with the judge: %d", code)
	}
	if out.Match.Judge == nil || out.Match.Judge.UserID != env.judges[1].ID {
		t.Errorf("judge after the judge came in: %+v", out.Match.Judge)
	}

	busy := env.entry(t, p["mohan"].ID) // mohan plays at 20:00 on Court 2
	execSQL(t, env.store, `UPDATE matches SET starts_at = $2, ends_at = $3 WHERE id <> $1`, *m.ID, env.at(30), env.at(60))
	for _, tc := range []struct {
		name string
		path string
		body map[string]string
		code int
		msg  string
	}{
		{"unknown match", "/api/admin/matches/mat_nope/replace", map[string]string{"outEntryId": asha, "inEntryId": neha, "outcome": "no_show"}, 404, "match not found"},
		{"bad outcome", path, map[string]string{"outEntryId": asha, "inEntryId": neha, "outcome": "refund"}, 400, "outcome must be"},
		{"missing ids", path, map[string]string{"outcome": "no_show"}, 400, "required"},
		{"unknown entry", path, map[string]string{"outEntryId": asha, "inEntryId": "ent_nope", "outcome": "no_show"}, 404, "entry not found"},
		{"out not in the match", path, map[string]string{"outEntryId": neha, "inEntryId": busy, "outcome": "no_show"}, 400, "Player neha's entry isn't in this match"},
		{"incoming not waiting", path, map[string]string{"outEntryId": asha, "inEntryId": ravi, "outcome": "no_show"}, 400, "isn't waiting for a match (it's no show)"},
		{"incoming already plays", path, map[string]string{"outEntryId": asha, "inEntryId": env.entry(t, env.judges[0].ID), "outcome": "no_show"}, 400, env.judges[0].Name + " already plays in this match"},
		{"incoming's gap", path, map[string]string{"outEntryId": asha, "inEntryId": busy, "outcome": "no_show"}, 409,
			"Player mohan already plays at " + clockAt(env.at(30))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, msg := errorOf(t, env.admin, "POST", tc.path, tc.body)
			if code != tc.code || !strings.Contains(msg, tc.msg) {
				t.Errorf("got %d %q, want %d containing %q", code, msg, tc.code, tc.msg)
			}
		})
	}
	// A live match can't change.
	execSQL(t, env.store, `UPDATE matches SET status = 'live' WHERE id = $1`, *m.ID)
	if code, msg := errorOf(t, env.admin, "POST", path, map[string]string{"outEntryId": asha, "inEntryId": neha, "outcome": "no_show"}); code != 409 ||
		!strings.Contains(msg, "this one is live") {
		t.Errorf("live match: %d %q", code, msg)
	}
}

func TestMoveMatch(t *testing.T) {
	env := newFixtureEnv(t, testEvent{}, 2)
	p := env.players
	j1, j2 := env.judges[0], env.judges[1]
	asha := env.entry(t, p["asha"].ID)
	m1 := env.mustCreate(t, singlesAt(asha, env.entry(t, p["ravi"].ID), "crt_t_entries_1", 0, env.start))
	m2 := env.mustCreate(t, singlesAt(env.entry(t, p["kiran"].ID), env.entry(t, p["neha"].ID), "crt_t_entries_1", 60, env.start))
	if m1.Judge.UserID != j1.ID || m2.Judge.UserID != j2.ID {
		t.Fatalf("judges %s, %s", m1.Judge.UserID, m2.Judge.UserID)
	}
	move := func(id, court string, minutes int) (ScheduleMatch, int, string) {
		t.Helper()
		body := map[string]string{"courtId": court, "startsAt": env.at(minutes).Format(time.RFC3339)}
		code, msg := errorOf(t, env.admin, "POST", "/api/admin/matches/"+id+"/move", body)
		var out matchResponse
		if code == http.StatusOK {
			env.admin.do("POST", "/api/admin/matches/"+id+"/move", body, &out) // the same move again: a no-op
		}
		return out.Match, code, msg
	}

	// To Court 2 at 19:00, overlapping m1: j2 is free then, so kept.
	got, code, _ := move(*m2.ID, "crt_t_entries_2", 0)
	if code != 200 || got.Court.ID != "crt_t_entries_2" || !got.StartsAt.Equal(env.start) || !got.EndsAt.Equal(env.at(30)) ||
		got.Judge == nil || got.Judge.UserID != j2.ID {
		t.Errorf("moved = %d %+v", code, got)
	}
	// Court 1 at 19:15 overlaps m1 on Court 1.
	if _, code, msg := move(*m2.ID, "crt_t_entries_1", 15); code != 409 || msg != "Court 1 is taken at "+clockAt(env.at(15)) {
		t.Errorf("court busy: %d %q", code, msg)
	}
	// Outside the window, and a court that isn't the event's.
	if _, code, msg := move(*m2.ID, "crt_t_entries_1", 95); code != 400 || !strings.Contains(msg, "pick a start between") {
		t.Errorf("outside the window: %d %q", code, msg)
	}
	if _, code, _ := move(*m2.ID, "crt_nope", 60); code != 400 {
		t.Errorf("other court: %d", code)
	}
	if _, code, _ := move("mat_nope", "crt_t_entries_1", 60); code != 404 {
		t.Errorf("unknown match: %d", code)
	}
	if code, _ := errorOf(t, env.admin, "POST", "/api/admin/matches/"+*m2.ID+"/move", map[string]string{"courtId": "crt_t_entries_1"}); code != 400 {
		t.Errorf("no start: %d", code)
	}

	// Asha's second match at 20:00: m1 can't move next to it.
	m3 := env.mustCreate(t, singlesAt(env.entry(t, p["asha"].ID), env.entry(t, p["mohan"].ID), "crt_t_entries_2", 60, env.start))
	if _, code, msg := move(*m1.ID, "crt_t_entries_1", 90); code != 409 || msg != "Player asha already plays at "+clockAt(env.at(60)) {
		t.Errorf("gap: %d %q", code, msg)
	}

	// j2 plays at 20:30 (a match with no judge, so j1 is free); m2 (judged
	// by j2) moves there: j1 takes over.
	x := env.mustCreate(t, singlesAt(env.entry(t, j2.ID), env.entry(t, p["ravi"].ID), "crt_t_entries_2", 90, env.start))
	execSQL(t, env.store, `UPDATE matches SET judge_id = NULL WHERE id = $1`, *x.ID)
	got, code, _ = move(*m2.ID, "crt_t_entries_1", 90)
	if code != 200 || got.Judge == nil || got.Judge.UserID != j1.ID {
		t.Errorf("judge reassigned: %d %+v", code, got.Judge)
	}
	// m3 moves to its own time on another court: its own slot doesn't get
	// in the way, and free-slots counts it as free.
	var free freeSlotsResponse
	if code := env.admin.do("GET", "/api/admin/events/"+fxEvent+"/free-slots?format=singles&matchId="+*m3.ID, nil, &free); code != 200 {
		t.Fatalf("free slots: %d", code)
	}
	if !slices.ContainsFunc(free.Slots, func(s FreeSlot) bool { return s.Court.ID == "crt_t_entries_2" && s.StartsAt.Equal(env.at(60)) }) {
		t.Errorf("m3's own slot isn't free: %+v", free.Slots)
	}
	if got, code, _ := move(*m3.ID, "crt_t_entries_2", 60); code != 200 || !got.StartsAt.Equal(env.at(60)) {
		t.Errorf("move in place: %d", code)
	}
	// A completed match doesn't move.
	execSQL(t, env.store, `UPDATE matches SET status = 'completed' WHERE id = $1`, *m1.ID)
	if _, code, _ := move(*m1.ID, "crt_t_entries_1", 30); code != 409 {
		t.Errorf("completed match moved: %d", code)
	}
	// Two moves of m2 and m3 (counting the no-op repeats) are audited.
	if n := countRows(t, env.store, `SELECT count(*) FROM match_changes WHERE action = 'move'`); n != 6 {
		t.Errorf("%d move audit rows", n)
	}
}

func TestFreeSlotsEndpoint(t *testing.T) {
	env := newFixtureEnv(t, testEvent{}, 2)
	p := env.players
	path := "/api/admin/events/" + fxEvent + "/free-slots?format="
	var free freeSlotsResponse
	if code := env.admin.do("GET", path+"singles", nil, &free); code != 200 || len(free.Slots) != 8 {
		t.Fatalf("free slots: %d %d", code, len(free.Slots))
	}
	if s := free.Slots[1]; s.Court.Label != "Court 2" || !s.StartsAt.Equal(env.start) || !s.EndsAt.Equal(env.at(30)) {
		t.Errorf("second slot = %+v", s)
	}
	m := env.mustCreate(t, singlesAt(env.entry(t, p["asha"].ID), env.entry(t, p["ravi"].ID), "", 0, env.start))
	env.admin.do("GET", path+"singles", nil, &free)
	if len(free.Slots) != 7 {
		t.Errorf("after a match: %d slots", len(free.Slots))
	}
	env.admin.do("GET", path+"singles&matchId="+*m.ID, nil, &free)
	if len(free.Slots) != 8 {
		t.Errorf("leaving the match out: %d slots", len(free.Slots))
	}
	// Doubles slots are 40 minutes: 3 a court in two hours.
	env.admin.do("GET", path+"doubles", nil, &free)
	if len(free.Slots) != 5 {
		t.Errorf("doubles: %d slots", len(free.Slots))
	}
	for q, want := range map[string]int{"": 400, "mixed": 400, "singles&matchId=mat_nope": 404} {
		if code, _ := errorOf(t, env.admin, "GET", path+q, nil); code != want {
			t.Errorf("?format=%s: %d, want %d", q, code, want)
		}
	}
	if code, _ := errorOf(t, env.admin, "GET", "/api/admin/events/evt_nope/free-slots?format=singles", nil); code != 404 {
		t.Errorf("unknown event: %d", code)
	}
}

func TestCancelMatch(t *testing.T) {
	env := newFixtureEnv(t, testEvent{}, 2)
	p := env.players
	asha, ravi := env.entry(t, p["asha"].ID), env.entry(t, p["ravi"].ID)
	m := env.mustCreate(t, singlesAt(asha, ravi, "", 0, env.start))
	var out matchResponse
	if code := env.admin.do("POST", "/api/admin/matches/"+*m.ID+"/cancel", nil, &out); code != http.StatusOK {
		t.Fatalf("cancel: %d", code)
	}
	if out.Match.Status != MatchCancelled || len(out.Match.Sides) != 2 {
		t.Errorf("cancelled = %+v", out.Match)
	}
	for _, id := range []string{asha, ravi} {
		if status, matchID, _ := entryRow(t, env.store, id); status != EntryConfirmed || matchID != nil {
			t.Errorf("entry %s: %s %v", id, status, matchID)
		}
	}
	if code, msg := errorOf(t, env.admin, "POST", "/api/admin/matches/"+*m.ID+"/cancel", nil); code != 409 || !strings.Contains(msg, "cancelled") {
		t.Errorf("cancel twice: %d %q", code, msg)
	}
	if code, _ := errorOf(t, env.admin, "POST", "/api/admin/matches/mat_nope/cancel", nil); code != 404 {
		t.Errorf("unknown match: %d", code)
	}
	// The court time and the entries are free again; the schedule hides it.
	again := env.mustCreate(t, singlesAt(asha, ravi, "", 0, env.start))
	if again.Court.Label != "Court 1" || !again.StartsAt.Equal(env.start) {
		t.Errorf("after cancelling = %+v", again)
	}
	var sched scheduleResponse
	env.admin.do("GET", "/api/admin/events/"+fxEvent+"/schedule", nil, &sched)
	if len(sched.Schedule.Matches) != 1 || *sched.Schedule.Matches[0].ID != *again.ID {
		t.Errorf("schedule = %+v", sched.Schedule.Matches)
	}
}

func TestChallengeMatchCanMoveButNotChange(t *testing.T) {
	f := newChallengeFixture(t, 2, time.Hour)
	payLocked(t, f.asha, f.ch, "pay_fake_fx_asha")
	payLocked(t, f.ravi, f.ch, "pay_fake_fx_ravi")
	ch, _ := getChallenge(f.asha, f.ch.ID)
	if ch.Match == nil {
		t.Fatal("no challenge match")
	}
	admin, _ := player(t, f.store, f.srv, 50, "desk")
	kiran, _ := player(t, f.store, f.srv, 3, "kiran")
	in := joinPaid(t, kiran, admin, "evt_t_fix", singles)
	mine, _ := getChallenge(f.asha, f.ch.ID)

	// The schedule shows it as the challenge's.
	var sched scheduleResponse
	if code := admin.do("GET", "/api/admin/events/evt_t_fix/schedule", nil, &sched); code != 200 ||
		len(sched.Schedule.Matches) != 1 || sched.Schedule.Matches[0].ChallengeID == nil || *sched.Schedule.Matches[0].ChallengeID != f.ch.ID {
		t.Fatalf("schedule: %d %+v", code, sched.Schedule)
	}
	path := "/api/admin/matches/" + ch.Match.ID
	if code, msg := errorOf(t, admin, "POST", path+"/replace",
		map[string]string{"outEntryId": mine.MyEntry.ID, "inEntryId": in.ID, "outcome": "back_to_pool"}); code != 409 ||
		!strings.Contains(msg, "challenge match") {
		t.Errorf("replace in a challenge: %d %q", code, msg)
	}
	if code, msg := errorOf(t, admin, "POST", path+"/cancel", nil); code != 409 || !strings.Contains(msg, "withdrawing or refunding") {
		t.Errorf("cancel a challenge: %d %q", code, msg)
	}
	var out matchResponse
	if code := admin.do("POST", path+"/move", map[string]string{"courtId": "crt_t_entries_1",
		"startsAt": f.start.Format(time.RFC3339)}, &out); code != 200 || !out.Match.StartsAt.Equal(f.start) ||
		out.Match.ChallengeID == nil {
		t.Errorf("move a challenge: %d %+v", code, out.Match)
	}
	if again, _ := getChallenge(f.ravi, f.ch.ID); again.Match == nil || !again.Match.StartsAt.Equal(f.start) {
		t.Errorf("challenge after the move: %+v", again.Match)
	}
}

func TestMatchChangesAudit(t *testing.T) {
	env := newFixtureEnv(t, testEvent{}, 2)
	p := env.players
	asha, ravi, kiran := env.entry(t, p["asha"].ID), env.entry(t, p["ravi"].ID), env.entry(t, p["kiran"].ID)
	m := env.mustCreate(t, singlesAt(asha, ravi, "", 0, env.start))
	id := *m.ID
	if code := env.admin.do("POST", "/api/admin/matches/"+id+"/move",
		map[string]string{"courtId": "crt_t_entries_2", "startsAt": env.at(30).Format(time.RFC3339)}, nil); code != 200 {
		t.Fatalf("move: %d", code)
	}
	if code := env.admin.do("POST", "/api/admin/matches/"+id+"/replace",
		map[string]string{"outEntryId": ravi, "inEntryId": kiran, "outcome": "no_show"}, nil); code != 200 {
		t.Fatalf("replace: %d", code)
	}
	if code := env.admin.do("POST", "/api/admin/matches/"+id+"/cancel", nil, nil); code != 200 {
		t.Fatalf("cancel: %d", code)
	}

	var list matchChangesResponse
	if code := env.admin.do("GET", "/api/admin/events/"+fxEvent+"/changes", nil, &list); code != 200 {
		t.Fatalf("changes: %d", code)
	}
	var actions []string
	for _, c := range list.Changes {
		actions = append(actions, c.Action)
		if c.MatchID != id || !strings.HasPrefix(c.ID, "mch_") || c.Admin.ID != env.adminU.ID || c.Admin.Username != "desk" ||
			c.Admin.Name != "Player desk" || c.CreatedAt.IsZero() {
			t.Errorf("change = %+v", c)
		}
	}
	if !slices.Equal(actions, []string{"cancel", "replace", "move", "create"}) {
		t.Fatalf("actions = %v", actions)
	}
	var cancel struct {
		Before struct {
			Status string     `json:"status"`
			Slot   changeSlot `json:"slot"`
		} `json:"before"`
		Released []string `json:"releasedEntryIds"`
	}
	var replace struct {
		Out, In       changeEntry
		Outcome, Side string
	}
	var move struct{ Before, After changeSlot }
	var create struct {
		Format string              `json:"format"`
		Sides  map[string][]string `json:"sides"`
		After  changeSlot          `json:"after"`
	}
	for i, v := range []any{&cancel, &replace, &move, &create} {
		if err := json.Unmarshal(list.Changes[i].Details, v); err != nil {
			t.Fatal(err)
		}
	}
	if cancel.Before.Status != MatchScheduled || cancel.Before.Slot.CourtLabel != "Court 2" ||
		!slices.Equal(cancel.Released, []string{min(asha, kiran), max(asha, kiran)}) {
		t.Errorf("cancel details = %+v", cancel)
	}
	if replace.Out.EntryID != ravi || replace.In.EntryID != kiran || replace.In.UserID != p["kiran"].ID ||
		replace.Outcome != "no_show" || replace.Side != "B" {
		t.Errorf("replace details = %s", list.Changes[1].Details)
	}
	if move.Before.CourtLabel != "Court 1" || !move.Before.StartsAt.Equal(env.start) || move.After.CourtLabel != "Court 2" ||
		!move.After.StartsAt.Equal(env.at(30)) || move.After.JudgeID == nil {
		t.Errorf("move details = %s", list.Changes[2].Details)
	}
	if create.Format != "singles" || !slices.Equal(create.Sides["A"], []string{asha}) || !slices.Equal(create.Sides["B"], []string{ravi}) ||
		create.After.CourtID != "crt_t_entries_1" || !create.After.EndsAt.Equal(env.at(30)) {
		t.Errorf("create details = %s", list.Changes[3].Details)
	}
	if code, _ := errorOf(t, env.admin, "GET", "/api/admin/events/evt_nope/changes", nil); code != 404 {
		t.Errorf("unknown event: %d", code)
	}
}

func TestPublishWorksAroundManualMatches(t *testing.T) {
	// One court, two hours: four singles slots. A match made by hand takes
	// 19:00.
	env := newFixtureEnv(t, testEvent{}, 1)
	p := env.players
	asha, ravi := env.entry(t, p["asha"].ID), env.entry(t, p["ravi"].ID)
	manual := env.mustCreate(t, singlesAt(asha, ravi, "", 0, env.start))
	// Asha again, and three more, through the pool.
	pool := map[string]bool{}
	for _, name := range []string{"asha", "kiran", "neha", "mohan"} {
		pool[env.entry(t, p[name].ID)] = true
	}

	var before scheduleResponse
	if code := env.admin.do("GET", "/api/admin/events/"+fxEvent+"/schedule", nil, &before); code != 200 ||
		before.Schedule.PublishedAt != nil || len(before.Schedule.Matches) != 1 {
		t.Fatalf("before publishing: %d %+v", code, before.Schedule)
	}
	var published scheduleResponse
	if code := env.admin.do("POST", "/api/admin/events/"+fxEvent+"/schedule/publish", nil, &published); code != http.StatusCreated {
		t.Fatalf("publish: %d", code)
	}
	if len(published.Schedule.Matches) != 2 {
		t.Fatalf("published %d matches: %+v", len(published.Schedule.Matches), published.Schedule)
	}
	for _, m := range published.Schedule.Matches {
		if m.StartsAt.Before(env.at(30)) {
			t.Errorf("a match at %v reuses the manual match's court time", m.StartsAt)
		}
		for _, s := range m.Sides {
			for _, pl := range s.Players {
				if !pool[pl.EntryID] {
					t.Errorf("entry %s isn't from the pool", pl.EntryID)
				}
				if pl.UserID == p["asha"].ID && m.StartsAt.Before(env.at(60)) {
					t.Errorf("asha plays again at %v", m.StartsAt)
				}
			}
		}
	}
	for _, id := range []string{asha, ravi} {
		if status, matchID, _ := entryRow(t, env.store, id); status != EntryScheduled || *matchID != *manual.ID {
			t.Errorf("manual entry %s: %s %v", id, status, matchID)
		}
	}
	var after scheduleResponse
	if code := env.admin.do("GET", "/api/admin/events/"+fxEvent+"/schedule", nil, &after); code != 200 ||
		after.Schedule.PublishedAt == nil || len(after.Schedule.Matches) != 3 || *after.Schedule.Matches[0].ID != *manual.ID {
		t.Errorf("after publishing: %d %+v", code, after.Schedule)
	}
	// Made by hand after publishing too, on a second court: ravi's gap
	// after 19:00 puts it at 20:00.
	execSQL(t, env.store, `INSERT INTO courts (id, venue_id, label) VALUES ('crt_t_entries_2', 'ven_t_entries', 'Court 2')
		ON CONFLICT (id) DO NOTHING`)
	execSQL(t, env.store, `INSERT INTO event_courts (event_id, court_id, venue_id) VALUES ($1, 'crt_t_entries_2', 'ven_t_entries')`, fxEvent)
	_, priya := player(t, env.store, env.admin.srv, 7, "priya")
	late := env.mustCreate(t, singlesAt(env.entry(t, priya.ID), env.entry(t, p["ravi"].ID), "", 0, env.start))
	if late.Court.Label != "Court 2" || !late.StartsAt.Equal(env.at(60)) {
		t.Errorf("after publishing = %s at %v", late.Court.Label, late.StartsAt)
	}
}

func TestCreateMatchRace(t *testing.T) {
	for round := range 3 {
		env := newFixtureEnv(t, testEvent{}, 2)
		p := env.players
		bodies := []map[string]any{
			singlesAt(env.entry(t, p["asha"].ID), env.entry(t, p["ravi"].ID), "crt_t_entries_1", 30, env.start),
			singlesAt(env.entry(t, p["kiran"].ID), env.entry(t, p["neha"].ID), "crt_t_entries_1", 30, env.start),
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		codes := make([]int, len(bodies))
		for i, body := range bodies {
			raw, _ := json.Marshal(body)
			wg.Go(func() {
				<-start
				req, _ := http.NewRequest("POST", env.admin.srv.URL+"/api/admin/events/"+fxEvent+"/matches", strings.NewReader(string(raw)))
				req.AddCookie(env.admin.cookie)
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Error(err)
					return
				}
				_ = res.Body.Close()
				codes[i] = res.StatusCode
			})
		}
		close(start)
		wg.Wait()
		slices.Sort(codes)
		if !slices.Equal(codes, []int{http.StatusCreated, http.StatusConflict}) {
			t.Errorf("round %d: codes %v", round, codes)
		}
		if n := countRows(t, env.store, `SELECT count(*) FROM matches WHERE status <> 'cancelled'`); n != 1 {
			t.Errorf("round %d: %d matches", round, n)
		}
	}
}

func TestMatchSources(t *testing.T) {
	// A challenge's match, one made by hand, and a publish, in one event.
	f := newChallengeFixture(t, 2, 0)
	payLocked(t, f.asha, f.ch, "pay_fake_src_asha")
	payLocked(t, f.ravi, f.ch, "pay_fake_src_ravi")
	admin, _ := player(t, f.store, f.srv, 50, "desk")
	var clients []adminClient
	var entries []string
	for i, name := range []string{"kiran", "neha", "mohan", "priya"} {
		c, _ := player(t, f.store, f.srv, 3+i, name)
		clients = append(clients, c)
		entries = append(entries, joinPaid(t, c, admin, "evt_t_fix", singles).ID)
	}
	var made matchResponse
	if code := admin.do("POST", "/api/admin/events/evt_t_fix/matches",
		map[string]any{"format": "singles", "sideA": []string{entries[0]}, "sideB": []string{entries[1]}}, &made); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if made.Match.Source != SourceAdmin {
		t.Errorf("created source = %q", made.Match.Source)
	}
	var preview scheduleResponse
	admin.do("POST", "/api/admin/events/evt_t_fix/schedule/preview", nil, &preview)
	if len(preview.Schedule.Matches) != 1 || preview.Schedule.Matches[0].Source != SourceMatcher {
		t.Errorf("preview = %+v", preview.Schedule.Matches)
	}
	var published scheduleResponse
	if code := admin.do("POST", "/api/admin/events/evt_t_fix/schedule/publish", nil, &published); code != http.StatusCreated {
		t.Fatalf("publish: %d", code)
	}

	var sched scheduleResponse
	admin.do("GET", "/api/admin/events/evt_t_fix/schedule", nil, &sched)
	got := map[string]int{}
	for _, m := range sched.Schedule.Matches {
		got[m.Source]++
		if (m.ChallengeID != nil) != (m.Source == SourceChallenge) {
			t.Errorf("match %s: source %s, challenge %v", *m.ID, m.Source, m.ChallengeID)
		}
	}
	if got[SourceChallenge] != 1 || got[SourceAdmin] != 1 || got[SourceMatcher] != 1 || len(got) != 3 {
		t.Errorf("sources = %v", got)
	}
	// A player's own matches say it too.
	for c, want := range map[adminClient]string{f.asha: SourceChallenge, clients[0]: SourceAdmin, clients[2]: SourceMatcher} {
		var mine matchesResponse
		c.do("GET", "/api/me/matches", nil, &mine)
		if len(mine.Matches) != 1 || mine.Matches[0].Source != want {
			t.Errorf("my matches = %+v, want source %s", mine.Matches, want)
		}
	}
	if n := countRows(t, f.store, `SELECT count(*) FROM matches WHERE source NOT IN ('matcher', 'admin', 'challenge')`); n != 0 {
		t.Errorf("%d matches with another source", n)
	}
}
