package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/rating"
)

const (
	glickoName = "glicko-1"
	eloName    = "elo-1"
)

// score sends one point per letter of sides ("AAB": A, A, B) from seq on,
// and returns the next seq.
func score(t *testing.T, c adminClient, matchID string, seq int, sides string) int {
	t.Helper()
	for _, side := range sides {
		id := fmt.Sprintf("%s-p%d", matchID, seq)
		if code, body := call(c, "POST", "/api/judge/matches/"+matchID+"/points", point(seq, string(side), id)); code != http.StatusOK {
			t.Fatalf("point %d: %d %v", seq, code, body)
		}
		seq++
	}
	return seq
}

// playOut starts a match and gives side A a 21–0, 21–0 win, then finishes
// it.
func playOut(t *testing.T, judge adminClient, matchID string) {
	t.Helper()
	if code, body := call(judge, "POST", "/api/judge/matches/"+matchID+"/start", map[string]string{"firstServer": "A"}); code != http.StatusOK {
		t.Fatalf("start %s: %d %v", matchID, code, body)
	}
	score(t, judge, matchID, 1, repeat("A", 42))
	if code, body := call(judge, "POST", "/api/judge/matches/"+matchID+"/finish", nil); code != http.StatusOK {
		t.Fatalf("finish %s: %d %v", matchID, code, body)
	}
}

// evenRatings gives every player in the match the same established rating,
// so expected changes are easy to work out.
func evenRatings(t *testing.T, store *PostgresStore, m PlayerMatch) {
	t.Helper()
	for _, side := range m.Sides {
		for _, p := range side.Players {
			if _, err := store.pool.Exec(context.Background(), `
				INSERT INTO player_ratings (user_id, discipline, rating, matches_played, provisional)
				VALUES ($1, $2, 1500, 30, false)
				ON CONFLICT (user_id, discipline) DO UPDATE SET rating = 1500, matches_played = 30, provisional = false`,
				p.UserID, m.Format); err != nil {
				t.Fatal(err)
			}
		}
	}
}

type ratingRow struct{ rating, played int }

// ratingOf is the rating players see.
func ratingOf(t *testing.T, store *PostgresStore, userID, discipline string) ratingRow {
	t.Helper()
	var r ratingRow
	if err := store.pool.QueryRow(context.Background(), `
		SELECT rating, matches_played FROM player_ratings WHERE user_id = $1 AND discipline = $2`,
		userID, discipline).Scan(&r.rating, &r.played); err != nil {
		t.Fatal(err)
	}
	return r
}

// stateOf is a system's exact state for a player; ok is false if the system
// has never rated them.
func stateOf(t *testing.T, store *PostgresStore, userID, discipline, system string) (rating.Player, bool) {
	t.Helper()
	var (
		r    float64
		rd   *float64
		n    int
		last *time.Time
	)
	err := store.pool.QueryRow(context.Background(), `
		SELECT rating, rd, matches_played, last_rated_at FROM rating_states
		WHERE user_id = $1 AND discipline = $2 AND system = $3`, userID, discipline, system).Scan(&r, &rd, &n, &last)
	if err != nil {
		return rating.Player{}, false
	}
	return playerState(userID, r, rd, n, last), true
}

// changesBySide sums each side's rating changes of one system and kind, and
// counts them.
func changesBySide(m LiveMatch, system, kind string) (a, b, n int) {
	sideOf := map[string]string{}
	for _, s := range m.Sides {
		for _, p := range s.Players {
			sideOf[p.UserID] = s.Side
		}
	}
	for _, c := range m.RatingChanges {
		if c.System != system || c.Kind != kind {
			continue
		}
		n++
		if sideOf[c.UserID] == "A" {
			a += c.Change
		} else {
			b += c.Change
		}
	}
	return a, b, n
}

func playerCount(m PlayerMatch) int {
	n := 0
	for _, s := range m.Sides {
		n += len(s.Players)
	}
	return n
}

// expectRated is what sys makes of the match from these states: the
// reference the stored results are checked against.
func expectRated(t *testing.T, store *PostgresStore, system string, m LiveMatch, states map[string]rating.Player,
	scoreA float64, games []rating.Game) map[string]rating.Player {
	t.Helper()
	sys, ok := store.ratings.system(system)
	if !ok {
		t.Fatalf("no system %s", system)
	}
	rm := rating.Match{ScoreA: scoreA, Games: games, At: m.CompletedAt.UTC()}
	for _, s := range m.Sides {
		for _, p := range s.Players {
			st := states[p.UserID]
			st.ID = p.UserID
			if s.Side == "A" {
				rm.A = append(rm.A, st)
			} else {
				rm.B = append(rm.B, st)
			}
		}
	}
	out := map[string]rating.Player{}
	for _, c := range sys.Rate(rm) {
		out[c.PlayerID] = c.After
	}
	return out
}

var whitewash = []rating.Game{{A: 21, B: 0}, {A: 21, B: 0}}

func TestFinishRatesInEverySystemAndVoidRestores(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	match := matches[0]
	evenRatings(t, store, match)
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	// Any signed-in user is an admin until admin roles exist (admin_handlers.go).
	admin := sessionFor(t, store, srv, "usr_demo_lata")
	winner := match.Sides[0].Players[0].UserID
	loser := match.Sides[1].Players[0].UserID

	playOut(t, sameer, match.ID)
	m := liveOf(t, sameer, match.ID)
	if m.RatingSystem != glickoName {
		t.Errorf("ratingSystem = %q, want %s", m.RatingSystem, glickoName)
	}

	// Elo: even ratings, K 20, and the 21–0, 21–0 margin of 1.1: ±11 each.
	per := len(match.Sides[0].Players)
	if a, b, n := changesBySide(m, eloName, ratingKindMatch); n != playerCount(match) || a != 11*per || b != -11*per {
		t.Errorf("elo: A %+d, B %+d over %d rows; want %+d, %+d", a, b, n, 11*per, -11*per)
	}
	// Glicko: what the rating package makes of it, from the seeded 1500
	// after 30 matches.
	gl, _ := store.ratings.system(glickoName)
	seeded := map[string]rating.Player{}
	for _, s := range match.Sides {
		for _, p := range s.Players {
			seeded[p.UserID] = gl.Carry(1500, 30)
		}
	}
	want := expectRated(t, store, glickoName, m, seeded, 1, whitewash)
	for id, w := range want {
		got, ok := stateOf(t, store, id, match.Format, glickoName)
		if !ok || math.Abs(got.Rating-w.Rating) > 1e-9 || math.Abs(got.RD-w.RD) > 1e-9 || got.Matches != 31 {
			t.Errorf("glicko state of %s: %+v, want %+v", id, got, w)
		}
	}
	// Players see Glicko's rating, rounded.
	if r := ratingOf(t, store, winner, match.Format); r.rating != shown(want[winner].Rating) || r.played != 31 || r.rating <= 1500 {
		t.Errorf("winner sees %+v, want %d over 31", r, shown(want[winner].Rating))
	}
	if r := ratingOf(t, store, loser, match.Format); r.rating != shown(want[loser].Rating) || r.rating >= 1500 {
		t.Errorf("loser sees %+v, want %d", r, shown(want[loser].Rating))
	}
	for _, c := range m.RatingChanges {
		if c.WinProbability == nil || (c.System == glickoName && (c.RDBefore == nil || c.RDAfter == nil)) ||
			(c.System == eloName && c.RDBefore != nil) {
			t.Errorf("change row %+v", c)
		}
	}

	// A finished match can't be retired, and only an admin's void with a
	// reason strikes it off.
	if code, _ := call(sameer, "POST", "/api/judge/matches/"+match.ID+"/retire", map[string]string{"retiredSide": "B"}); code != http.StatusConflict {
		t.Errorf("retiring a finished match: %d, want 409", code)
	}
	if code, _ := call(admin, "POST", "/api/admin/matches/"+match.ID+"/void", map[string]string{"reason": "  "}); code != http.StatusBadRequest {
		t.Errorf("void without a reason: %d, want 400", code)
	}
	if code, body := call(admin, "POST", "/api/admin/matches/"+match.ID+"/void", map[string]string{"reason": "Wrong players on court"}); code != http.StatusOK {
		t.Fatalf("void: %d %v", code, body)
	}

	// Nobody has played since, so every system's exact earlier state comes
	// back, with a void row per player and system.
	m = liveOf(t, sameer, match.ID)
	if m.Status != MatchVoid || m.Void == nil || m.Void.Reason != "Wrong players on court" || m.Void.From != MatchCompleted {
		t.Errorf("after void: status %s, void %+v", m.Status, m.Void)
	}
	if a, b, n := changesBySide(m, eloName, ratingKindVoid); n != playerCount(match) || a != -11*per || b != 11*per {
		t.Errorf("elo reversals: A %+d, B %+d over %d rows", a, b, n)
	}
	if _, _, n := changesBySide(m, glickoName, ratingKindVoid); n != playerCount(match) {
		t.Errorf("%d glicko void rows, want %d", n, playerCount(match))
	}
	for id, before := range seeded {
		got, _ := stateOf(t, store, id, match.Format, glickoName)
		if got.Rating != before.Rating || got.RD != before.RD || got.Matches != 30 || !got.LastRated.IsZero() {
			t.Errorf("glicko state of %s after the void: %+v, want %+v", id, got, before)
		}
		if r := ratingOf(t, store, id, match.Format); r.rating != 1500 || r.played != 30 {
			t.Errorf("%s sees %+v after the void, want 1500 over 30", id, r)
		}
	}
	if code, _ := call(admin, "POST", "/api/admin/matches/"+match.ID+"/void", map[string]string{"reason": "again"}); code != http.StatusConflict {
		t.Errorf("voiding twice: %d, want 409", code)
	}
}

func TestVoidReplaysLaterMatches(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	ctx := context.Background()
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	admin := sessionFor(t, store, srv, "usr_demo_lata")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := store.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Two of the judge's matches, made into one discipline: a singles one
	// makes the other singles too, keeping one player a side.
	first, second := matches[0], matches[1]
	if second.Format == FormatSingles {
		first, second = second, first
	}
	if first.Format != second.Format {
		if first.Format == FormatSingles {
			exec(`DELETE FROM match_sides WHERE match_id = $1 AND user_id NOT IN (
				SELECT min(user_id) FROM match_sides WHERE match_id = $1 GROUP BY side)`, second.ID)
		}
		exec(`UPDATE matches SET format = $2 WHERE id = $1`, second.ID, first.Format)
	}
	// One of the first match's players also plays the second, so the second
	// depends on the first.
	lm2 := liveOf(t, sameer, second.ID)
	shared := first.Sides[0].Players[0].UserID
	exec(`UPDATE match_sides SET user_id = $3, entry_id = NULL WHERE match_id = $1 AND user_id = $2`,
		second.ID, lm2.Sides[1].Players[0].UserID, shared)
	second.Format = first.Format

	// Everyone's seeded rating, before anything is rated.
	seeds := map[string]seed{}
	for _, mm := range []PlayerMatch{first, second} {
		lm := liveOf(t, sameer, mm.ID)
		for _, s := range lm.Sides {
			for _, p := range s.Players {
				var sd seed
				var r int
				if err := store.pool.QueryRow(ctx, `
					SELECT rating, matches_played FROM player_ratings WHERE user_id = $1 AND discipline = $2`,
					p.UserID, mm.Format).Scan(&r, &sd.matches); err != nil {
					t.Fatalf("seed of %s: %v", p.UserID, err)
				}
				sd.rating = float64(r)
				seeds[p.UserID] = sd
			}
		}
	}

	playOut(t, sameer, first.ID)
	playOut(t, sameer, second.ID)
	if code, body := call(admin, "POST", "/api/admin/matches/"+first.ID+"/void", map[string]string{"reason": "Played on the wrong day"}); code != http.StatusOK {
		t.Fatalf("void: %d %v", code, body)
	}

	// Every system's state for the second match's players is what it would
	// be had the first never happened: the second rated from the seeds.
	lm2 = liveOf(t, sameer, second.ID)
	for _, system := range []string{glickoName, eloName} {
		sys, _ := store.ratings.system(system)
		start := map[string]rating.Player{}
		for id, sd := range seeds {
			start[id] = sys.Carry(sd.rating, sd.matches)
		}
		want := expectRated(t, store, system, lm2, start, 1, whitewash)
		for id, w := range want {
			got, _ := stateOf(t, store, id, second.Format, system)
			if math.Abs(got.Rating-w.Rating) > 1e-9 || math.Abs(got.RD-w.RD) > 1e-9 || got.Matches != w.Matches {
				t.Errorf("%s, %s after the replay: %+v, want %+v", system, id, got, w)
			}
		}
		// The first match's players who didn't play again are back where
		// they started.
		for _, s := range first.Sides {
			for _, p := range s.Players {
				if _, inSecond := want[p.UserID]; inSecond {
					continue
				}
				got, _ := stateOf(t, store, p.UserID, first.Format, system)
				if st := start[p.UserID]; got.Rating != st.Rating || got.Matches != st.Matches {
					t.Errorf("%s, %s: %+v, want back to %+v", system, p.UserID, got, st)
				}
			}
		}
	}

	// The second match's other players moved though they weren't in the
	// voided match: an adjust row each, credited to the void.
	var adjusts int
	if err := store.pool.QueryRow(ctx, `
		SELECT count(*) FROM rating_changes WHERE match_id = $1 AND kind = $2 AND system = $3`,
		first.ID, ratingKindAdjust, glickoName).Scan(&adjusts); err != nil {
		t.Fatal(err)
	}
	if adjusts == 0 {
		t.Error("no adjust rows for the second match's players")
	}
	// The shared player's void row takes them from after both matches to
	// after the second alone.
	var kinds []string
	rows, _ := store.pool.Query(ctx, `
		SELECT kind FROM rating_changes WHERE user_id = $1 AND system = $2 ORDER BY id`, shared, glickoName)
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		kinds = append(kinds, k)
	}
	rows.Close()
	if fmt.Sprint(kinds) != "[match match void]" {
		t.Errorf("shared player's history: %v", kinds)
	}
}

func TestRetirementIsRatedOnPointsPlayed(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	match := matches[0]
	evenRatings(t, store, match)
	base := "/api/judge/matches/" + match.ID
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")

	if code, _ := call(sameer, "POST", base+"/start", map[string]string{"firstServer": "A"}); code != http.StatusOK {
		t.Fatalf("start: %d", code)
	}
	// A leads 6–2, then gets injured and retires.
	score(t, sameer, match.ID, 1, "AAABAABA")
	for name, bad := range map[string]map[string]string{"no side": {}, "bad side": {"retiredSide": "C"}} {
		if code, _ := call(sameer, "POST", base+"/retire", bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}
	lata := sessionFor(t, store, srv, "usr_demo_lata")
	if code, _ := call(lata, "POST", base+"/retire", map[string]string{"retiredSide": "A"}); code != http.StatusForbidden {
		t.Errorf("another judge retiring it: %d, want 403", code)
	}
	if code, body := call(sameer, "POST", base+"/retire", map[string]string{"retiredSide": "A"}); code != http.StatusOK {
		t.Fatalf("retire: %d %v", code, body)
	}

	// B wins, but the rating follows the score so far: A won 6 of 8 points,
	// 0.75 against an expected 0.5, with no margin on top. Elo: 20 × 0.25.
	m := liveOf(t, sameer, match.ID)
	if m.Status != MatchRetired || m.WinnerSide == nil || *m.WinnerSide != "B" || m.RetiredSide == nil || *m.RetiredSide != "A" {
		t.Errorf("after retiring: status %s, winner %v, retired %v", m.Status, m.WinnerSide, m.RetiredSide)
	}
	per := len(match.Sides[0].Players)
	if a, b, n := changesBySide(m, eloName, ratingKindMatch); n != playerCount(match) || a != 5*per || b != -5*per {
		t.Errorf("elo: A %+d, B %+d over %d rows; want %+d, %+d", a, b, n, 5*per, -5*per)
	}
	if a, b, _ := changesBySide(m, glickoName, ratingKindMatch); a <= 0 || b >= 0 {
		t.Errorf("glicko: A %+d, B %+d; want A up, B down", a, b)
	}
	if code, _ := call(sameer, "POST", base+"/points", point(9, "A", "late")); code != http.StatusConflict {
		t.Errorf("point after retiring: %d, want 409", code)
	}
}

func TestRetirementBeforeAnyPointIsUnrated(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	match := matches[0]
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	base := "/api/judge/matches/" + match.ID
	if code, _ := call(sameer, "POST", base+"/start", map[string]string{"firstServer": "A"}); code != http.StatusOK {
		t.Fatalf("start: %d", code)
	}
	// A point and its undo: still nothing played.
	score(t, sameer, match.ID, 1, "B")
	if code, _ := call(sameer, "POST", base+"/points", map[string]any{"seq": 2, "kind": PointUndo, "clientId": "u"}); code != http.StatusOK {
		t.Fatalf("undo: %d", code)
	}
	if code, body := call(sameer, "POST", base+"/retire", map[string]string{"retiredSide": "B"}); code != http.StatusOK {
		t.Fatalf("retire: %d %v", code, body)
	}
	m := liveOf(t, sameer, match.ID)
	if m.Status != MatchRetired || len(m.RatingChanges) != 0 {
		t.Errorf("retired before a point: status %s, %d rating changes", m.Status, len(m.RatingChanges))
	}
}

func TestRetiringADecidedMatchIsRefused(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	match := matches[0]
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	base := "/api/judge/matches/" + match.ID
	if code, _ := call(sameer, "POST", base+"/start", map[string]string{"firstServer": "A"}); code != http.StatusOK {
		t.Fatalf("start: %d", code)
	}
	score(t, sameer, match.ID, 1, repeat("B", 42))
	if code, _ := call(sameer, "POST", base+"/retire", map[string]string{"retiredSide": "A"}); code != http.StatusConflict {
		t.Errorf("retiring a decided match: %d, want 409 (finish it)", code)
	}
}

func TestFirstRatedMatchCreatesTheRating(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	match := matches[0]
	winner := match.Sides[0].Players[0].UserID
	for _, side := range match.Sides {
		for _, p := range side.Players {
			// New to every discipline, so nothing carries over from singles.
			if _, err := store.pool.Exec(context.Background(),
				`DELETE FROM player_ratings WHERE user_id = $1`, p.UserID); err != nil {
				t.Fatal(err)
			}
		}
	}
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	playOut(t, sameer, match.ID)

	// Everyone starts at 1200 (RD 350 in Glicko). Players see Glicko's
	// result; Elo's provisional K 40 and the 1.1 margin make its win +22.
	gl, _ := store.ratings.system(glickoName)
	start := map[string]rating.Player{}
	for _, s := range match.Sides {
		for _, p := range s.Players {
			start[p.UserID] = gl.Initial()
		}
	}
	m := liveOf(t, sameer, match.ID)
	want := expectRated(t, store, glickoName, m, start, 1, whitewash)[winner]
	var provisional bool
	r := ratingOf(t, store, winner, match.Format)
	if err := store.pool.QueryRow(context.Background(),
		`SELECT provisional FROM player_ratings WHERE user_id = $1 AND discipline = $2`, winner, match.Format,
	).Scan(&provisional); err != nil {
		t.Fatal(err)
	}
	if r.rating != shown(want.Rating) || r.played != 1 || !provisional || r.rating <= startingRating {
		t.Errorf("after a first win: %+v, provisional %v; want %d over 1, provisional", r, provisional, shown(want.Rating))
	}
	if elo, _ := stateOf(t, store, winner, match.Format, eloName); elo.Rating != startingRating+22 {
		t.Errorf("elo after a first win: %v, want %d", elo.Rating, startingRating+22)
	}
}

func TestPlayerRatingsAPI(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	match := matches[0]
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	playOut(t, sameer, match.ID)
	winner := match.Sides[0].Players[0].UserID

	if code, _ := call(adminClient{t: t, srv: srv}, "GET", "/api/players/"+winner+"/ratings", nil); code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d, want 401", code)
	}
	if code, _ := call(sameer, "GET", "/api/players/usr_nobody/ratings", nil); code != http.StatusNotFound {
		t.Errorf("unknown player: %d, want 404", code)
	}
	var out PlayerRatings
	if code := sameer.do("GET", "/api/players/"+winner+"/ratings", nil, &out); code != http.StatusOK {
		t.Fatalf("ratings: %d", code)
	}
	if out.Default != glickoName {
		t.Errorf("default = %q", out.Default)
	}
	systems := map[string]PlayerRating{}
	for _, r := range out.Ratings {
		if r.Discipline == match.Format {
			systems[r.System] = r
		}
	}
	g, e := systems[glickoName], systems[eloName]
	if g.RD == nil || g.RDNow == nil || *g.RDNow < *g.RD || e.RD != nil || g.LastRatedAt == nil {
		t.Errorf("ratings: glicko %+v, elo %+v", g, e)
	}
	if r := ratingOf(t, store, winner, match.Format); g.Rating != r.rating {
		t.Errorf("glicko %d isn't the rating players see, %d", g.Rating, r.rating)
	}
	if len(out.History) != 2 || out.History[0].MatchID != match.ID || out.History[0].WinProbability == nil {
		t.Errorf("history: %+v", out.History)
	}
}

func entryStatuses(t *testing.T, store *PostgresStore, matchID, side string) []string {
	t.Helper()
	rows, err := store.pool.Query(context.Background(), `
		SELECT e.status FROM match_sides ms JOIN entries e ON e.id = ms.entry_id
		WHERE ms.match_id = $1 AND ms.side = $2 ORDER BY e.id`, matchID, side)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func allAre(statuses []string, want string) bool {
	for _, s := range statuses {
		if s != want {
			return false
		}
	}
	return len(statuses) > 0
}

func TestWalkover(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	match := matches[0]
	base := "/api/judge/matches/" + match.ID
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	lata := sessionFor(t, store, srv, "usr_demo_lata")
	if !allAre(entryStatuses(t, store, match.ID, "B"), EntryScheduled) {
		t.Fatalf("side B's entries before: %v", entryStatuses(t, store, match.ID, "B"))
	}

	if code, _ := call(sameer, "POST", base+"/walkover", map[string]string{"winnerSide": "X"}); code != http.StatusBadRequest {
		t.Errorf("bad side: %d, want 400", code)
	}
	if code, _ := call(lata, "POST", base+"/walkover", map[string]string{"winnerSide": "A"}); code != http.StatusForbidden {
		t.Errorf("another judge: %d, want 403", code)
	}
	code, body := call(sameer, "POST", base+"/walkover", map[string]string{"winnerSide": "A"})
	if code != http.StatusOK {
		t.Fatalf("walkover: %d %v", code, body)
	}

	// A wins without a rating change; B's entries are no-shows.
	m := liveOf(t, sameer, match.ID)
	if m.Status != MatchWalkover || m.WinnerSide == nil || *m.WinnerSide != "A" || len(m.RatingChanges) != 0 {
		t.Errorf("after walkover: status %s, winner %v, %d rating changes", m.Status, m.WinnerSide, len(m.RatingChanges))
	}
	if got := entryStatuses(t, store, match.ID, "B"); !allAre(got, EntryNoShow) {
		t.Errorf("side B's entries: %v, want no_show", got)
	}
	if got := entryStatuses(t, store, match.ID, "A"); !allAre(got, EntryScheduled) {
		t.Errorf("side A's entries: %v, want scheduled", got)
	}
	if code, _ := call(sameer, "POST", base+"/start", map[string]string{"firstServer": "A"}); code != http.StatusConflict {
		t.Errorf("starting after a walkover: %d, want 409", code)
	}
	if code, _ := call(sameer, "POST", base+"/walkover", map[string]string{"winnerSide": "B"}); code != http.StatusConflict {
		t.Errorf("a second walkover: %d, want 409", code)
	}

	// Voiding it gives the entries back.
	if code, body := call(lata, "POST", "/api/admin/matches/"+match.ID+"/void", map[string]string{"reason": "B was on court 3"}); code != http.StatusOK {
		t.Fatalf("void: %d %v", code, body)
	}
	if got := entryStatuses(t, store, match.ID, "B"); !allAre(got, EntryScheduled) {
		t.Errorf("side B's entries after the void: %v, want scheduled", got)
	}
}

func TestAdminWalkoverAndVoidRules(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	sameer := sessionFor(t, store, srv, "usr_demo_sameer")
	admin := sessionFor(t, store, srv, "usr_demo_lata")
	anon := adminClient{t: t, srv: srv}
	scheduled, live := matches[0], matches[1]

	if code, _ := call(anon, "POST", "/api/admin/matches/"+scheduled.ID+"/walkover", map[string]string{"winnerSide": "A"}); code != http.StatusUnauthorized {
		t.Errorf("anonymous walkover: %d, want 401", code)
	}
	if code, _ := call(admin, "POST", "/api/admin/matches/"+scheduled.ID+"/void", map[string]string{"reason": "x"}); code != http.StatusConflict {
		t.Errorf("voiding a match that hasn't started: %d, want 409 (cancel it)", code)
	}
	if code, _ := call(admin, "POST", "/api/admin/matches/m_nope/void", map[string]string{"reason": "x"}); code != http.StatusNotFound {
		t.Errorf("voiding an unknown match: %d, want 404", code)
	}

	// The admin records a walkover on a match that isn't theirs to judge.
	code, body := call(admin, "POST", "/api/admin/matches/"+scheduled.ID+"/walkover", map[string]string{"winnerSide": "B"})
	if code != http.StatusOK {
		t.Fatalf("admin walkover: %d %v", code, body)
	}
	if m := liveOf(t, admin, scheduled.ID); m.Status != MatchWalkover || *m.WinnerSide != "B" {
		t.Errorf("after the admin's walkover: %s %v", m.Status, m.WinnerSide)
	}

	// A live match can't be a walkover, but it can be voided, which stops
	// the scoring.
	if code, _ := call(sameer, "POST", "/api/judge/matches/"+live.ID+"/start", map[string]string{"firstServer": "A"}); code != http.StatusOK {
		t.Fatalf("start: %d", code)
	}
	score(t, sameer, live.ID, 1, "AB")
	if code, _ := call(admin, "POST", "/api/admin/matches/"+live.ID+"/walkover", map[string]string{"winnerSide": "A"}); code != http.StatusConflict {
		t.Errorf("walkover of a live match: %d, want 409", code)
	}
	if code, _ := call(admin, "POST", "/api/admin/matches/"+live.ID+"/void", map[string]string{"reason": "Floor flooded"}); code != http.StatusOK {
		t.Fatalf("void live: %d", code)
	}
	if code, _ := call(sameer, "POST", "/api/judge/matches/"+live.ID+"/points", point(3, "A", "p3")); code != http.StatusConflict {
		t.Errorf("point after the void: %d, want 409", code)
	}
	if m := liveOf(t, admin, live.ID); m.Status != MatchVoid || m.Void.From != MatchLive || len(m.RatingChanges) != 0 {
		t.Errorf("after voiding live: %s %+v, %d rating changes", m.Status, m.Void, len(m.RatingChanges))
	}
}

// firstMatch is the first of the judge's matches in a singles (or not)
// format.
func firstMatch(t *testing.T, matches []PlayerMatch, singles bool) PlayerMatch {
	t.Helper()
	for _, m := range matches {
		if (m.Format == FormatSingles) == singles {
			return m
		}
	}
	t.Fatalf("the demo judge has no %s match", map[bool]string{true: "singles", false: "doubles or mixed"}[singles])
	return PlayerMatch{}
}

func TestFirstDoublesRatingComesFromSingles(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	ctx := context.Background()
	match := firstMatch(t, matches, false)
	// Nobody in it has played this format; their singles ratings are known.
	singles := map[string]int{}
	for i, s := range match.Sides {
		for j, p := range s.Players {
			singles[p.UserID] = []int{1800, 1300, 1500, 1500}[i*2+j]
			if _, err := store.pool.Exec(ctx, `DELETE FROM player_ratings WHERE user_id = $1 AND discipline = $2`,
				p.UserID, match.Format); err != nil {
				t.Fatal(err)
			}
			if _, err := store.pool.Exec(ctx, `
				INSERT INTO player_ratings (user_id, discipline, rating, matches_played, provisional)
				VALUES ($1, 'singles', $2, 12, false)
				ON CONFLICT (user_id, discipline) DO UPDATE SET rating = $2, matches_played = 12`,
				p.UserID, singles[p.UserID]); err != nil {
				t.Fatal(err)
			}
		}
	}
	playOut(t, sessionFor(t, store, srv, "usr_demo_sameer"), match.ID)

	// Each player started from their singles rating, as new to the format:
	// Glicko with the singles RD plus 150 in quadrature, Elo with K 40.
	for _, system := range []string{glickoName, eloName} {
		sys, _ := store.ratings.system(system)
		for id, r := range singles {
			want := sys.Transfer(sys.Carry(float64(r), 12), time.Now())
			var before, rdBefore *float64
			var matchesBefore int
			var k *float64
			if err := store.pool.QueryRow(ctx, `
				SELECT rating_before, rd_before, matches_before, (details->>'k')::float8
				FROM rating_changes WHERE match_id = $1 AND user_id = $2 AND system = $3 AND kind = 'match'`,
				match.ID, id, system).Scan(&before, &rdBefore, &matchesBefore, &k); err != nil {
				t.Fatalf("%s, %s: %v", system, id, err)
			}
			if *before != want.Rating || matchesBefore != 0 {
				t.Errorf("%s, %s started at %v after %d matches, want %v after 0", system, id, *before, matchesBefore, want.Rating)
			}
			if system == glickoName && (rdBefore == nil || math.Abs(*rdBefore-want.RD) > 1e-9) {
				t.Errorf("%s started with RD %v, want %.2f", id, rdBefore, want.RD)
			}
			if system == eloName && *k != 40 {
				t.Errorf("%s's Elo K %v, want the provisional 40", id, *k)
			}
		}
	}
	// The 1800 and 1300 pair played as 1600: 60/40 towards the stronger.
	var side float64
	if err := store.pool.QueryRow(ctx, `
		SELECT (details->>'sideRating')::float8 FROM rating_changes
		WHERE match_id = $1 AND user_id = $2 AND system = $3`,
		match.ID, match.Sides[0].Players[0].UserID, glickoName).Scan(&side); err != nil {
		t.Fatal(err)
	}
	if math.Abs(side-1600) > 1e-9 {
		t.Errorf("pair rating %v, want 1600", side)
	}
}

func TestSinglesRatingShowsInDoublesUntilPlayed(t *testing.T) {
	store, srv, matches := scoringTestServer(t)
	ctx := context.Background()
	match := firstMatch(t, matches, true)
	fresh := match.Sides[0].Players[0].UserID    // has never played doubles or mixed
	seasoned := match.Sides[1].Players[0].UserID // keeps the seeded doubles rating
	if _, err := store.pool.Exec(ctx, `DELETE FROM player_ratings WHERE user_id = $1 AND discipline <> 'singles'`, fresh); err != nil {
		t.Fatal(err)
	}
	before := ratingOf(t, store, seasoned, FormatDoubles)
	playOut(t, sessionFor(t, store, srv, "usr_demo_sameer"), match.ID)

	shownSingles := ratingOf(t, store, fresh, FormatSingles).rating
	for _, d := range []string{FormatDoubles, FormatMixed} {
		var provisional bool
		r := ratingOf(t, store, fresh, d)
		if err := store.pool.QueryRow(ctx, `SELECT provisional FROM player_ratings WHERE user_id = $1 AND discipline = $2`,
			fresh, d).Scan(&provisional); err != nil {
			t.Fatal(err)
		}
		if r.rating != shownSingles || r.played != 0 || !provisional {
			t.Errorf("%s shows %+v (provisional %v), want the singles %d, provisional", d, r, provisional, shownSingles)
		}
	}
	if after := ratingOf(t, store, seasoned, FormatDoubles); after != before {
		t.Errorf("a real doubles rating changed from a singles match: %+v -> %+v", before, after)
	}
}
