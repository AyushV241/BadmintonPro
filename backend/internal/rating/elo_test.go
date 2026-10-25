package rating

import (
	"math"
	"testing"
	"time"
)

// side returns established players named id, id2, … with these ratings.
func side(id string, ratings ...float64) []Player {
	ps := make([]Player, len(ratings))
	for i, r := range ratings {
		name := id
		if i > 0 {
			name = id + string(rune('1'+i))
		}
		ps[i] = Player{ID: name, Rating: r, Matches: 20}
	}
	return ps
}

func change(cs []Change, id string) float64 {
	for _, c := range cs {
		if c.PlayerID == id {
			return c.After.Rating - c.Before.Rating
		}
	}
	return math.NaN()
}

func TestExpected(t *testing.T) {
	if got := Expected(1500, 1600); math.Abs(got-0.36) > 0.005 {
		t.Errorf("Expected(1500, 1600) = %.3f, want about 0.36", got)
	}
	if got := Expected(1400, 1400); got != 0.5 {
		t.Errorf("even ratings: %v, want 0.5", got)
	}
}

func TestEloSinglesWin(t *testing.T) {
	e := DefaultElo()
	a := []Player{{ID: "a", Rating: 1400, Matches: 20}}
	b := []Player{{ID: "b", Rating: 1400, Matches: 20}}
	cs := e.Rate(Match{A: a, B: b, ScoreA: 1})
	// Established K 20, expected 0.5: +10 and -10.
	if change(cs, "a") != 10 || change(cs, "b") != -10 {
		t.Errorf("changes = %+v", cs)
	}
	if cs[0].Details["k"] != 20 || cs[0].Details["expected"] != 0.5 {
		t.Errorf("details = %v", cs[0].Details)
	}
}

func TestEloProvisionalKAndUpsets(t *testing.T) {
	e := DefaultElo()
	newcomer := []Player{{ID: "new", Rating: 1200, Matches: 3}}
	veteran := []Player{{ID: "vet", Rating: 1400, Matches: 50}}
	cs := e.Rate(Match{A: newcomer, B: veteran, ScoreA: 1})
	// expected for the newcomer ≈ 0.24: 40 × 0.76 = 30.4 → +30 with K 40;
	// the veteran loses 20 × 0.76 = 15.2 → -15 with K 20.
	if change(cs, "new") != 30 || change(cs, "vet") != -15 {
		t.Errorf("upset: new %+v, vet %+v", change(cs, "new"), change(cs, "vet"))
	}
	// Beating a weaker player earns less than beating a stronger one.
	easy := e.Rate(Match{A: side("a", 1400), B: side("b", 1200), ScoreA: 1})
	hard := e.Rate(Match{A: side("a", 1400), B: side("b", 1600), ScoreA: 1})
	if change(easy, "a") >= change(hard, "a") {
		t.Errorf("beating 1200 gave %v, beating 1600 gave %v", change(easy, "a"), change(hard, "a"))
	}
}

func TestEloDoubles(t *testing.T) {
	e := DefaultElo()
	// A pair is one player, weighted 60/40 towards the stronger partner:
	// a2 (1400, still provisional, K 40) and a1 (1200, K 20) are a 1320
	// pair with K 32; level partners are a plain 1300.
	a := []Player{{ID: "a1", Rating: 1200, Matches: 20}, {ID: "a2", Rating: 1400, Matches: 2}}
	b := []Player{{ID: "b1", Rating: 1300, Matches: 20}, {ID: "b2", Rating: 1300, Matches: 20}}
	cs := e.Rate(Match{A: a, B: b, ScoreA: 1})
	if len(cs) != 4 {
		t.Fatalf("%d changes, want 4", len(cs))
	}
	if cs[0].Details["sideRating"] != 1320 || cs[0].Details["k"] != 32 || cs[2].Details["sideRating"] != 1300 {
		t.Errorf("pairs: %v vs %v", cs[0].Details, cs[2].Details)
	}
	// Expected 0.529: 32 × 0.471 = 15.1 → +15 each; 20 × 0.471 → -9 each.
	if change(cs, "a1") != 15 || change(cs, "a2") != 15 || change(cs, "b1") != -9 || change(cs, "b2") != -9 {
		t.Errorf("doubles: %+v", cs)
	}
}

func TestEloRetirementScoresByPointShare(t *testing.T) {
	e := DefaultElo()
	// A led 15–10 when B retired: A scores 0.6.
	cs := e.Rate(Match{A: side("a", 1400), B: side("b", 1400), ScoreA: 0.6, Games: []Game{{15, 10}}})
	if change(cs, "a") != 2 || change(cs, "b") != -2 {
		t.Errorf("retirement: %+v", cs)
	}
}

func TestEloMarginOnByDefault(t *testing.T) {
	e := DefaultElo()
	whitewash := []Game{{21, 2}, {21, 3}}
	close := []Game{{21, 19}, {19, 21}, {22, 20}}

	big := e.Rate(Match{A: side("a", 1400), B: side("b", 1400), ScoreA: 1, Games: whitewash})
	tight := e.Rate(Match{A: side("a", 1400), B: side("b", 1400), ScoreA: 1, Games: close})
	// Plain Elo gives 10; the margin moves it to 11 and 9.
	if change(big, "a") != 11 || change(tight, "a") != 9 {
		t.Errorf("one-sided win %v, close win %v; want 11 and 9", change(big, "a"), change(tight, "a"))
	}
	if big[0].Details["unadjusted"] != 10 {
		t.Errorf("unadjusted = %v, want the plain 10", big[0].Details["unadjusted"])
	}
	// The sign mistake to avoid: when B wins convincingly, B's gain AND A's
	// loss both grow; the multiplier comes from the winner's side.
	bWins := e.Rate(Match{A: side("a", 1400), B: side("b", 1400), ScoreA: 0, Games: []Game{{2, 21}, {3, 21}}})
	if change(bWins, "b") != 11 || change(bWins, "a") != -11 {
		t.Errorf("convincing win for B: a %v, b %v; want -11 and +11", change(bWins, "a"), change(bWins, "b"))
	}
	// Singles stays zero-sum with equal K.
	if change(big, "a")+change(big, "b") != 0 {
		t.Errorf("not zero-sum: %+v", big)
	}
	// Off, it's plain Elo.
	e.Margin.Weight = 0
	if off := e.Rate(Match{A: side("a", 1400), B: side("b", 1400), ScoreA: 1, Games: whitewash}); change(off, "a") != 10 {
		t.Errorf("margin off: %v, want 10", change(off, "a"))
	}
}

func TestEloAfterState(t *testing.T) {
	e := DefaultElo()
	at := time.Date(2026, 10, 26, 19, 0, 0, 0, time.UTC)
	cs := e.Rate(Match{A: side("a", 1400), B: side("b", 1400), ScoreA: 1, At: at})
	if a := cs[0].After; a.Matches != 21 || !a.LastRated.Equal(at) || a.RD != 0 {
		t.Errorf("after = %+v", a)
	}
	if !e.Provisional(Player{Matches: 9}) || e.Provisional(Player{Matches: 10}) {
		t.Error("provisional for the first 10 matches only")
	}
}

// Any System can stand in for Elo.
var _ System = DefaultElo()
