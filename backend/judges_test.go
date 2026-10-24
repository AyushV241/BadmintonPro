package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
	"github.com/AyushV241/BadmintonPro/backend/internal/otp"
)

// judgesTestServer is the full API with phone login, against Postgres.
func judgesTestServer(t *testing.T) (*PostgresStore, *httptest.Server, *smsInbox) {
	t.Helper()
	store := eventsTestStore(t)
	if _, err := store.pool.Exec(context.Background(),
		`TRUNCATE users, sessions, user_identities, event_judges, judges CASCADE`); err != nil {
		t.Fatal(err)
	}
	inbox := &smsInbox{codes: make(map[string]string)}
	phone := NewPhoneLogin(otp.NewMemory("test", inbox.deliver), mustPhonePolicy(t, "IN", "IN"))
	srv := httptest.NewServer(NewAPI(store, oauth.NewRegistry(), phone, false).
		WithEvents(store).WithAdmin(store).WithJudges(store).Routes())
	t.Cleanup(srv.Close)
	return store, srv, inbox
}

// phoneSignIn sends a code to phone and verifies it as `as` ("" or
// "judge"). It returns the status and, on success, the session cookie.
func phoneSignIn(t *testing.T, srv *httptest.Server, inbox *smsInbox, phone, as string) (int, *http.Cookie) {
	t.Helper()
	c := adminClient{t: t, srv: srv}
	if code := c.do("POST", "/api/auth/phone/start", map[string]string{"phone": phone}, nil); code != http.StatusOK {
		t.Fatalf("start %s: %d", phone, code)
	}
	body := fmt.Sprintf(`{"phone":%q,"code":%q,"as":%q}`, phone, inbox.code(t, phone), as)
	res, err := http.Post(srv.URL+"/api/auth/phone/verify", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	for _, ck := range res.Cookies() {
		if ck.Name == sessionCookieName {
			return res.StatusCode, ck
		}
	}
	return res.StatusCode, nil
}

func countIdentities(t *testing.T, store *PostgresStore, phone string) int {
	t.Helper()
	var n int
	if err := store.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM user_identities WHERE provider = 'phone' AND subject = $1`, phone,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestJudgeAccounts(t *testing.T) {
	store, srv, inbox := judgesTestServer(t)
	ctx := context.Background()
	adminCookie, _ := signIn(t, store, "+919800000001")
	api := adminClient{t: t, srv: srv, cookie: adminCookie}

	// The admin registers a number that has never signed in. Typed without
	// a country code, it's read as Indian.
	var created registeredJudgeResponse
	if code := api.do("POST", "/api/admin/judges", map[string]string{"name": " Priya Nair ", "phone": "98000 00002"}, &created); code != http.StatusCreated {
		t.Fatalf("register: %d", code)
	}
	priya := created.Judge
	if priya.Name != "Priya Nair" || priya.Phone != "+919800000002" || priya.UserID == "" {
		t.Errorf("judge = %+v", priya)
	}
	if n := countIdentities(t, store, "+919800000002"); n != 1 {
		t.Errorf("registering made %d phone identities, want 1 (the account it signs in to)", n)
	}

	// Bad input and duplicates.
	for _, tc := range []struct {
		body map[string]string
		want int
	}{
		{map[string]string{"name": "Priya again", "phone": "+919800000002"}, http.StatusConflict},
		{map[string]string{"name": "", "phone": "+919800000003"}, http.StatusBadRequest},
		{map[string]string{"name": "Nope", "phone": "12"}, http.StatusBadRequest},
		{map[string]string{"name": "Nope", "phone": "+919800000003", "role": "admin"}, http.StatusBadRequest},
	} {
		if code := api.do("POST", "/api/admin/judges", tc.body, nil); code != tc.want {
			t.Errorf("register %v: %d, want %d", tc.body, code, tc.want)
		}
	}
	var list struct{ Judges []RegisteredJudge }
	if api.do("GET", "/api/admin/judges", nil, &list); len(list.Judges) != 1 || list.Judges[0].UserID != priya.UserID {
		t.Errorf("list = %+v", list.Judges)
	}

	// "Log in as judge" with the registered number: judge mode.
	code, judgeCookie := phoneSignIn(t, srv, inbox, "+919800000002", SessionJudge)
	if code != http.StatusOK || judgeCookie == nil {
		t.Fatalf("judge login: %d", code)
	}
	judge := adminClient{t: t, srv: srv, cookie: judgeCookie}
	var me meResponse
	judge.do("GET", "/api/me", nil, &me)
	if me.User.ID != priya.UserID || me.Mode != SessionJudge || !me.IsJudge || me.User.Name != "Priya Nair" {
		t.Errorf("judge /api/me = %+v", me)
	}
	var home JudgeHome
	if code := judge.do("GET", "/api/judge/home", nil, &home); code != http.StatusOK || home.Judge.UserID != priya.UserID {
		t.Errorf("judge home: %d %+v", code, home.Judge)
	}

	// Judges can play: a normal sign-in with the same number reaches the same
	// account, in player mode.
	code, playerCookie := phoneSignIn(t, srv, inbox, "+919800000002", "")
	if code != http.StatusOK {
		t.Fatalf("player login: %d", code)
	}
	player := adminClient{t: t, srv: srv, cookie: playerCookie}
	player.do("GET", "/api/me", nil, &me)
	if me.User.ID != priya.UserID || me.Mode != SessionPlayer || !me.IsJudge {
		t.Errorf("player /api/me = %+v", me)
	}
	// ...and can switch to the judge view and back without signing in again.
	if code := player.do("POST", "/api/me/mode", map[string]string{"mode": SessionJudge}, &me); code != http.StatusOK || me.Mode != SessionJudge {
		t.Errorf("switch to judge: %d %+v", code, me)
	}
	if code := player.do("POST", "/api/me/mode", map[string]string{"mode": SessionPlayer}, &me); code != http.StatusOK || me.Mode != SessionPlayer {
		t.Errorf("switch to player: %d %+v", code, me)
	}
	if code := player.do("POST", "/api/me/mode", map[string]string{"mode": "admin"}, nil); code != http.StatusBadRequest {
		t.Errorf("bad mode: %d", code)
	}

	// A number nobody registered can't log in as a judge, and no account is
	// made for it.
	code, cookie := phoneSignIn(t, srv, inbox, "+919800000009", SessionJudge)
	if code != http.StatusForbidden || cookie != nil {
		t.Errorf("unregistered judge login: %d (cookie %v), want 403 and no session", code, cookie != nil)
	}
	if n := countIdentities(t, store, "+919800000009"); n != 0 {
		t.Errorf("refused judge login created %d accounts", n)
	}
	// It can still sign in to play, which doesn't make it a judge.
	_, outsiderCookie := phoneSignIn(t, srv, inbox, "+919800000009", "")
	outsider := adminClient{t: t, srv: srv, cookie: outsiderCookie}
	outsider.do("GET", "/api/me", nil, &me)
	if me.IsJudge || me.Mode != SessionPlayer {
		t.Errorf("outsider /api/me = %+v", me)
	}
	if code := outsider.do("POST", "/api/me/mode", map[string]string{"mode": SessionJudge}, nil); code != http.StatusForbidden {
		t.Errorf("outsider to judge view: %d, want 403", code)
	}
	if code := outsider.do("GET", "/api/judge/home", nil, nil); code != http.StatusForbidden {
		t.Errorf("outsider judge home: %d, want 403", code)
	}

	// A bad "as" is refused before the code is used up.
	if code := api.do("POST", "/api/auth/phone/start", map[string]string{"phone": "+919800000002"}, nil); code != http.StatusOK {
		t.Fatal(code)
	}
	sent := inbox.code(t, "+919800000002")
	if code := api.do("POST", "/api/auth/phone/verify", map[string]string{"phone": "+919800000002", "code": sent, "as": "admin"}, nil); code != http.StatusBadRequest {
		t.Errorf(`as "admin": %d, want 400`, code)
	}
	if code := api.do("POST", "/api/auth/phone/verify", map[string]string{"phone": "+919800000002", "code": sent, "as": "judge"}, nil); code != http.StatusOK {
		t.Errorf("the same code afterwards: %d, want 200", code)
	}

	// Event judges come from the registry, and removing a judge takes them
	// off events still to come but not finished ones.
	insertEvent(t, store, "evt_j_future", testEvent{})
	insertEvent(t, store, "evt_j_past", testEvent{})
	if _, err := store.pool.Exec(ctx, `
		UPDATE events SET registration_closes_at = now() - interval '72 hours',
			starts_at = now() - interval '48 hours', ends_at = now() - interval '46 hours'
		WHERE id = 'evt_j_past'`); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{"evt_j_future", "evt_j_past"} {
		if _, err := store.AddEventJudge(ctx, ev, priya.UserID); err != nil {
			t.Fatalf("add to %s: %v", ev, err)
		}
	}
	judge.do("GET", "/api/judge/home", nil, &home)
	if len(home.Events) != 1 || home.Events[0].ID != "evt_j_future" {
		t.Errorf("home events = %+v, want only the one still to come", home.Events)
	}

	if code := api.do("DELETE", "/api/admin/judges/"+priya.UserID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if code := api.do("DELETE", "/api/admin/judges/"+priya.UserID, nil, nil); code != http.StatusNotFound {
		t.Errorf("remove twice: %d", code)
	}
	if code := judge.do("GET", "/api/judge/home", nil, nil); code != http.StatusForbidden {
		t.Errorf("removed judge's home: %d, want 403 at once", code)
	}
	judge.do("GET", "/api/me", nil, &me)
	if me.IsJudge || me.Mode != SessionPlayer {
		t.Errorf("removed judge /api/me = %+v", me)
	}
	future, _ := store.ListEventJudges(ctx, "evt_j_future")
	past, _ := store.ListEventJudges(ctx, "evt_j_past")
	if len(future) != 0 || len(past) != 1 {
		t.Errorf("after removal: future judges %d (want 0), past judges %d (want 1)", len(future), len(past))
	}
	if code, _ := phoneSignIn(t, srv, inbox, "+919800000002", SessionJudge); code != http.StatusForbidden {
		t.Errorf("removed judge login: %d, want 403", code)
	}
	// The account itself is untouched: they can still play.
	if code, _ := phoneSignIn(t, srv, inbox, "+919800000002", ""); code != http.StatusOK {
		t.Errorf("removed judge playing: %d", code)
	}
}

// Registering a number that already plays makes that same account a judge.
func TestRegisterExistingPlayerAsJudge(t *testing.T) {
	store, srv, inbox := judgesTestServer(t)
	adminCookie, _ := signIn(t, store, "+919800000001")
	api := adminClient{t: t, srv: srv, cookie: adminCookie}

	_, existing := signIn(t, store, "+919800000005")
	if _, err := store.UpdateProfile(context.Background(), existing.ID, ProfileUpdate{Name: "Arjun Das", Username: "arjun"}); err != nil {
		t.Fatal(err)
	}
	var created registeredJudgeResponse
	if code := api.do("POST", "/api/admin/judges", map[string]string{"name": "Arjun (umpire)", "phone": "+919800000005"}, &created); code != http.StatusCreated {
		t.Fatalf("register: %d", code)
	}
	if created.Judge.UserID != existing.ID || created.Judge.Username != "arjun" {
		t.Errorf("judge = %+v, want the existing account", created.Judge)
	}
	code, cookie := phoneSignIn(t, srv, inbox, "+919800000005", SessionJudge)
	if code != http.StatusOK {
		t.Fatalf("judge login: %d", code)
	}
	var me meResponse
	adminClient{t: t, srv: srv, cookie: cookie}.do("GET", "/api/me", nil, &me)
	// The player keeps the name they chose; the admin's label is the judge's.
	if me.User.ID != existing.ID || me.User.Name != "Arjun Das" || me.Mode != SessionJudge {
		t.Errorf("/api/me = %+v", me)
	}
}

// The judge home page lists the matches the schedule gave the judge.
func TestJudgeHomeListsAssignedMatches(t *testing.T) {
	store := seededStore(t, testNow)
	ctx := context.Background()
	if _, err := store.PublishSchedule(ctx, demoEntryEvent, "usr_demo_sameer", testNow); err != nil {
		t.Fatalf("publish: %v", err)
	}
	home, err := store.JudgeHome(ctx, "usr_demo_sameer", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Events) == 0 || home.Events[0].ID != demoEntryEvent {
		t.Errorf("events = %+v", home.Events)
	}
	if len(home.Matches) == 0 {
		t.Fatal("no matches for a seeded judge")
	}
	for _, m := range home.Matches {
		if m.Judge == nil || m.Judge.UserID != "usr_demo_sameer" || len(m.Sides) != 2 || m.MySide != "" {
			t.Errorf("match %s: judge %+v, %d sides, mySide %q", m.ID, m.Judge, len(m.Sides), m.MySide)
		}
	}
	// The seed registers its judges, so they can sign in as judges.
	u, err := store.JudgeForPhone(ctx, demoJudgePhones["usr_demo_sameer"])
	if err != nil || u.ID != "usr_demo_sameer" {
		t.Errorf("JudgeForPhone = %+v, %v", u, err)
	}
	if _, err := store.JudgeHome(ctx, "usr_demo_kavya", testNow); !errors.Is(err, ErrNotAJudge) {
		t.Errorf("a player's judge home: %v, want ErrNotAJudge", err)
	}
}
