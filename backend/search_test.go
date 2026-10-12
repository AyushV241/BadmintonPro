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

func TestSearchUsersRanking(t *testing.T) {
	store, srv := entriesTestServer(t)
	me, _ := player(t, store, srv, 1, "searcher")
	// similarity to "priya": priya_k 0.75, priyam 0.625, priyanka 0.5,
	// supriya 0.4, prya and riya 0.375, pablo 0.09.
	for i, name := range []string{"priyanka", "supriya", "prya", "riya", "pablo", "priya_k", "priya"} {
		player(t, store, srv, 10+i, name)
	}

	// Exact, then prefix (closest first), then contains; fewer than five, so
	// fuzzy matches follow, ties broken by length and then name.
	assertUsernames(t, "priya", search(me, " @Priya "), "priya", "priya_k", "priyanka", "supriya", "prya", "riya")

	// Five matches without fuzzy: no fuzzy ones are added.
	player(t, store, srv, 20, "priyam")
	assertUsernames(t, "priya", search(me, "priya"), "priya", "priya_k", "priyam", "priyanka", "supriya")

	// No fuzzy matching under three characters.
	assertUsernames(t, "pr", search(me, "pr"), "prya", "priya", "priyam", "priya_k", "priyanka", "supriya")

	// Empty queries find no-one without asking the database.
	assertUsernames(t, "@", search(me, "@"))
	assertUsernames(t, "", search(me, ""))
}

func TestSearchUsersEscapesLike(t *testing.T) {
	store, srv := entriesTestServer(t)
	me, _ := player(t, store, srv, 1, "searcher")
	for i, name := range []string{"a_bc", "axb", "abc"} {
		player(t, store, srv, 10+i, name)
	}
	// "_" is a literal underscore, not "any character": axb doesn't match.
	assertUsernames(t, "a_b", search(me, "a_b"), "a_bc")
	// "%" isn't "anything"; no username has one.
	assertUsernames(t, "%", search(me, "%"))
	assertUsernames(t, `\`, search(me, `\`))
}

func TestSearchUsersPrivacy(t *testing.T) {
	store, srv := entriesTestServer(t)
	me, _ := player(t, store, srv, 1, "asha")
	player(t, store, srv, 2, "ashwin")
	_, noName := player(t, store, srv, 3, "ashok")
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
	me, _ := player(t, store, srv, 1, "asha")
	other, _ := player(t, store, srv, 2, "ravi")

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
