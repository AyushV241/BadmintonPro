package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

func TestNormaliseUsername(t *testing.T) {
	for in, want := range map[string]string{
		"  @Asha_Rao ": "asha_rao",
		"@@ash":        "ash",
		"@":            "",
		"   ":          "",
	} {
		if got := normaliseUsername(in); got != want {
			t.Errorf("normaliseUsername(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLikeEscaper(t *testing.T) {
	if got := likeEscaper.Replace(`a_b%c\d`); got != `a\_b\%c\\d` {
		t.Errorf("got %q", got)
	}
}

// search returns the usernames found for q, in order.
func search(c adminClient, q string) []string {
	c.t.Helper()
	var got usersResponse
	if code := c.do("GET", "/api/users/search?q="+url.QueryEscape(q), nil, &got); code != http.StatusOK {
		c.t.Fatalf("search %q: %d", q, code)
	}
	names := make([]string, len(got.Users))
	for i, u := range got.Users {
		names[i] = u.Username
	}
	return names
}

func assertUsernames(t *testing.T, q string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) { // nil and empty are equal here
		t.Errorf("search %q = %v, want %v", q, got, want)
	}
}

// searchPlayer is player() with a name no query in these tests matches. The
// default name, "Player <username>", contains the username, so every
// username search would match on the name as well.
func searchPlayer(t *testing.T, store *PostgresStore, srv *httptest.Server, n int, username string) (adminClient, User) {
	t.Helper()
	return namedPlayer(t, store, srv, n, username, "Zed Quill")
}

func namedPlayer(t *testing.T, store *PostgresStore, srv *httptest.Server, n int, username, name string) (adminClient, User) {
	t.Helper()
	c, u := player(t, store, srv, n, username)
	u, err := store.UpdateProfile(context.Background(), u.ID, ProfileUpdate{Name: name, Username: username})
	if err != nil {
		t.Fatal(err)
	}
	return c, u
}

func TestSearchUsersRanking(t *testing.T) {
	store, srv := entriesTestServer(t)
	me, _ := searchPlayer(t, store, srv, 1, "searcher")
	// similarity to "priya": priya_k 0.75, priyam 0.625, priyanka 0.5,
	// supriya 0.4, prya and riya 0.375, pablo 0.09.
	for i, name := range []string{"priyanka", "supriya", "prya", "riya", "pablo", "priya_k", "priya"} {
		searchPlayer(t, store, srv, 10+i, name)
	}

	// Exact, then prefix (closest first), then contains; fewer than five, so
	// fuzzy matches follow, ties broken by length and then name.
	assertUsernames(t, "priya", search(me, " @Priya "), "priya", "priya_k", "priyanka", "supriya", "prya", "riya")

	// Five matches without fuzzy: no fuzzy ones are added.
	searchPlayer(t, store, srv, 20, "priyam")
	assertUsernames(t, "priya", search(me, "priya"), "priya", "priya_k", "priyam", "priyanka", "supriya")

	// No fuzzy matching under three characters.
	assertUsernames(t, "pr", search(me, "pr"), "prya", "priya", "priyam", "priya_k", "priyanka", "supriya")

	// Empty queries find no-one without asking the database.
	assertUsernames(t, "@", search(me, "@"))
	assertUsernames(t, "", search(me, ""))
}

func TestSearchUsersEscapesLike(t *testing.T) {
	store, srv := entriesTestServer(t)
	me, _ := searchPlayer(t, store, srv, 1, "searcher")
	for i, name := range []string{"a_bc", "axb", "abc"} {
		searchPlayer(t, store, srv, 10+i, name)
	}
	// "_" is a literal underscore, not "any character": axb doesn't match.
	assertUsernames(t, "a_b", search(me, "a_b"), "a_bc")
	// "%" isn't "anything"; no username has one.
	assertUsernames(t, "%", search(me, "%"))
	assertUsernames(t, `\`, search(me, `\`))
}

func TestSearchUsersPrivacy(t *testing.T) {
	store, srv := entriesTestServer(t)
	me, _ := searchPlayer(t, store, srv, 1, "asha")
	searchPlayer(t, store, srv, 2, "ashwin")
	_, noName := searchPlayer(t, store, srv, 3, "ashok")
	if _, err := store.pool.Exec(context.Background(), `UPDATE users SET name = '' WHERE id = $1`, noName.ID); err != nil {
		t.Fatal(err)
	}
	signIn(t, store, "+919800009999") // no username at all

	// The searcher and incomplete profiles are left out.
	assertUsernames(t, "ash", search(me, "ash"), "ashwin")

	// Only public fields: no phone, email or anything else.
	var raw struct {
		Users []map[string]any `json:"users"`
	}
	me.do("GET", "/api/users/search?q=ashwin", nil, &raw)
	if len(raw.Users) != 1 {
		t.Fatalf("users = %v", raw.Users)
	}
	var keys []string
	for k := range raw.Users[0] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if strings.Join(keys, ",") != "id,name,username" {
		t.Errorf("fields = %v, want only id, name and username", keys)
	}

	anon := adminClient{t: t, srv: srv}
	if code := anon.do("GET", "/api/users/search?q=ash", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("without a session: %d", code)
	}
	if code := me.do("GET", "/api/users/search?q="+strings.Repeat("a", maxSearchQueryLength+1), nil, nil); code != http.StatusBadRequest {
		t.Errorf("long query: %d", code)
	}
}

func TestSearchUsersRateLimit(t *testing.T) {
	store := eventsTestStore(t)
	if _, err := store.pool.Exec(context.Background(), `TRUNCATE users, sessions, user_identities CASCADE`); err != nil {
		t.Fatal(err)
	}
	api := NewAPI(store, oauth.NewRegistry(), nil, false).WithUserSearch(store)
	api.searchLimit = newLimiter(2, time.Minute)
	srv := httptest.NewServer(api.Routes())
	t.Cleanup(srv.Close)
	me, _ := searchPlayer(t, store, srv, 1, "asha")
	other, _ := searchPlayer(t, store, srv, 2, "ravi")

	search(me, "ra")
	search(me, "rav")
	req, _ := http.NewRequest("GET", srv.URL+"/api/users/search?q=ravi", nil)
	req.AddCookie(me.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body errorResponse
	_ = json.NewDecoder(res.Body).Decode(&body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" || !strings.HasPrefix(body.Error, "too many searches") {
		t.Errorf("third search: %d %q, Retry-After %q", res.StatusCode, body.Error, res.Header.Get("Retry-After"))
	}
	// The limit is per user.
	assertUsernames(t, "asha", search(other, "asha"), "asha")
}

func TestSearchUsersByName(t *testing.T) {
	store, srv := entriesTestServer(t)
	me, _ := namedPlayer(t, store, srv, 1, "searcher", "Zed Quill")
	namedPlayer(t, store, srv, 10, "ayush_v", "Ayush Verma")
	namedPlayer(t, store, srv, 11, "kiran_k", "Kiran Verma")
	namedPlayer(t, store, srv, 12, "xkirx", "Zed Quill")
	namedPlayer(t, store, srv, 13, "rohan", "Rohan Mehta")

	// A full name, any letter case, is an exact match and comes first.
	// (With so few matches, similar names follow fuzzily: Kiran Verma.)
	if got := search(me, "Ayush Verma"); len(got) == 0 || got[0] != "ayush_v" {
		t.Errorf(`search "Ayush Verma" = %v, want ayush_v first`, got)
	}

	// The start of any word of a name: both Vermas, not the others.
	got := search(me, "ver")
	if !slices.Contains(got, "ayush_v") || !slices.Contains(got, "kiran_k") || slices.Contains(got, "rohan") {
		t.Errorf(`search "ver" = %v, want both Vermas`, got)
	}

	// The closer of name and username counts: "kir" starts Kiran's name
	// (tier 2), so Kiran comes before a username that only contains it
	// (tier 3).
	assertUsernames(t, "kir", search(me, "kir"), "kiran_k", "xkirx")

	// Usernames still match on their own.
	assertUsernames(t, "rohan", search(me, "@rohan"), "rohan")

	// A misspelt name is found fuzzily.
	if got := search(me, "aysh"); !slices.Contains(got, "ayush_v") {
		t.Errorf(`search "aysh" = %v, want the fuzzy match ayush_v`, got)
	}
}

func TestSuggestPlayers(t *testing.T) {
	store, srv := entriesTestServer(t)
	ctx := context.Background()
	me, meUser := namedPlayer(t, store, srv, 1, "searcher", "Zed Quill")
	_, close1 := namedPlayer(t, store, srv, 10, "close_up", "Close Up")
	_, close2 := namedPlayer(t, store, srv, 11, "close_down", "Close Down")
	_, far := namedPlayer(t, store, srv, 12, "far_away", "Far Away")
	_, noName := namedPlayer(t, store, srv, 13, "no_name", "No Name")
	for _, r := range []struct {
		id     string
		rating int
	}{{meUser.ID, 1300}, {close1.ID, 1310}, {close2.ID, 1280}, {far.ID, 1700}} {
		if _, err := store.pool.Exec(ctx,
			`INSERT INTO player_ratings (user_id, discipline, rating) VALUES ($1, 'singles', $2)`, r.id, r.rating); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET name = '' WHERE id = $1`, noName.ID); err != nil {
		t.Fatal(err)
	}

	var resp struct{ Players []Player }
	if code := me.do("GET", "/api/players/suggested", nil, &resp); code != http.StatusOK {
		t.Fatalf("suggested: %d", code)
	}
	var got []string
	for _, p := range resp.Players {
		got = append(got, p.Username)
	}
	// Closest rating first; never the searcher or an incomplete profile.
	if want := []string{"close_up", "close_down", "far_away"}; !slices.Equal(got, want) {
		t.Errorf("suggested = %v, want %v", got, want)
	}
	if resp.Players[0].Rating != 1310 {
		t.Errorf("rating = %d, want 1310", resp.Players[0].Rating)
	}

	anon := adminClient{t: t, srv: srv}
	if code := anon.do("GET", "/api/players/suggested", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("without a session: %d", code)
	}
}
