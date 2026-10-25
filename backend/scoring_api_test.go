package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

// scoringTestServer publishes the demo schedule, so the demo judges (who the
// seed registers) have matches, and serves the API with live scoring and the
// Postgres listener running.
func scoringTestServer(t *testing.T) (*PostgresStore, *httptest.Server, []PlayerMatch) {
	t.Helper()
	store := seededStore(t, testNow)
	ctx := context.Background()
	if _, err := store.PublishSchedule(ctx, demoEntryEvent, "usr_demo_sameer", testNow); err != nil {
		t.Fatalf("publish: %v", err)
	}
	home, err := store.JudgeHome(ctx, "usr_demo_sameer", testNow)
	if err != nil || len(home.Matches) < 2 {
		t.Fatalf("Sameer needs two matches: %v, %d", err, len(home.Matches))
	}
	api := NewAPI(store, oauth.NewRegistry(), nil, false).
		WithEvents(store).WithMatches(store).WithJudges(store).WithScoring(store)
	stop := startLiveListener(context.Background(), store.pool, api.live)
	srv := httptest.NewServer(api.Routes())
	t.Cleanup(func() { srv.Close(); stop() })
	return store, srv, home.Matches
}

func sessionFor(t *testing.T, store *PostgresStore, srv *httptest.Server, userID string) adminClient {
	t.Helper()
	token, err := store.CreateSession(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return adminClient{t: t, srv: srv, cookie: &http.Cookie{Name: sessionCookieName, Value: token}}
}

// call sends JSON and returns the status and decoded body, errors included.
func call(c adminClient, method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out) // error bodies may be empty
	return res.StatusCode, out
}

func point(seq int, side, clientID string) map[string]any {
	return map[string]any{"seq": seq, "kind": PointRally, "side": side, "clientId": clientID}
}

func liveOf(t *testing.T, c adminClient, matchID string) LiveMatch {
	t.Helper()
	var resp liveMatchResponse
	if code := c.do("GET", "/api/matches/"+matchID+"/live", nil, &resp); code != http.StatusOK {
		t.Fatalf("live %s: %d", matchID, code)
	}
	return resp.Match
}

func TestLiveScoring(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	match := matches[0]
	base := "/api/judge/matches/" + match.ID
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	lata := sessionFor(t, store, srv, "usr_demo_lata")
	playerID := match.Sides[0].Players[0].UserID
	playerC := sessionFor(t, store, srv, playerID)
	anon := adminClient{t: t, srv: srv}

	// Watching needs a session; scoring needs this match's own judge.
	if code, _ := call(anon, "GET", "/api/matches/"+match.ID+"/live", nil); code != http.StatusUnauthorized {
		t.Errorf("anonymous watch: %d, want 401", code)
	}
	if code, _ := call(lata, "POST", base+"/start", map[string]string{"firstServer": "A"}); code != http.StatusForbidden {
		t.Errorf("another judge starting: %d, want 403", code)
	}
	if code, _ := call(playerC, "POST", base+"/start", map[string]string{"firstServer": "A"}); code != http.StatusForbidden {
		t.Errorf("a player starting: %d, want 403", code)
	}
	if code, _ := call(sameer, "POST", base+"/points", point(1, "A", "c1")); code != http.StatusConflict {
		t.Errorf("point before start: %d, want 409", code)
	}

	// Start, with B to serve first.
	if code, _ := call(sameer, "POST", base+"/start", map[string]string{"firstServer": "C"}); code != http.StatusBadRequest {
		t.Errorf("bad first server: %d", code)
	}
	code, body := call(sameer, "POST", base+"/start", map[string]string{"firstServer": "B"})
	if code != http.StatusOK {
		t.Fatalf("start: %d %v", code, body)
	}
	m := liveOf(t, playerC, match.ID)
	if m.Status != MatchLive || m.Score.Server != "B" || m.NextSeq != 1 || m.StartedAt == nil || len(m.Sides) != 2 {
		t.Errorf("after start: status %s, server %q, nextSeq %d, sides %d", m.Status, m.Score.Server, m.NextSeq, len(m.Sides))
	}
	if code, _ := call(sameer, "POST", base+"/start", map[string]string{"firstServer": "A"}); code != http.StatusConflict {
		t.Errorf("starting twice: %d, want 409", code)
	}

	// A point, then the same point resent: counted once.
	if code, body := call(sameer, "POST", base+"/points", point(1, "A", "c1")); code != http.StatusOK {
		t.Fatalf("point 1: %d %v", code, body)
	}
	if code, _ := call(sameer, "POST", base+"/points", point(1, "A", "c1")); code != http.StatusOK {
		t.Errorf("resend: %d, want 200", code)
	}
	m = liveOf(t, sameer, match.ID)
	if g := m.Score.Games[0]; g.A != 1 || g.B != 0 || m.NextSeq != 2 || m.Score.Server != "A" {
		t.Errorf("after one point and a resend: %+v, nextSeq %d, server %q", g, m.NextSeq, m.Score.Server)
	}

	// Out of turn: refused, saying which number comes next.
	code, body = call(sameer, "POST", base+"/points", point(3, "A", "c3"))
	if code != http.StatusConflict || body["nextSeq"] != float64(2) {
		t.Errorf("out of turn: %d %v, want 409 with nextSeq 2", code, body)
	}
	for name, bad := range map[string]map[string]any{
		"no side":        {"seq": 2, "kind": PointRally, "clientId": "x"},
		"undo with side": {"seq": 2, "kind": PointUndo, "side": "A", "clientId": "x"},
		"unknown kind":   {"seq": 2, "kind": "let", "clientId": "x"},
		"no client id":   {"seq": 2, "kind": PointRally, "side": "A"},
		"unknown field":  {"seq": 2, "kind": PointRally, "side": "A", "clientId": "x", "score": 21},
	} {
		if code, _ := call(sameer, "POST", base+"/points", bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}

	// Undo takes the point back, and the serve with it.
	if code, _ := call(sameer, "POST", base+"/points", map[string]any{"seq": 2, "kind": PointUndo, "clientId": "u2"}); code != http.StatusOK {
		t.Fatalf("undo: %d", code)
	}
	m = liveOf(t, sameer, match.ID)
	if g := m.Score.Games[0]; g.A != 0 || m.Score.Server != "B" || m.NextSeq != 3 {
		t.Errorf("after undo: %+v, server %q, nextSeq %d", g, m.Score.Server, m.NextSeq)
	}
	if code, _ := call(sameer, "POST", base+"/points", map[string]any{"seq": 3, "kind": PointUndo, "clientId": "u3"}); code != http.StatusConflict {
		t.Errorf("undo with nothing to undo: %d, want 409", code)
	}

	// Not decided yet, so it can't be finished.
	if code, _ := call(sameer, "POST", base+"/finish", nil); code != http.StatusConflict {
		t.Errorf("finish undecided: %d, want 409", code)
	}

	// A wins in straight games: 42 points.
	seq := m.NextSeq
	for i := range 42 {
		if code, body := call(sameer, "POST", base+"/points", point(seq, "A", fmt.Sprintf("a%d", i))); code != http.StatusOK {
			t.Fatalf("point %d: %d %v", seq, code, body)
		}
		seq++
	}
	m = liveOf(t, sameer, match.ID)
	if m.Score.Winner != "A" || m.Score.GamesA != 2 || m.Status != MatchLive {
		t.Fatalf("after 42 points: winner %q, games %d, status %s", m.Score.Winner, m.Score.GamesA, m.Status)
	}
	if code, _ := call(sameer, "POST", base+"/points", point(seq, "B", "late")); code != http.StatusConflict {
		t.Errorf("point after the match is decided: %d, want 409", code)
	}

	// Finish records the result; afterwards nothing changes it.
	code, body = call(sameer, "POST", base+"/finish", nil)
	if code != http.StatusOK {
		t.Fatalf("finish: %d %v", code, body)
	}
	m = liveOf(t, playerC, match.ID)
	if m.Status != MatchCompleted || m.WinnerSide == nil || *m.WinnerSide != "A" || m.CompletedAt == nil {
		t.Errorf("after finish: status %s, winner %v", m.Status, m.WinnerSide)
	}
	if code, _ := call(sameer, "POST", base+"/points", map[string]any{"seq": seq, "kind": PointUndo, "clientId": "after"}); code != http.StatusConflict {
		t.Errorf("undo after finish: %d, want 409", code)
	}
	if code, _ := call(sameer, "POST", base+"/points", point(1, "A", "c1")); code != http.StatusOK {
		t.Errorf("an old point resent after finish: %d, want 200 (nothing added)", code)
	}

	// The judge's home and the player's matches both show the result.
	var mine struct{ Matches []PlayerMatch }
	playerC.do("GET", "/api/me/matches", nil, &mine)
	for _, pm := range mine.Matches {
		if pm.ID == match.ID && (pm.Status != MatchCompleted || pm.WinnerSide == nil) {
			t.Errorf("player's list: %+v", pm)
		}
	}
}

func TestStartTooEarly(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	match := matches[1]
	// Move it well into the future, on a day nothing else uses the court.
	if _, err := store.pool.Exec(context.Background(), `
		UPDATE matches SET starts_at = now() + interval '30 days', ends_at = now() + interval '30 days 30 minutes'
		WHERE id = $1`, match.ID); err != nil {
		t.Fatal(err)
	}
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	code, body := call(sameer, "POST", "/api/judge/matches/"+match.ID+"/start", map[string]string{"firstServer": "A"})
	if code != http.StatusConflict || body["canStartFrom"] == nil {
		t.Errorf("starting a month early: %d %v, want 409 with canStartFrom", code, body)
	}
	if code, _ := call(sameer, "POST", "/api/judge/matches/m_nope/start", map[string]string{"firstServer": "A"}); code != http.StatusNotFound {
		t.Errorf("unknown match: %d, want 404", code)
	}
}

// readEvent reads the next server-sent "match" event.
func readEvent(t *testing.T, r *bufio.Reader) LiveMatch {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended: %v", err)
		}
		if data, ok := strings.CutPrefix(strings.TrimRight(line, "\n"), "data: "); ok {
			var m LiveMatch
			if err := json.Unmarshal([]byte(data), &m); err != nil {
				t.Fatal(err)
			}
			return m
		}
	}
	t.Fatal("no event in time")
	return LiveMatch{}
}

func TestLiveStream(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	match := matches[0]
	base := "/api/judge/matches/" + match.ID
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	viewer := sessionFor(t, store, srv, match.Sides[1].Players[0].UserID)

	req, _ := http.NewRequest("GET", srv.URL+"/api/matches/"+match.ID+"/live/stream", nil)
	req.AddCookie(viewer.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	r := bufio.NewReader(res.Body)

	if m := readEvent(t, r); m.Status != MatchScheduled {
		t.Errorf("first event: status %s, want scheduled", m.Status)
	}
	call(sameer, "POST", base+"/start", map[string]string{"firstServer": "A"})
	if m := readEvent(t, r); m.Status != MatchLive {
		t.Errorf("after start: %s", m.Status)
	}
	call(sameer, "POST", base+"/points", point(1, "B", "s1"))
	// The publish and the database notification can both arrive; read
	// until the score shows the point.
	for {
		if m := readEvent(t, r); len(m.Score.Games) > 0 && m.Score.Games[0].B == 1 {
			break
		}
	}

	// A score from another server instance arrives through NOTIFY alone.
	if _, err := store.AddPoint(context.Background(), match.ID, "usr_demo_sameer", PointInput{Seq: 2, Kind: PointRally, Side: "A", ClientID: "other-instance"}); err != nil {
		t.Fatal(err)
	}
	for {
		if m := readEvent(t, r); m.Score.Games[0].A == 1 {
			break
		}
	}
}
