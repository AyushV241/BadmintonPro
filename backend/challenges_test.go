package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
	"github.com/AyushV241/BadmintonPro/backend/internal/payments"
)

// --- rules without a database ---

func TestChallengeExpiry(t *testing.T) {
	sent := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	if got := challengeExpiry(sent, sent.Add(24*time.Hour)); !got.Equal(sent.Add(4 * time.Hour)) {
		t.Errorf("far cutoff: %v, want 4 hours after sending", got)
	}
	if got := challengeExpiry(sent, sent.Add(time.Hour)); !got.Equal(sent.Add(time.Hour)) {
		t.Errorf("near cutoff: %v, want the cutoff", got)
	}
}

func TestValidateChallenge(t *testing.T) {
	eventID, opponent, start, err := validateChallenge(challengeRequest{
		EventID: "evt_1", OpponentUsername: " @Ravi ", PreferredStart: "2026-10-17T19:30:45+05:30",
	})
	if err != nil {
		t.Fatal(err)
	}
	if eventID != "evt_1" || opponent != "ravi" || !start.Equal(time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("got %q %q %v", eventID, opponent, start)
	}
	for name, req := range map[string]challengeRequest{
		"no event":    {OpponentUsername: "ravi", PreferredStart: "2026-10-17T19:30:00Z"},
		"no opponent": {EventID: "evt_1", OpponentUsername: "@", PreferredStart: "2026-10-17T19:30:00Z"},
		"no time":     {EventID: "evt_1", OpponentUsername: "ravi"},
		"bad time":    {EventID: "evt_1", OpponentUsername: "ravi", PreferredStart: "17/10/2026 19:30"},
	} {
		if _, _, _, err := validateChallenge(req); !errors.Is(err, errValidation) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
}

// slotStart is 19:00 on the day of a two-hour event in UTC.
var slotStart = time.Date(2026, 10, 17, 19, 0, 0, 0, time.UTC)

func at(minutes int) time.Time { return slotStart.Add(time.Duration(minutes) * time.Minute) }

func baseSlotRequest() SlotRequest {
	return SlotRequest{
		StartsAt: slotStart, EndsAt: at(120), Length: 30 * time.Minute, Preferred: at(60),
		Courts:    []Court{{ID: "crt_2", Label: "Court 2"}, {ID: "crt_1", Label: "Court 1"}},
		JudgeIDs:  []string{"usr_judge_b", "usr_judge_a"},
		PlayerIDs: []string{"x", "y"},
	}
}

func fixedAt(court string, from, to int, judge string, users ...string) FixedMatch {
	return FixedMatch{CourtID: court, StartsAt: at(from), EndsAt: at(to), JudgeID: judge, UserIDs: users}
}

func TestFindChallengeSlot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*SlotRequest)
		court string
		start int // minutes after 19:00
		judge string
	}{
		{name: "the preferred time, first court, first judge", court: "crt_1", start: 60, judge: "usr_judge_a"},
		{name: "a preferred time off the hour stays", edit: func(r *SlotRequest) { r.Preferred = at(47) }, court: "crt_1", start: 47, judge: "usr_judge_a"},
		{name: "first court taken: the next court", edit: func(r *SlotRequest) {
			r.Taken = []FixedMatch{fixedAt("crt_1", 60, 90, "", "p", "q")}
		}, court: "crt_2", start: 60, judge: "usr_judge_a"},
		{name: "both courts taken: the nearest free start", edit: func(r *SlotRequest) {
			r.Taken = []FixedMatch{fixedAt("crt_1", 45, 75, "", "p", "q"), fixedAt("crt_2", 45, 75, "", "r", "s")}
		}, court: "crt_1", start: 75, judge: "usr_judge_a"},
		{name: "the earlier on a tie", edit: func(r *SlotRequest) {
			// 19:10 and 20:30 are both 40 minutes from 19:50.
			r.Preferred = at(50)
			r.Taken = []FixedMatch{fixedAt("crt_1", 40, 90, "", "p", "q"), fixedAt("crt_2", 40, 90, "", "r", "s")}
		}, court: "crt_1", start: 10, judge: "usr_judge_a"},
		{name: "a player's other match: a gap of one slot each side", edit: func(r *SlotRequest) {
			// x plays 20:00–20:30, so nothing between 19:00 (exclusive) and
			// 21:00; 20:30 is the latest start, so 19:00.
			r.Taken = []FixedMatch{fixedAt("crt_1", 60, 90, "", "x", "z")}
		}, court: "crt_1", start: 0, judge: "usr_judge_a"},
		{name: "the gap is the shorter slot", edit: func(r *SlotRequest) {
			// y's 40-minute doubles match ends at 19:40; singles needs 30
			// minutes after, so 20:10.
			r.Preferred = at(40)
			r.Taken = []FixedMatch{fixedAt("crt_1", 0, 40, "", "y", "p", "q", "r")}
		}, court: "crt_1", start: 70, judge: "usr_judge_a"},
		{name: "the window's end bounds a late preference", edit: func(r *SlotRequest) { r.Preferred = at(115) }, court: "crt_1", start: 90, judge: "usr_judge_a"},
		{name: "a judge playing then is skipped", edit: func(r *SlotRequest) {
			r.Taken = []FixedMatch{fixedAt("crt_2", 50, 80, "", "usr_judge_a", "p")}
		}, court: "crt_1", start: 60, judge: "usr_judge_b"},
		{name: "the judge with fewest matches", edit: func(r *SlotRequest) {
			r.Taken = []FixedMatch{fixedAt("crt_2", 0, 30, "usr_judge_a", "p", "q")}
		}, court: "crt_1", start: 60, judge: "usr_judge_b"},
		{name: "a player is never their own judge", edit: func(r *SlotRequest) { r.JudgeIDs = []string{"x"} }, court: "crt_1", start: 60},
		{name: "a judge judging at the same time is busy", edit: func(r *SlotRequest) {
			r.JudgeIDs = []string{"usr_judge_a"}
			r.Taken = []FixedMatch{fixedAt("crt_2", 60, 90, "usr_judge_a", "p", "q")}
		}, court: "crt_1", start: 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := baseSlotRequest()
			if tc.edit != nil {
				tc.edit(&r)
			}
			got, ok := findChallengeSlot(r)
			if !ok {
				t.Fatal("no slot")
			}
			want := SlotChoice{CourtID: tc.court, StartsAt: at(tc.start), EndsAt: at(tc.start + 30), JudgeID: tc.judge}
			if got != want {
				t.Errorf("got %s at %s judged by %q, want %s at %s judged by %q",
					got.CourtID, got.StartsAt.Format("15:04"), got.JudgeID, want.CourtID, want.StartsAt.Format("15:04"), want.JudgeID)
			}
		})
	}
}

func TestFindChallengeSlotNothingFits(t *testing.T) {
	r := baseSlotRequest()
	r.Courts = r.Courts[:1]
	r.Taken = []FixedMatch{fixedAt("crt_2", 0, 60, "", "p", "q"), fixedAt("crt_2", 60, 120, "", "r", "s")}
	if got, ok := findChallengeSlot(r); ok {
		t.Errorf("got %+v on a full court", got)
	}
	// x plays every half hour on another court: no gap anywhere.
	r = baseSlotRequest()
	r.Taken = []FixedMatch{fixedAt("crt_9", 15, 45, "", "x", "p"), fixedAt("crt_9", 75, 105, "", "x", "q")}
	if got, ok := findChallengeSlot(r); ok {
		t.Errorf("got %+v though x has no gap", got)
	}
	r = baseSlotRequest()
	r.Courts = nil
	if _, ok := findChallengeSlot(r); ok {
		t.Error("a slot with no courts")
	}
}

// --- challenges against Postgres ---

// challengesTestServer is paymentsTestServer with challenges, and the API
// itself so tests can run the expiry sweep with their own clock.
func challengesTestServer(t *testing.T, provider *testProvider) (*PostgresStore, *httptest.Server, *API) {
	t.Helper()
	store := eventsTestStore(t)
	if _, err := store.pool.Exec(context.Background(),
		`TRUNCATE users, sessions, user_identities, event_judges, entries, payments, matches, challenges CASCADE`); err != nil {
		t.Fatal(err)
	}
	api := NewAPI(store, oauth.NewRegistry(), nil, false).
		WithEvents(store).WithAdmin(store).WithEntries(store).WithMatches(store).WithUserSearch(store).
		WithPayments(store, provider).WithChallenges(store)
	srv := httptest.NewServer(api.Routes())
	t.Cleanup(srv.Close)
	return store, srv, api
}

// challengeEventStart puts an insertEvent event on whole hours: 19:00 UTC
// three days on, for two hours, and returns its start.
func challengeEventStart(t *testing.T, store *PostgresStore, eventID string) time.Time {
	t.Helper()
	start := time.Now().UTC().Add(72 * time.Hour).Truncate(24 * time.Hour).Add(19 * time.Hour)
	execSQL(t, store, `UPDATE events SET starts_at = $2, ends_at = $3 WHERE id = $1`, eventID, start, start.Add(2*time.Hour))
	return start
}

func sendChallenge(c adminClient, eventID, opponent string, start time.Time) (Challenge, int) {
	c.t.Helper()
	var out challengeResponse
	code := c.do("POST", "/api/challenges", map[string]string{
		"eventId": eventID, "opponentUsername": opponent, "preferredStart": start.Format(time.RFC3339),
	}, &out)
	return out.Challenge, code
}

func challengeAct(c adminClient, id, verb string) (Challenge, int) {
	c.t.Helper()
	var out challengeResponse
	code := c.do("POST", "/api/challenges/"+id+"/"+verb, nil, &out)
	return out.Challenge, code
}

func getChallenge(c adminClient, id string) (Challenge, int) {
	c.t.Helper()
	var out challengeResponse
	code := c.do("GET", "/api/challenges/"+id, nil, &out)
	return out.Challenge, code
}

func mustSend(t *testing.T, c adminClient, eventID, opponent string, start time.Time) Challenge {
	t.Helper()
	ch, code := sendChallenge(c, eventID, opponent, start)
	if code != http.StatusCreated {
		t.Fatalf("send challenge: %d", code)
	}
	return ch
}

func mustAct(t *testing.T, c adminClient, id, verb string) Challenge {
	t.Helper()
	ch, code := challengeAct(c, id, verb)
	if code != http.StatusOK {
		t.Fatalf("%s: %d", verb, code)
	}
	return ch
}

// payLocked pays for the player's entry in the challenge through the fake
// checkout and returns the entry afterwards.
func payLocked(t *testing.T, c adminClient, ch Challenge, paymentID string) Entry {
	t.Helper()
	mine, code := getChallenge(c, ch.ID)
	if code != http.StatusOK || mine.MyEntry == nil {
		t.Fatalf("challenge for paying: %d %+v", code, mine)
	}
	en, code := fakePay(c, checkout(t, c, mine.MyEntry.ID), paymentID)
	if code != http.StatusOK {
		t.Fatalf("pay: %d", code)
	}
	return en
}

func challengeStatus(t *testing.T, store *PostgresStore, id string) string {
	t.Helper()
	var s string
	if err := store.pool.QueryRow(context.Background(), `SELECT status FROM challenges WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// challengeEntries returns the challenge's locked entries' statuses by user.
func challengeEntries(t *testing.T, store *PostgresStore, id string) map[string]string {
	t.Helper()
	rows, err := store.pool.Query(context.Background(), `SELECT user_id, status FROM entries WHERE challenge_id = $1`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var u, s string
		if err := rows.Scan(&u, &s); err != nil {
			t.Fatal(err)
		}
		out[u] = s
	}
	return out
}

// makeDue moves a challenge's deadline (and its entries' holds) into the
// past, as if four hours had gone by.
func makeDue(t *testing.T, store *PostgresStore, id string) {
	t.Helper()
	execSQL(t, store, `UPDATE challenges SET expires_at = now() - interval '1 minute' WHERE id = $1`, id)
	execSQL(t, store, `UPDATE entries SET hold_expires_at = now() - interval '1 minute' WHERE challenge_id = $1`, id)
}

func TestChallengeRoutesNeedASession(t *testing.T) {
	_, srv, _ := challengesTestServer(t, newTestProvider())
	anon := adminClient{t: t, srv: srv}
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/challenges"}, {"GET", "/api/me/challenges"}, {"GET", "/api/challenges/chl_x"},
		{"POST", "/api/challenges/chl_x/accept"}, {"POST", "/api/challenges/chl_x/decline"},
		{"POST", "/api/challenges/chl_x/withdraw"}, {"GET", "/api/me/recent-players"},
		{"GET", "/api/players/nearby?lat=12.9&lng=77.6"},
	} {
		if code := anon.do(route.method, route.path, nil, nil); code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d", route.method, route.path, code)
		}
	}
}

func TestSendChallenge(t *testing.T) {
	store, srv, _ := challengesTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_chl", testEvent{})
	start := challengeEventStart(t, store, "evt_t_chl")
	asha, ashaUser := player(t, store, srv, 1, "asha")
	_, raviUser := player(t, store, srv, 2, "ravi")

	before := time.Now()
	ch := mustSend(t, asha, "evt_t_chl", "@Ravi", start.Add(45*time.Minute))
	if ch.Status != ChallengePending || ch.Role != RoleChallenger || ch.Format != FormatSingles ||
		ch.Challenger.ID != ashaUser.ID || ch.Challenger.Username != "asha" || ch.Challenger.Rating != startingRating ||
		ch.Opponent.ID != raviUser.ID || ch.Opponent.Name != "Player ravi" ||
		!ch.PreferredStart.Equal(start.Add(45*time.Minute)) || ch.RespondedAt != nil ||
		ch.MyEntry != nil || ch.TheirEntryPaid || ch.Match != nil ||
		ch.Event.ID != "evt_t_chl" || ch.Event.FeePaise != 5000 || ch.Event.Venue.ID != "ven_t_entries" {
		t.Errorf("challenge = %+v", ch)
	}
	// The deadline: 4 hours from sending (the cutoff is a day away).
	if d := ch.ExpiresAt.Sub(ch.CreatedAt); d != challengeDeadline || ch.CreatedAt.Before(before.Add(-time.Second)) {
		t.Errorf("expires %v after %v", d, ch.CreatedAt)
	}

	// The JSON has exactly the keys the screens read.
	res, err := http.DefaultClient.Do(func() *http.Request {
		req, _ := http.NewRequest("GET", srv.URL+"/api/challenges/"+ch.ID, nil)
		req.AddCookie(asha.cookie)
		return req
	}())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	keys := func(m map[string]json.RawMessage) string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return strings.Join(ks, ",")
	}
	if got := keys(raw["challenge"]); got != "challenger,createdAt,event,expiresAt,format,id,match,myEntry,opponent,preferredStart,respondedAt,role,status,theirEntryPaid" {
		t.Errorf("challenge keys = %s", got)
	}
	var event, challenger map[string]json.RawMessage
	_ = json.Unmarshal(raw["challenge"]["event"], &event)
	_ = json.Unmarshal(raw["challenge"]["challenger"], &challenger)
	if got := keys(event); got != "currency,endsAt,feePaise,id,registrationClosesAt,startsAt,title,venue" {
		t.Errorf("event keys = %s", got)
	}
	if got := keys(challenger); got != "id,name,rating,username" {
		t.Errorf("player keys = %s", got)
	}

	// A cutoff sooner than 4 hours is the deadline.
	insertEvent(t, store, "evt_t_soon", testEvent{closesIn: time.Hour})
	soonStart := challengeEventStart(t, store, "evt_t_soon")
	soon := mustSend(t, asha, "evt_t_soon", "ravi", soonStart)
	if !soon.ExpiresAt.Equal(soon.Event.RegistrationClosesAt) {
		t.Errorf("expires %v, cutoff %v", soon.ExpiresAt, soon.Event.RegistrationClosesAt)
	}
}

func TestSendChallengeRejects(t *testing.T) {
	store, srv, _ := challengesTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_chl", testEvent{})
	start := challengeEventStart(t, store, "evt_t_chl")
	asha, ashaUser := player(t, store, srv, 1, "asha")
	ravi, raviUser := player(t, store, srv, 2, "ravi")
	kiranClient, _ := player(t, store, srv, 3, "kiran")
	cookie, _ := signIn(t, store, "+919800009999")
	noProfile := adminClient{t: t, srv: srv, cookie: cookie}
	_, ghost := signIn(t, store, "+919800009998")
	execSQL(t, store, `UPDATE users SET username = 'ghost' WHERE id = $1`, ghost.ID) // a username, no name

	for _, ev := range []struct {
		id string
		e  testEvent
	}{
		{"evt_t_draft", testEvent{status: EventDraft}},
		{"evt_t_cancelled", testEvent{status: EventCancelled}},
		{"evt_t_closed", testEvent{closesIn: -time.Minute}},
		{"evt_t_doubles", testEvent{formats: []string{FormatDoubles}}},
		{"evt_t_band", testEvent{ratingMin: intp(1300), ratingMax: intp(1500)}},
		{"evt_t_small", testEvent{capacity: 1}},
		{"evt_t_matches", testEvent{maxMatches: 1}},
	} {
		insertEvent(t, store, ev.id, ev.e)
		challengeEventStart(t, store, ev.id)
	}
	ok := start.Add(30 * time.Minute)
	send := func(c adminClient, body any) int {
		return c.do("POST", "/api/challenges", body, nil)
	}
	req := func(event, opponent, at string) map[string]string {
		return map[string]string{"eventId": event, "opponentUsername": opponent, "preferredStart": at}
	}
	okAt := ok.Format(time.RFC3339)
	for _, tc := range []struct {
		name string
		c    adminClient
		body any
		want int
	}{
		{"profile not set up", noProfile, req("evt_t_chl", "ravi", okAt), http.StatusForbidden},
		{"not JSON", asha, "{", http.StatusBadRequest},
		{"unknown field", asha, map[string]string{"eventId": "evt_t_chl", "opponentUsername": "ravi", "preferredStart": okAt, "format": "doubles"}, http.StatusBadRequest},
		{"bad time", asha, req("evt_t_chl", "ravi", "tomorrow at 7"), http.StatusBadRequest},
		{"before the event", asha, req("evt_t_chl", "ravi", start.Add(-time.Minute).Format(time.RFC3339)), http.StatusBadRequest},
		{"too late for a slot", asha, req("evt_t_chl", "ravi", start.Add(91*time.Minute).Format(time.RFC3339)), http.StatusBadRequest},
		{"yourself", asha, req("evt_t_chl", "@asha", okAt), http.StatusBadRequest},
		{"no singles", asha, req("evt_t_doubles", "ravi", okAt), http.StatusBadRequest},
		{"unknown opponent", asha, req("evt_t_chl", "nobody", okAt), http.StatusNotFound},
		{"unknown event", asha, req("evt_t_nope", "ravi", okAt), http.StatusNotFound},
		{"draft event", asha, req("evt_t_draft", "ravi", okAt), http.StatusNotFound},
		{"opponent without a profile", asha, req("evt_t_chl", "ghost", okAt), http.StatusConflict},
		{"cancelled event", asha, req("evt_t_cancelled", "ravi", okAt), http.StatusConflict},
		{"joining closed", asha, req("evt_t_closed", "ravi", okAt), http.StatusConflict},
		{"outside the band", asha, req("evt_t_band", "ravi", okAt), http.StatusConflict},
		{"no room for two", asha, req("evt_t_small", "ravi", okAt), http.StatusConflict},
	} {
		if code := send(tc.c, tc.body); code != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, code, tc.want)
		}
	}

	// The opponent outside the band: asha is in, ravi isn't.
	setRating(t, store, ashaUser.ID, FormatSingles, 1400)
	if _, code := sendChallenge(asha, "evt_t_band", "ravi", ok); code != http.StatusConflict {
		t.Errorf("opponent outside the band: %d", code)
	}
	// One match left but already used by an entry: a challenge needs a whole one.
	join(ravi, "evt_t_matches", singles, nil)
	if _, code := sendChallenge(asha, "evt_t_matches", "kiran", ok); code != http.StatusConflict {
		t.Errorf("no match left: %d", code)
	}

	// The entry limit, counting the challenge's entry: 3 already is too many,
	// for either player.
	for range maxEntriesPerPlayer {
		join(ravi, "evt_t_chl", singles, nil)
	}
	if _, code := sendChallenge(asha, "evt_t_chl", "ravi", ok); code != http.StatusConflict {
		t.Errorf("opponent at the entry limit: %d", code)
	}
	if _, code := sendChallenge(ravi, "evt_t_chl", "asha", ok); code != http.StatusConflict {
		t.Errorf("challenger at the entry limit: %d", code)
	}

	// One open challenge per pair per event, whoever sends it.
	kiran := mustSend(t, asha, "evt_t_chl", "kiran", ok)
	if _, code := sendChallenge(asha, "evt_t_chl", "kiran", ok); code != http.StatusConflict {
		t.Errorf("same challenge twice: %d", code)
	}
	if _, code := sendChallenge(kiranClient, "evt_t_chl", "asha", ok); code != http.StatusConflict {
		t.Errorf("the reverse challenge: %d", code)
	}
	// Once it's answered, another can be sent.
	mustAct(t, kiranClient, kiran.ID, "decline")
	mustSend(t, kiranClient, "evt_t_chl", "asha", ok)
	if n := countRows(t, store, `SELECT count(*) FROM challenges WHERE challenger_id = $1 OR opponent_id = $1`, raviUser.ID); n != 0 {
		t.Errorf("%d challenges with ravi saved by rejected requests", n)
	}
}

func TestAcceptChallenge(t *testing.T) {
	store, srv, _ := challengesTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_chl", testEvent{})
	start := challengeEventStart(t, store, "evt_t_chl")
	asha, ashaUser := player(t, store, srv, 1, "asha")
	ravi, raviUser := player(t, store, srv, 2, "ravi")
	kiran, _ := player(t, store, srv, 3, "kiran")
	before := spotsLeft(t, asha, "evt_t_chl")

	ch := mustSend(t, asha, "evt_t_chl", "ravi", start)
	if _, code := challengeAct(asha, ch.ID, "accept"); code != http.StatusForbidden {
		t.Errorf("challenger accepting: %d", code)
	}
	if _, code := challengeAct(kiran, ch.ID, "accept"); code != http.StatusNotFound {
		t.Errorf("stranger accepting: %d", code)
	}
	if _, code := getChallenge(kiran, ch.ID); code != http.StatusNotFound {
		t.Errorf("stranger reading: %d", code)
	}

	got := mustAct(t, ravi, ch.ID, "accept")
	if got.Status != ChallengeAccepted || got.Role != RoleOpponent || got.RespondedAt == nil ||
		got.MyEntry == nil || got.MyEntry.Status != EntryPendingPayment ||
		got.MyEntry.HoldExpiresAt == nil || !got.MyEntry.HoldExpiresAt.Equal(got.ExpiresAt) || got.TheirEntryPaid {
		t.Errorf("accepted = %+v", got)
	}
	if _, code := challengeAct(ravi, ch.ID, "accept"); code != http.StatusConflict {
		t.Errorf("accepting twice: %d", code)
	}
	// Two locked entries, held to the deadline, counting against the event.
	var n int
	if err := store.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM entries
		WHERE challenge_id = $1 AND kind = 'locked' AND status = 'pending_payment' AND formats = '{singles}'
		  AND hold_expires_at = $2 AND user_id IN ($3, $4)`,
		ch.ID, got.ExpiresAt, ashaUser.ID, raviUser.ID).Scan(&n); err != nil || n != 2 {
		t.Errorf("locked entries = %d, %v", n, err)
	}
	if left := spotsLeft(t, asha, "evt_t_chl"); left != before-2 {
		t.Errorf("spots left %d, want %d", left, before-2)
	}

	// The entry list shows it as a challenge's, and it can't be cancelled
	// or paid at the desk on its own.
	var mine entriesResponse
	asha.do("GET", "/api/me/entries", nil, &mine)
	if len(mine.Entries) != 1 || mine.Entries[0].Kind != EntryLocked || mine.Entries[0].ChallengeID == nil || *mine.Entries[0].ChallengeID != ch.ID {
		t.Fatalf("entries = %+v", mine.Entries)
	}
	if code := asha.do("DELETE", "/api/me/entries/"+mine.Entries[0].ID, nil, nil); code != http.StatusConflict {
		t.Errorf("cancelling a locked entry: %d", code)
	}
	if code := kiran.do("POST", "/api/admin/entries/"+mine.Entries[0].ID+"/mark-paid", nil, nil); code != http.StatusConflict {
		t.Errorf("marking a locked entry paid: %d", code)
	}

	// The challenger's view: the same entry, and nothing paid yet.
	view, _ := getChallenge(asha, ch.ID)
	if view.MyEntry == nil || view.MyEntry.ID != mine.Entries[0].ID || view.Role != RoleChallenger {
		t.Errorf("challenger's view = %+v", view)
	}
}

func TestAcceptChallengeRechecksRoom(t *testing.T) {
	store, srv, _ := challengesTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_cap", testEvent{capacity: 2})
	insertEvent(t, store, "evt_t_mm", testEvent{maxMatches: 2})
	insertEvent(t, store, "evt_t_lim", testEvent{})
	insertEvent(t, store, "evt_t_band", testEvent{ratingMax: intp(1500)})
	for _, id := range []string{"evt_t_cap", "evt_t_mm", "evt_t_lim", "evt_t_band"} {
		challengeEventStart(t, store, id)
	}
	asha, ashaUser := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")
	others := make([]adminClient, 3)
	for i := range others {
		others[i], _ = player(t, store, srv, 10+i, fmt.Sprintf("other%d", i))
	}
	starts := func(id string) time.Time {
		var s time.Time
		_ = store.pool.QueryRow(context.Background(), `SELECT starts_at FROM events WHERE id = $1`, id).Scan(&s)
		return s
	}

	// Capacity: room for two when sent, not once someone else joins.
	capCh := mustSend(t, asha, "evt_t_cap", "ravi", starts("evt_t_cap"))
	join(others[0], "evt_t_cap", singles, nil)
	if _, code := challengeAct(ravi, capCh.ID, "accept"); code != http.StatusConflict {
		t.Errorf("full event: %d", code)
	}
	// max_matches 2 is four half-matches; three entries leave one.
	mmCh := mustSend(t, asha, "evt_t_mm", "ravi", starts("evt_t_mm"))
	for _, c := range others {
		join(c, "evt_t_mm", singles, nil)
	}
	if _, code := challengeAct(ravi, mmCh.ID, "accept"); code != http.StatusConflict {
		t.Errorf("no match left: %d", code)
	}
	// The entry limit, for either player.
	limCh := mustSend(t, asha, "evt_t_lim", "ravi", starts("evt_t_lim"))
	for range maxEntriesPerPlayer {
		join(asha, "evt_t_lim", singles, nil)
	}
	if _, code := challengeAct(ravi, limCh.ID, "accept"); code != http.StatusConflict {
		t.Errorf("challenger at the limit: %d", code)
	}
	// The band, rechecked: asha's rating rose since.
	bandCh := mustSend(t, asha, "evt_t_band", "ravi", starts("evt_t_band"))
	setRating(t, store, ashaUser.ID, FormatSingles, 1600)
	if _, code := challengeAct(ravi, bandCh.ID, "accept"); code != http.StatusConflict {
		t.Errorf("challenger outside the band: %d", code)
	}
	// None of them made entries, and all are still pending.
	if n := countRows(t, store, `SELECT count(*) FROM entries WHERE kind = 'locked'`); n != 0 {
		t.Errorf("%d locked entries", n)
	}
	for _, id := range []string{capCh.ID, mmCh.ID, limCh.ID, bandCh.ID} {
		if s := challengeStatus(t, store, id); s != ChallengePending {
			t.Errorf("challenge %s is %s", id, s)
		}
	}
	// The event cancelled meanwhile.
	execSQL(t, store, `UPDATE events SET status = 'cancelled' WHERE id = 'evt_t_cap'`)
	if _, code := challengeAct(ravi, capCh.ID, "accept"); code != http.StatusConflict {
		t.Errorf("cancelled event: %d", code)
	}
}

func TestDeclineAndWithdraw(t *testing.T) {
	provider := newTestProvider()
	store, srv, _ := challengesTestServer(t, provider)
	insertEvent(t, store, "evt_t_chl", testEvent{})
	start := challengeEventStart(t, store, "evt_t_chl")
	asha, ashaUser := player(t, store, srv, 1, "asha")
	ravi, raviUser := player(t, store, srv, 2, "ravi")

	// Declining: the opponent, while pending.
	ch := mustSend(t, asha, "evt_t_chl", "ravi", start)
	if _, code := challengeAct(asha, ch.ID, "decline"); code != http.StatusForbidden {
		t.Errorf("challenger declining: %d", code)
	}
	if _, code := challengeAct(ravi, ch.ID, "withdraw"); code != http.StatusConflict {
		t.Errorf("opponent withdrawing before accepting: %d", code)
	}
	declined := mustAct(t, ravi, ch.ID, "decline")
	if declined.Status != ChallengeDeclined || declined.RespondedAt == nil {
		t.Errorf("declined = %+v", declined)
	}
	if _, code := challengeAct(ravi, ch.ID, "decline"); code != http.StatusConflict {
		t.Errorf("declining twice: %d", code)
	}
	if _, code := challengeAct(ravi, ch.ID, "accept"); code != http.StatusConflict {
		t.Errorf("accepting after declining: %d", code)
	}

	// Withdrawing while pending: the challenger.
	ch = mustSend(t, asha, "evt_t_chl", "ravi", start)
	if w := mustAct(t, asha, ch.ID, "withdraw"); w.Status != ChallengeWithdrawn || w.RespondedAt != nil {
		t.Errorf("withdrawn = %+v", w)
	}

	// Withdrawing once accepted, after one has paid: the opponent can too.
	// The payer is refunded, the other entry cancelled.
	ch = mustSend(t, asha, "evt_t_chl", "ravi", start)
	mustAct(t, ravi, ch.ID, "accept")
	paid := payLocked(t, asha, ch, "pay_fake_wd_asha")
	if paid.Status != EntryConfirmed {
		t.Fatalf("paid entry = %+v", paid)
	}
	if theirs, _ := getChallenge(ravi, ch.ID); !theirs.TheirEntryPaid {
		t.Errorf("ravi doesn't see asha paid: %+v", theirs)
	}
	w := mustAct(t, ravi, ch.ID, "withdraw")
	if w.Status != ChallengeWithdrawn || w.MyEntry == nil || w.MyEntry.Status != EntryCancelled {
		t.Errorf("withdrawn = %+v", w)
	}
	if got := challengeEntries(t, store, ch.ID); got[ashaUser.ID] != EntryRefunded || got[raviUser.ID] != EntryCancelled {
		t.Errorf("entries = %v", got)
	}
	if rows := paymentRows(t, store, paid.ID); statuses(rows) != PaymentRefunded {
		t.Errorf("asha's payments = %s", statuses(rows))
	}
	if !slices.Contains(provider.refunded(), "pay_fake_wd_asha") {
		t.Errorf("refunds = %v", provider.refunded())
	}
	if _, code := challengeAct(asha, ch.ID, "withdraw"); code != http.StatusConflict {
		t.Errorf("withdrawing twice: %d", code)
	}
	// The freed places count again.
	if n := countRows(t, store, `SELECT count(*) FROM entries WHERE event_id = 'evt_t_chl'
		AND (status IN ('confirmed', 'standby', 'scheduled') OR (status = 'pending_payment' AND hold_expires_at > now()))`); n != 0 {
		t.Errorf("%d live entries left", n)
	}
}

// challengeFixture is an event with courts and two judges, and asha and
// ravi with an accepted challenge.
type challengeFixture struct {
	store       *PostgresStore
	srv         *httptest.Server
	api         *API
	provider    *testProvider
	start       time.Time
	asha, ravi  adminClient
	ashaU, ravU User
	judges      []User
	ch          Challenge
}

func newChallengeFixture(t *testing.T, courts int, preferred time.Duration) challengeFixture {
	t.Helper()
	f := challengeFixture{provider: newTestProvider()}
	f.store, f.srv, f.api = challengesTestServer(t, f.provider)
	insertEvent(t, f.store, "evt_t_fix", testEvent{})
	f.start = challengeEventStart(t, f.store, "evt_t_fix")
	addCourts(t, f.store, "evt_t_fix", courts)
	f.asha, f.ashaU = player(t, f.store, f.srv, 1, "asha")
	f.ravi, f.ravU = player(t, f.store, f.srv, 2, "ravi")
	for i, name := range []string{"umpire", "referee"} {
		_, u := player(t, f.store, f.srv, 90+i, name)
		addJudge(t, f.store, "evt_t_fix", u.ID)
		f.judges = append(f.judges, u)
	}
	f.ch = mustSend(t, f.asha, "evt_t_fix", "ravi", f.start.Add(preferred))
	mustAct(t, f.ravi, f.ch.ID, "accept")
	return f
}

// insertMatch adds a match straight into the table, on a court of
// evt_t_fix (or with event NULL), for the users given.
func insertMatch(t *testing.T, store *PostgresStore, id, eventID, court string, from, to time.Time, judge any, users ...string) {
	t.Helper()
	var event any
	if eventID != "" {
		event = eventID
	}
	execSQL(t, store, `INSERT INTO matches (id, event_id, format, court_id, starts_at, ends_at, judge_id)
		VALUES ($1, $2, 'singles', $3, $4, $5, $6)`, id, event, court, from, to, judge)
	for i, u := range users {
		execSQL(t, store, `INSERT INTO match_sides (match_id, side, user_id) VALUES ($1, $2, $3)`, id, sideLabel(i%2), u)
	}
}

func TestPayBothFixesTheMatch(t *testing.T) {
	f := newChallengeFixture(t, 2, time.Hour)
	// At the preferred time Court 1 is taken by a match the first judge (by
	// ID) is judging, so the challenge gets Court 2 and the other judge.
	judges := []User{f.judges[0], f.judges[1]}
	sort.Slice(judges, func(i, j int) bool { return judges[i].ID < judges[j].ID })
	_, p := player(t, f.store, f.srv, 3, "kiran")
	_, q := player(t, f.store, f.srv, 4, "neha")
	insertMatch(t, f.store, "mat_t_busy", "evt_t_fix", "crt_t_entries_1",
		f.start.Add(time.Hour), f.start.Add(90*time.Minute), judges[0].ID, p.ID, q.ID)

	// One payment: confirmed, nothing scheduled yet.
	first := payLocked(t, f.asha, f.ch, "pay_fake_fix_asha")
	if first.Status != EntryConfirmed || first.Kind != EntryLocked {
		t.Fatalf("first payment left %+v", first)
	}
	if n := countRows(t, f.store, `SELECT count(*) FROM matches WHERE id <> 'mat_t_busy'`); n != 0 {
		t.Fatalf("%d matches after one payment", n)
	}
	if v, _ := getChallenge(f.asha, f.ch.ID); v.Status != ChallengeAccepted || v.TheirEntryPaid || v.Match != nil {
		t.Errorf("after one payment = %+v", v)
	}

	// The second, through the webhook: the match is fixed at once.
	mine, _ := getChallenge(f.ravi, f.ch.ID)
	order := checkout(t, f.ravi, mine.MyEntry.ID)
	if code, status := webhook(t, f.srv, "payment.captured", order, "pay_fake_fix_ravi"); code != http.StatusOK || status != "applied" {
		t.Fatalf("webhook: %d %s", code, status)
	}
	got, _ := getChallenge(f.ravi, f.ch.ID)
	if got.Status != ChallengeScheduled || got.Match == nil || got.MyEntry.Status != EntryScheduled || !got.TheirEntryPaid {
		t.Fatalf("after both paid = %+v", got)
	}
	m := got.Match
	if m.Court.ID != "crt_t_entries_2" || m.Court.Label != "Court 2" ||
		!m.StartsAt.Equal(f.start.Add(time.Hour)) || !m.EndsAt.Equal(f.start.Add(90*time.Minute)) ||
		m.Judge == nil || m.Judge.UserID != judges[1].ID || m.Judge.Username == "" {
		t.Errorf("match = %+v (judge %+v)", m, m.Judge)
	}
	// Both entries scheduled into it, the challenger on side A.
	if n := countRows(t, f.store, `SELECT count(*) FROM entries WHERE challenge_id = $1 AND status = 'scheduled' AND match_id = $2`,
		f.ch.ID, m.ID); n != 2 {
		t.Errorf("%d entries scheduled into the match", n)
	}
	if n := countRows(t, f.store, `SELECT count(*) FROM match_sides WHERE match_id = $1 AND side = 'A' AND user_id = $2`,
		m.ID, f.ashaU.ID); n != 1 {
		t.Error("the challenger isn't side A")
	}

	// It's in both players' matches, marked as the challenge's.
	var matches matchesResponse
	f.asha.do("GET", "/api/me/matches", nil, &matches)
	if len(matches.Matches) != 1 || matches.Matches[0].ID != m.ID || matches.Matches[0].ChallengeID == nil ||
		*matches.Matches[0].ChallengeID != f.ch.ID || len(matches.Matches[0].Sides) != 2 {
		t.Errorf("asha's matches = %+v", matches.Matches)
	}
	// Scheduled: nobody can withdraw now, and a repeat webhook changes nothing.
	if _, code := challengeAct(f.asha, f.ch.ID, "withdraw"); code != http.StatusConflict {
		t.Errorf("withdrawing a scheduled challenge: %d", code)
	}
	if _, status := webhook(t, f.srv, "payment.captured", order, "pay_fake_fix_ravi"); status != "already applied" {
		t.Errorf("repeat webhook: %s", status)
	}
	if n := countRows(t, f.store, `SELECT count(*) FROM matches`); n != 2 {
		t.Errorf("%d matches, want 2", n)
	}
}

func TestChallengeSlotAvoidsOtherMatches(t *testing.T) {
	f := newChallengeFixture(t, 2, time.Hour)
	_, p := player(t, f.store, f.srv, 3, "kiran")
	_, q := player(t, f.store, f.srv, 4, "neha")
	// Asha already plays at the preferred time (on Court 2), so the nearest
	// starts are 30 minutes clear either side: 19:00 or 21:00 → 19:00, the
	// earlier. Court 1 is taken then by another match, so Court 2.
	insertMatch(t, f.store, "mat_t_asha", "evt_t_fix", "crt_t_entries_2",
		f.start.Add(time.Hour), f.start.Add(90*time.Minute), nil, f.ashaU.ID, p.ID)
	insertMatch(t, f.store, "mat_t_other", "evt_t_fix", "crt_t_entries_1",
		f.start, f.start.Add(30*time.Minute), nil, q.ID, p.ID)

	payLocked(t, f.asha, f.ch, "pay_fake_av_asha")
	payLocked(t, f.ravi, f.ch, "pay_fake_av_ravi")
	got, _ := getChallenge(f.asha, f.ch.ID)
	if got.Match == nil || !got.Match.StartsAt.Equal(f.start) || got.Match.Court.ID != "crt_t_entries_2" {
		t.Fatalf("challenge = %+v, match %+v", got, got.Match)
	}
}

func TestChallengeWithNoSlotFails(t *testing.T) {
	f := newChallengeFixture(t, 1, 30*time.Minute)
	_, p := player(t, f.store, f.srv, 3, "kiran")
	_, q := player(t, f.store, f.srv, 4, "neha")
	// The only court is taken for the whole event.
	insertMatch(t, f.store, "mat_t_full", "evt_t_fix", "crt_t_entries_1", f.start, f.start.Add(2*time.Hour), nil, p.ID, q.ID)

	first := payLocked(t, f.asha, f.ch, "pay_fake_ns_asha")
	second := payLocked(t, f.ravi, f.ch, "pay_fake_ns_ravi")
	if first.Status != EntryConfirmed || second.Status != EntryRefunded {
		t.Errorf("entries after paying: %s, %s", first.Status, second.Status)
	}
	if s := challengeStatus(t, f.store, f.ch.ID); s != ChallengeFailed {
		t.Errorf("challenge = %s", s)
	}
	if got := challengeEntries(t, f.store, f.ch.ID); got[f.ashaU.ID] != EntryRefunded || got[f.ravU.ID] != EntryRefunded {
		t.Errorf("entries = %v", got)
	}
	for _, id := range []string{first.ID, second.ID} {
		if rows := paymentRows(t, f.store, id); statuses(rows) != PaymentRefunded {
			t.Errorf("payments of %s = %s", id, statuses(rows))
		}
	}
	refunds := f.provider.refunded()
	if !slices.Contains(refunds, "pay_fake_ns_asha") || !slices.Contains(refunds, "pay_fake_ns_ravi") {
		t.Errorf("refunds = %v", refunds)
	}
	if n := countRows(t, f.store, `SELECT count(*) FROM matches`); n != 1 {
		t.Errorf("%d matches", n)
	}
}

func TestChallengeExpiresWithOnePaid(t *testing.T) {
	f := newChallengeFixture(t, 2, time.Hour)
	paid := payLocked(t, f.asha, f.ch, "pay_fake_ex_asha")

	// The sweeper, with a clock: nothing is due a minute before the
	// deadline; at it, the challenge expires and asha is refunded.
	ctx := context.Background()
	n, refunds, err := f.store.ExpireChallenges(ctx, f.ch.ExpiresAt.Add(-time.Minute), "")
	if err != nil || n != 0 || len(refunds) != 0 {
		t.Fatalf("early sweep: %d, %v, %v", n, refunds, err)
	}
	f.api.expireChallenges(ctx, f.ch.ExpiresAt)
	if s := challengeStatus(t, f.store, f.ch.ID); s != ChallengeExpired {
		t.Fatalf("challenge = %s", s)
	}
	if got := challengeEntries(t, f.store, f.ch.ID); got[f.ashaU.ID] != EntryRefunded || got[f.ravU.ID] != EntryCancelled {
		t.Errorf("entries = %v", got)
	}
	if rows := paymentRows(t, f.store, paid.ID); statuses(rows) != PaymentRefunded {
		t.Errorf("asha's payments = %s", statuses(rows))
	}
	if !slices.Contains(f.provider.refunded(), "pay_fake_ex_asha") {
		t.Errorf("refunds = %v", f.provider.refunded())
	}
	// A second sweep finds nothing; acting on it is too late.
	if n, _, _ := f.store.ExpireChallenges(ctx, f.ch.ExpiresAt.Add(time.Hour), ""); n != 0 {
		t.Errorf("second sweep expired %d", n)
	}
	if _, code := challengeAct(f.asha, f.ch.ID, "withdraw"); code != http.StatusConflict {
		t.Errorf("withdrawing an expired challenge: %d", code)
	}
	// Never turned into open entries.
	if n := countRows(t, f.store, `SELECT count(*) FROM entries WHERE kind = 'open'`); n != 0 {
		t.Errorf("%d open entries", n)
	}
}

func TestChallengeExpiresLazily(t *testing.T) {
	f := newChallengeFixture(t, 2, time.Hour)
	paid := payLocked(t, f.asha, f.ch, "pay_fake_lz_asha")
	// Ravi starts paying, then the deadline passes with nobody sweeping.
	mine, _ := getChallenge(f.ravi, f.ch.ID)
	order := checkout(t, f.ravi, mine.MyEntry.ID)
	makeDue(t, f.store, f.ch.ID)

	// Reading it expires it, and refunds asha.
	got, code := getChallenge(f.asha, f.ch.ID)
	if code != http.StatusOK || got.Status != ChallengeExpired || got.MyEntry.Status != EntryRefunded {
		t.Fatalf("read = %d %+v", code, got)
	}
	if rows := paymentRows(t, f.store, paid.ID); statuses(rows) != PaymentRefunded {
		t.Errorf("asha's payments = %s", statuses(rows))
	}
	// Ravi's payment lands late: refunded, no match.
	en, code := fakePay(f.ravi, order, "pay_fake_lz_ravi")
	if code != http.StatusOK || en.Status != EntryRefunded {
		t.Errorf("late payment: %d %+v", code, en)
	}
	if !slices.Contains(f.provider.refunded(), "pay_fake_lz_ravi") {
		t.Errorf("refunds = %v", f.provider.refunded())
	}

	// The list expires too: a pending one nobody answered.
	other := mustSend(t, f.asha, "evt_t_fix", "umpire", f.start)
	makeDue(t, f.store, other.ID)
	var list challengesResponse
	if code := f.asha.do("GET", "/api/me/challenges", nil, &list); code != http.StatusOK || len(list.Challenges) != 2 {
		t.Fatalf("list: %d %+v", code, list)
	}
	for _, c := range list.Challenges {
		if c.Status != ChallengeExpired {
			t.Errorf("%s is %s", c.ID, c.Status)
		}
	}
	if n := countRows(t, f.store, `SELECT count(*) FROM matches`); n != 0 {
		t.Errorf("%d matches", n)
	}
}

func TestPaymentLandingAfterDeadlineExpiresTheChallenge(t *testing.T) {
	f := newChallengeFixture(t, 2, time.Hour)
	paid := payLocked(t, f.asha, f.ch, "pay_fake_pl_asha")
	mine, _ := getChallenge(f.ravi, f.ch.ID)
	order := checkout(t, f.ravi, mine.MyEntry.ID)
	makeDue(t, f.store, f.ch.ID)

	// Nobody read it: the webhook for ravi's payment finds it due, expires
	// it, and refunds both.
	if code, status := webhook(t, f.srv, "payment.captured", order, "pay_fake_pl_ravi"); code != http.StatusOK || status != "applied" {
		t.Fatalf("webhook: %d %s", code, status)
	}
	if s := challengeStatus(t, f.store, f.ch.ID); s != ChallengeExpired {
		t.Errorf("challenge = %s", s)
	}
	if rows := paymentRows(t, f.store, paid.ID); statuses(rows) != PaymentRefunded {
		t.Errorf("asha's payments = %s", statuses(rows))
	}
	refunds := f.provider.refunded()
	if !slices.Contains(refunds, "pay_fake_pl_asha") || !slices.Contains(refunds, "pay_fake_pl_ravi") {
		t.Errorf("refunds = %v", refunds)
	}
}

func TestBothPaymentsAtOnceMakeOneMatch(t *testing.T) {
	for round := range 3 {
		f := newChallengeFixture(t, 2, time.Hour)
		var orders [2]string
		for i, c := range []adminClient{f.asha, f.ravi} {
			mine, _ := getChallenge(c, f.ch.ID)
			orders[i] = checkout(t, c, mine.MyEntry.ID)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		codes := make(chan int, 4)
		for i := range orders {
			// The page's verify and the webhook for each payment, all at once.
			paymentID := fmt.Sprintf("pay_fake_race_%d_%d", round, i)
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				body := webhookBody("payment.captured", orders[i], paymentID, 5000)
				req, _ := http.NewRequest("POST", f.srv.URL+"/api/payments/webhook", strings.NewReader(string(body)))
				req.Header.Set("X-Razorpay-Signature", signWebhook(testWebhookSecret, body))
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Error(err)
					return
				}
				_ = res.Body.Close()
				codes <- res.StatusCode
			}()
			go func() {
				defer wg.Done()
				<-start
				c := []adminClient{f.asha, f.ravi}[i]
				body := fmt.Sprintf(`{"orderId":%q,"paymentId":%q,"signature":%q}`,
					orders[i], paymentID, payments.FakeSignature(orders[i], paymentID))
				req, _ := http.NewRequest("POST", f.srv.URL+"/api/payments/verify", strings.NewReader(body))
				req.AddCookie(c.cookie)
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Error(err)
					return
				}
				_ = res.Body.Close()
				codes <- res.StatusCode
			}()
		}
		close(start)
		wg.Wait()
		close(codes)
		for code := range codes {
			if code != http.StatusOK {
				t.Errorf("round %d: status %d", round, code)
			}
		}
		if n := countRows(t, f.store, `SELECT count(*) FROM matches`); n != 1 {
			t.Errorf("round %d: %d matches, want exactly 1", round, n)
		}
		if s := challengeStatus(t, f.store, f.ch.ID); s != ChallengeScheduled {
			t.Errorf("round %d: challenge %s", round, s)
		}
		if n := countRows(t, f.store, `SELECT count(*) FROM payments WHERE status = 'paid'`); n != 2 {
			t.Errorf("round %d: %d paid payments", round, n)
		}
	}
}

func TestMatcherWorksAroundChallengeMatches(t *testing.T) {
	// One court, two hours: four singles slots. The challenge takes 19:00.
	f := newChallengeFixture(t, 1, 0)
	payLocked(t, f.asha, f.ch, "pay_fake_mw_asha")
	payLocked(t, f.ravi, f.ch, "pay_fake_mw_ravi")
	fixed, _ := getChallenge(f.asha, f.ch.ID)
	if fixed.Match == nil || !fixed.Match.StartsAt.Equal(f.start) {
		t.Fatalf("challenge match = %+v", fixed.Match)
	}

	// A second challenge, accepted and paid by one side only: its locked
	// entry is confirmed but never in the pool.
	kiran, _ := player(t, f.store, f.srv, 3, "kiran")
	half := mustSend(t, f.ravi, "evt_t_fix", "kiran", f.start.Add(90*time.Minute))
	mustAct(t, kiran, half.ID, "accept")
	halfPaid := payLocked(t, f.ravi, half, "pay_fake_mw_half")

	// Open entries: asha again, and two more.
	admin, _ := player(t, f.store, f.srv, 50, "desk")
	openIDs := map[string]bool{}
	for _, c := range []adminClient{f.asha, kiran} {
		openIDs[joinPaid(t, c, admin, "evt_t_fix", singles).ID] = true
	}
	neha, _ := player(t, f.store, f.srv, 4, "neha")
	openIDs[joinPaid(t, neha, admin, "evt_t_fix", singles).ID] = true
	mohan, _ := player(t, f.store, f.srv, 5, "mohan")
	openIDs[joinPaid(t, mohan, admin, "evt_t_fix", singles).ID] = true

	check := func(view ScheduleView, what string) {
		t.Helper()
		if len(view.Matches) != 2 {
			t.Fatalf("%s: %d matches: %+v", what, len(view.Matches), view)
		}
		for _, m := range view.Matches {
			if m.StartsAt.Before(f.start.Add(30 * time.Minute)) {
				t.Errorf("%s: a match at %v reuses the challenge's court time", what, m.StartsAt)
			}
			for _, s := range m.Sides {
				for _, p := range s.Players {
					if !openIDs[p.EntryID] {
						t.Errorf("%s: entry %s isn't an open one", what, p.EntryID)
					}
					// Asha played at 19:00: not before 20:00.
					if p.UserID == f.ashaU.ID && m.StartsAt.Before(f.start.Add(time.Hour)) {
						t.Errorf("%s: asha plays again at %v", what, m.StartsAt)
					}
				}
			}
		}
		for _, s := range view.Standby {
			if s.EntryID == halfPaid.ID {
				t.Errorf("%s: the half-paid locked entry is on standby", what)
			}
		}
		if !slices.ContainsFunc(view.Warnings, func(w string) bool { return strings.Contains(w, "already scheduled") }) {
			t.Errorf("%s: warnings = %v", what, view.Warnings)
		}
	}
	var preview scheduleResponse
	if code := admin.do("POST", "/api/admin/events/evt_t_fix/schedule/preview", nil, &preview); code != http.StatusOK {
		t.Fatalf("preview: %d", code)
	}
	check(preview.Schedule, "preview")
	var published scheduleResponse
	if code := admin.do("POST", "/api/admin/events/evt_t_fix/schedule/publish", nil, &published); code != http.StatusCreated {
		t.Fatalf("publish: %d", code)
	}
	check(published.Schedule, "publish")
	// The challenge's match is untouched, and the half-paid entry still waits.
	if again, _ := getChallenge(f.asha, f.ch.ID); again.Match == nil || again.Match.ID != fixed.Match.ID || !again.Match.StartsAt.Equal(f.start) {
		t.Errorf("challenge match moved: %+v", again.Match)
	}
	if s := entryStatus(t, f.store, halfPaid.ID); s != EntryConfirmed {
		t.Errorf("half-paid locked entry = %s", s)
	}
	// The published schedule shows all three.
	var saved scheduleResponse
	if code := admin.do("GET", "/api/admin/events/evt_t_fix/schedule", nil, &saved); code != http.StatusOK || len(saved.Schedule.Matches) != 3 {
		t.Errorf("saved schedule: %d, %d matches", code, len(saved.Schedule.Matches))
	}
}

func TestFreeChallenge(t *testing.T) {
	store, srv, _ := challengesTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_free", testEvent{})
	execSQL(t, store, `UPDATE events SET fee_paise = 0 WHERE id = 'evt_t_free'`)
	start := challengeEventStart(t, store, "evt_t_free")
	addCourts(t, store, "evt_t_free", 1)
	asha, _ := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")
	ch := mustSend(t, asha, "evt_t_free", "ravi", start.Add(30*time.Minute))
	mustAct(t, ravi, ch.ID, "accept")
	for _, c := range []adminClient{asha, ravi} {
		mine, _ := getChallenge(c, ch.ID)
		if out, code := pay(c, mine.MyEntry.ID); code != http.StatusOK || out.Entry == nil {
			t.Fatalf("free pay: %d %+v", code, out)
		}
	}
	got, _ := getChallenge(asha, ch.ID)
	if got.Status != ChallengeScheduled || got.Match == nil || !got.Match.StartsAt.Equal(start.Add(30*time.Minute)) ||
		got.Match.Judge != nil {
		t.Errorf("free challenge = %+v, match %+v", got, got.Match)
	}
}

func TestListChallenges(t *testing.T) {
	store, srv, _ := challengesTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_chl", testEvent{})
	start := challengeEventStart(t, store, "evt_t_chl")
	asha, _ := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")
	kiran, _ := player(t, store, srv, 3, "kiran")
	player(t, store, srv, 4, "neha")

	declined := mustSend(t, asha, "evt_t_chl", "ravi", start)
	mustAct(t, ravi, declined.ID, "decline")
	received := mustSend(t, kiran, "evt_t_chl", "asha", start)
	sent := mustSend(t, asha, "evt_t_chl", "neha", start)
	// Spread the creation times so "newest" is unambiguous.
	for i, id := range []string{declined.ID, received.ID, sent.ID} {
		execSQL(t, store, `UPDATE challenges SET created_at = now() - make_interval(mins => $2) WHERE id = $1`, id, 10-i)
	}

	var list challengesResponse
	if code := asha.do("GET", "/api/me/challenges", nil, &list); code != http.StatusOK {
		t.Fatalf("list: %d", code)
	}
	var got []string
	for _, c := range list.Challenges {
		got = append(got, c.ID+":"+c.Role)
	}
	want := []string{sent.ID + ":challenger", received.ID + ":opponent", declined.ID + ":challenger"}
	if !slices.Equal(got, want) {
		t.Errorf("list = %v, want open ones newest first, then the rest: %v", got, want)
	}
	var none challengesResponse
	if code := kiran.do("GET", "/api/me/challenges", nil, &none); code != http.StatusOK || len(none.Challenges) != 1 {
		t.Errorf("kiran's list: %d %d", code, len(none.Challenges))
	}
}

func TestRecentPlayers(t *testing.T) {
	store, srv, _ := challengesTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_chl", testEvent{})
	start := challengeEventStart(t, store, "evt_t_chl")
	addCourts(t, store, "evt_t_chl", 1)
	asha, ashaU := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")
	_, kiranU := player(t, store, srv, 3, "kiran")
	_, nehaU := player(t, store, srv, 4, "neha")
	stranger, _ := player(t, store, srv, 5, "stranger")
	setRating(t, store, nehaU.ID, FormatSingles, 1400)

	// Ravi challenged asha (declined); asha challenged kiran; asha played
	// neha in a match that started a week ago.
	c1 := mustSend(t, ravi, "evt_t_chl", "asha", start)
	mustAct(t, asha, c1.ID, "decline")
	c2 := mustSend(t, asha, "evt_t_chl", "kiran", start)
	execSQL(t, store, `UPDATE challenges SET created_at = now() - interval '1 hour' WHERE id = $1`, c1.ID)
	execSQL(t, store, `UPDATE challenges SET created_at = now() - interval '2 hours' WHERE id = $1`, c2.ID)
	insertMatch(t, store, "mat_t_old", "", "crt_t_entries_1",
		time.Now().Add(-7*24*time.Hour), time.Now().Add(-7*24*time.Hour+30*time.Minute), nil, ashaU.ID, nehaU.ID)

	var got playersResponse
	if code := asha.do("GET", "/api/me/recent-players", nil, &got); code != http.StatusOK {
		t.Fatalf("recent: %d", code)
	}
	var names []string
	for _, p := range got.Players {
		names = append(names, fmt.Sprintf("%s:%d", p.Username, p.Rating))
	}
	if want := []string{"ravi:1200", "kiran:1200", "neha:1400"}; !slices.Equal(names, want) {
		t.Errorf("recent players = %v, want %v", names, want)
	}
	if got.Players[1].ID != kiranU.ID || got.Players[1].Name != "Player kiran" {
		t.Errorf("player = %+v", got.Players[1])
	}
	var empty playersResponse
	if code := stranger.do("GET", "/api/me/recent-players", nil, &empty); code != http.StatusOK || empty.Players == nil || len(empty.Players) != 0 {
		t.Errorf("stranger: %d %+v", code, empty.Players)
	}
}

func TestNearbyPlayers(t *testing.T) {
	store, srv, _ := challengesTestServer(t, newTestProvider())
	// The entries venue in Koramangala; another 300 km away.
	insertEvent(t, store, "evt_t_near", testEvent{})
	execSQL(t, store, `UPDATE venues SET lat = 12.935, lng = 77.624 WHERE id = 'ven_t_entries'`)
	execSQL(t, store, `INSERT INTO venues (id, name, city, lat, lng, timezone)
		VALUES ('ven_t_far', 'Far Club', 'chennai', 13.08, 80.27, 'Asia/Kolkata')`)
	execSQL(t, store, `INSERT INTO events (id, venue_id, title, formats, starts_at, ends_at, registration_closes_at,
			fee_paise, capacity, max_matches, status)
		VALUES ('evt_t_far', 'ven_t_far', 'Far', '{singles}', now() + interval '3 days', now() + interval '3 days 2 hours',
			now() + interval '2 days', 5000, 16, 8, 'published')`)

	me, meU := player(t, store, srv, 1, "me")
	setRating(t, store, meU.ID, FormatSingles, 1250)
	type p struct {
		name   string
		rating int
		event  string
	}
	clients := map[string]adminClient{}
	users := map[string]User{}
	for i, x := range []p{
		{"same", 1250, "evt_t_near"},  // 0 apart
		{"older", 1240, "evt_t_near"}, // 10 apart, joined first
		{"newer", 1260, "evt_t_near"}, // 10 apart, joined later
		{"far_rated", 1500, "evt_t_near"},
		{"faraway", 1250, "evt_t_far"}, // too far
		{"cancelled", 1250, "evt_t_near"},
		{"stale", 1250, "evt_t_near"},
		{"unrated", 0, "evt_t_near"}, // 1200: 50 apart
	} {
		c, u := player(t, store, srv, 10+i, x.name)
		clients[x.name], users[x.name] = c, u
		if x.rating > 0 {
			setRating(t, store, u.ID, FormatSingles, x.rating)
		}
		var en entryResponse
		if code := join(c, x.event, singles, &en); code != http.StatusCreated {
			t.Fatalf("join %s: %d", x.name, code)
		}
		execSQL(t, store, `UPDATE entries SET created_at = now() - make_interval(mins => $2) WHERE id = $1`, en.Entry.ID, 100-i)
		switch x.name {
		case "cancelled":
			clients[x.name].do("DELETE", "/api/me/entries/"+en.Entry.ID, nil, nil)
		case "stale":
			execSQL(t, store, `UPDATE entries SET created_at = now() - interval '61 days' WHERE id = $1`, en.Entry.ID)
		}
	}
	// My own entry doesn't make me a suggestion.
	join(me, "evt_t_near", singles, nil)

	var got playersResponse
	if code := me.do("GET", "/api/players/nearby?lat=12.93&lng=77.62&radiusKm=5", nil, &got); code != http.StatusOK {
		t.Fatalf("nearby: %d", code)
	}
	var names []string
	for _, pl := range got.Players {
		names = append(names, pl.Username)
	}
	if want := []string{"same", "newer", "older", "unrated", "far_rated"}; !slices.Equal(names, want) {
		t.Errorf("nearby = %v, want %v", names, want)
	}
	// A wide radius reaches the far venue too.
	me.do("GET", "/api/players/nearby?lat=12.93&lng=77.62&radiusKm=100", nil, &got)
	if slices.ContainsFunc(got.Players, func(pl Player) bool { return pl.Username == "faraway" }) {
		t.Error("300 km away is within 100 km")
	}
	for _, bad := range []string{"", "?lat=12.9", "?lat=12.9&lng=77.6&radiusKm=101", "?lat=91&lng=0", "?radiusKm=5"} {
		if code := me.do("GET", "/api/players/nearby"+bad, nil, nil); code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", bad, code)
		}
	}
}

func TestChallengesTableRejectsBadRows(t *testing.T) {
	store, srv, _ := challengesTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_chl", testEvent{})
	_, a := player(t, store, srv, 1, "asha")
	_, b := player(t, store, srv, 2, "ravi")
	ctx := context.Background()
	insert := func(id, challenger, opponent, status string) error {
		_, err := store.pool.Exec(ctx, `
			INSERT INTO challenges (id, event_id, challenger_id, opponent_id, preferred_start, status, expires_at)
			VALUES ($1, 'evt_t_chl', $2, $3, now(), $4, now())`, id, challenger, opponent, status)
		return err
	}
	if err := insert("chl_t_self", a.ID, a.ID, ChallengePending); err == nil {
		t.Error("a challenge against yourself was saved")
	}
	if err := insert("chl_t_sched", a.ID, b.ID, ChallengeScheduled); err == nil {
		t.Error("a scheduled challenge without a match was saved")
	}
	if err := insert("chl_t_1", a.ID, b.ID, ChallengePending); err != nil {
		t.Fatal(err)
	}
	if err := insert("chl_t_2", b.ID, a.ID, ChallengePending); !isUniqueViolation(err) {
		t.Errorf("a second open challenge for the pair: %v", err)
	}
	if err := insert("chl_t_3", b.ID, a.ID, ChallengeWithdrawn); err != nil {
		t.Errorf("a closed one beside it: %v", err)
	}
	// Locked entries need a challenge; open ones can't have one.
	for _, tc := range []struct {
		kind      string
		challenge any
	}{{EntryLocked, nil}, {EntryOpen, "chl_t_1"}} {
		if _, err := store.pool.Exec(ctx, `
			INSERT INTO entries (id, event_id, user_id, kind, formats, status, challenge_id)
			VALUES ('ent_t_bad', 'evt_t_chl', $1, $2, '{singles}', 'confirmed', $3)`, a.ID, tc.kind, tc.challenge); err == nil {
			t.Errorf("a %s entry with challenge %v was saved", tc.kind, tc.challenge)
		}
	}
}
