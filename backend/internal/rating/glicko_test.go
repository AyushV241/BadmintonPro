package rating

import (
	"math"
	"testing"
	"time"
)

func near(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

// The worked example in Glickman's paper (section "Example calculation"):
// a player rated 1500, RD 200, beats a 1400 (RD 30) and loses to a 1550
// (RD 100) and a 1700 (RD 300) in one rating period.
func TestGlickoPaperExample(t *testing.T) {
	r, rd, _ := Update(1500, 200, []Observation{
		{Rating: 1400, RD: 30, Score: 1},
		{Rating: 1550, RD: 100, Score: 0},
		{Rating: 1700, RD: 300, Score: 0},
	})
	if !near(r, 1464, 0.5) || !near(rd, 151.4, 0.05) {
		t.Errorf("got %.2f / %.2f, want 1464 / 151.4", r, rd)
	}
}

// noMargin is the default system with the margin off, to check the paper's
// arithmetic on its own.
func noMargin() Glicko {
	gl := DefaultGlicko()
	gl.Margin.Weight = 0
	return gl
}

var day0 = time.Date(2026, 10, 26, 19, 0, 0, 0, time.UTC)

func TestGlickoInactivity(t *testing.T) {
	gl := DefaultGlicko()
	p := Player{RD: 50, LastRated: day0}
	// 30 days at C = 8: √(50² + 64 × 30) ≈ 66.5.
	if got := gl.Current(p, day0.Add(30*24*time.Hour)); !near(got, 66.48, 0.01) {
		t.Errorf("after 30 days: %.2f, want 66.48", got)
	}
	if got := gl.Current(p, day0.Add(20*365*24*time.Hour)); got != 350 {
		t.Errorf("after 20 years: %.2f, want the 350 cap", got)
	}
	if got := gl.Current(Player{RD: 350}, day0); got != 350 {
		t.Errorf("never rated: %.2f", got)
	}
}

func TestGlickoNewcomerBeatsEstablished(t *testing.T) {
	gl := noMargin()
	newcomer := gl.Initial()
	newcomer.ID = "new"
	settled := Player{ID: "old", Rating: 1200, RD: 80, Matches: 40, LastRated: day0}
	cs := gl.Rate(Match{A: []Player{newcomer}, B: []Player{settled}, ScoreA: 1, At: day0})
	n, o := cs[0], cs[1]
	// The newcomer's rating is unsure, so it moves a lot; beating an
	// unknown costs the settled player little. Not zero-sum, by design.
	if !near(n.After.Rating-n.Before.Rating, 175, 0.5) || !near(o.After.Rating-o.Before.Rating, -12, 0.5) {
		t.Errorf("changes %+.1f and %+.1f, want about +175 and -12",
			n.After.Rating-n.Before.Rating, o.After.Rating-o.Before.Rating)
	}
	if !near(n.After.RD, 250, 1) || n.After.Matches != 1 || !n.After.LastRated.Equal(day0) {
		t.Errorf("newcomer after: %+v", n.After)
	}
}

func TestGlickoWinNeverLowersRating(t *testing.T) {
	gl := DefaultGlicko()
	scores := [][]Game{
		{{21, 19}, {19, 21}, {22, 20}},
		{{21, 15}, {18, 21}, {21, 17}},
		{{21, 0}, {21, 0}},
		{{30, 29}, {29, 30}, {30, 29}},
	}
	for _, gap := range []float64{0, 200, 400, 800} {
		for _, games := range scores {
			fav := []Player{{ID: "fav", Rating: 1200 + gap, RD: 80, LastRated: day0}}
			dog := []Player{{ID: "dog", Rating: 1200, RD: 80, LastRated: day0}}
			cs := gl.Rate(Match{A: fav, B: dog, ScoreA: 1, Games: games, At: day0})
			if cs[0].After.Rating <= cs[0].Before.Rating || cs[1].After.Rating >= cs[1].Before.Rating {
				t.Errorf("gap %v, %v: favourite %+.2f, underdog %+.2f", gap, games,
					cs[0].After.Rating-cs[0].Before.Rating, cs[1].After.Rating-cs[1].Before.Rating)
			}
			if m := cs[0].Details["margin"]; m < 0.9 || m > 1.1 {
				t.Errorf("margin %v out of bounds", m)
			}
		}
	}
}

func TestGlickoMarginScalesTheChangeOnly(t *testing.T) {
	gl := DefaultGlicko()
	a := []Player{{ID: "a", Rating: 1300, RD: 90, LastRated: day0}}
	b := []Player{{ID: "b", Rating: 1300, RD: 90, LastRated: day0}}
	rout := gl.Rate(Match{A: a, B: b, ScoreA: 1, Games: []Game{{21, 0}, {21, 0}}, At: day0})
	plain := noMargin().Rate(Match{A: a, B: b, ScoreA: 1, Games: []Game{{21, 0}, {21, 0}}, At: day0})
	got, want := rout[0].After.Rating-1300, (plain[0].After.Rating-1300)*1.1
	if !near(got, want, 1e-9) {
		t.Errorf("rout: %+.3f, want 1.1 × plain = %+.3f", got, want)
	}
	// RD follows the paper whatever the margin.
	if rout[0].After.RD != plain[0].After.RD {
		t.Errorf("RD %v vs %v: the margin must not touch it", rout[0].After.RD, plain[0].After.RD)
	}
	// A retirement's share of points is never scaled again.
	ret := gl.Rate(Match{A: a, B: b, ScoreA: 0.75, Games: []Game{{6, 2}}, At: day0})
	if ret[0].Details["margin"] != 1 || ret[0].After.Rating <= 1300 {
		t.Errorf("retirement at 6–2 up: margin %v, change %+.2f", ret[0].Details["margin"], ret[0].After.Rating-1300)
	}
}

func TestGlickoDoubles(t *testing.T) {
	gl := noMargin()
	a := []Player{
		{ID: "a1", Rating: 1400, RD: 80, LastRated: day0},
		{ID: "a2", Rating: 1200, RD: 300, LastRated: day0},
	}
	b := []Player{
		{ID: "b1", Rating: 1250, RD: 80, LastRated: day0},
		{ID: "b2", Rating: 1350, RD: 300, LastRated: day0},
	}
	cs := gl.Rate(Match{A: a, B: b, ScoreA: 1, At: day0})
	byID := map[string]Change{}
	for _, c := range cs {
		byID[c.PlayerID] = c
	}
	// Each pair is one player, weighted 60/40 towards its stronger
	// partner. A: a1 is stronger, so 1320 with RD 0.6 × 80 + 0.4 × 300 = 168.
	// B: b2 is stronger, so 1310 with RD 0.6 × 300 + 0.4 × 80 = 212.
	d := byID["a1"].Details
	if !near(d["sideRating"], 1320, 1e-9) || !near(d["sideRD"], 168, 1e-9) ||
		!near(d["opponentRating"], 1310, 1e-9) || !near(d["opponentRD"], 212, 1e-9) {
		t.Errorf("details = %v", d)
	}
	// It's the singles update for that player, and both partners get it.
	wantR, wantRD, _ := Update(1320, 168, []Observation{{Rating: 1310, RD: 212, Score: 1}})
	up1 := byID["a1"].After.Rating - 1400
	up2 := byID["a2"].After.Rating - 1200
	if !near(up1, wantR-1320, 1e-9) || up1 != up2 || up1 <= 0 {
		t.Errorf("a1 %+.2f, a2 %+.2f; want both %+.2f", up1, up2, wantR-1320)
	}
	// Each partner's RD shrinks by the pair's factor, so the pair's RD
	// afterwards is the update's.
	rd1, rd2 := byID["a1"].After.RD, byID["a2"].After.RD
	if !near(rd1/80, rd2/300, 1e-12) || !near(0.6*rd1+0.4*rd2, wantRD, 1e-9) {
		t.Errorf("RDs %.2f and %.2f; pair RD %.3f, want %.3f", rd1, rd2, 0.6*rd1+0.4*rd2, wantRD)
	}
	if byID["b1"].After.Rating-1250 != byID["b2"].After.Rating-1350 || byID["b1"].After.Rating >= 1250 {
		t.Error("the losing pair should both go down by the same amount")
	}
	// Level partners are a plain average.
	level := gl.Rate(Match{A: []Player{{Rating: 1300, RD: 80}, {Rating: 1300, RD: 300}}, B: b, ScoreA: 1, At: day0})
	if level[0].Details["sideRating"] != 1300 || level[0].Details["sideRD"] != 190 {
		t.Errorf("level pair: %v / %v, want 1300 / 190", level[0].Details["sideRating"], level[0].Details["sideRD"])
	}
}

func TestGlickoWinProbability(t *testing.T) {
	gl := DefaultGlicko()
	settled := func(r float64) []Player { return []Player{{Rating: r, RD: 60, LastRated: day0}} }
	unsure := func(r float64) []Player { return []Player{{Rating: r, RD: 350}} }
	if p := gl.WinProbability(Match{A: settled(1400), B: settled(1400), At: day0}); p != 0.5 {
		t.Errorf("even: %v", p)
	}
	sure := gl.WinProbability(Match{A: settled(1600), B: settled(1400), At: day0})
	vague := gl.WinProbability(Match{A: unsure(1600), B: unsure(1400), At: day0})
	// Uncertainty pulls the prediction towards a coin toss.
	if !(sure > vague && vague > 0.5) {
		t.Errorf("200 up: settled %.3f, unsure %.3f", sure, vague)
	}
}

func TestGlickoStartAndProvisional(t *testing.T) {
	gl := DefaultGlicko()
	if p := gl.Initial(); p.Rating != 1200 || p.RD != 350 {
		t.Errorf("initial %+v", p)
	}
	// A seeded rating after 30 matches carries over with RD 350/√31 ≈ 63.
	if p := gl.Carry(1500, 30); p.Rating != 1500 || !near(p.RD, 62.9, 0.1) || p.Matches != 30 {
		t.Errorf("carry %+v", p)
	}
	if !gl.Provisional(Player{RD: 130}) || gl.Provisional(Player{RD: 129.9}) {
		t.Error("provisional from RD 130")
	}
}

func TestTransferToDoubles(t *testing.T) {
	gl := DefaultGlicko()
	// The example: 1800 and 1300 in singles, both settled, last rated 30
	// days before their first doubles match.
	at := day0.Add(30 * 24 * time.Hour)
	p1 := gl.Transfer(Player{Rating: 1800, RD: 70, Matches: 40, LastRated: day0}, at)
	p2 := gl.Transfer(Player{Rating: 1300, RD: 90, Matches: 25, LastRated: day0}, at)
	// Same rating; RD grown for the 30 days, then 150 added in quadrature;
	// no doubles matches yet.
	want1 := math.Sqrt(70*70 + 64*30 + 150*150)
	if p1.Rating != 1800 || !near(p1.RD, want1, 1e-9) || p1.Matches != 0 || !p1.LastRated.IsZero() {
		t.Errorf("p1 = %+v, want 1800 with RD %.1f", p1, want1)
	}
	if !gl.Provisional(p1) || !gl.Provisional(p2) {
		t.Error("a rating carried from singles starts provisional")
	}
	// Their pair leans 60/40 towards the stronger: 1600, and the RD the same.
	opp := []Player{{Rating: 1550, RD: 80, LastRated: at}}
	cs := gl.Rate(Match{A: []Player{p1, p2}, B: opp, ScoreA: 1, At: at})
	if !near(cs[0].Details["sideRating"], 1600, 1e-9) || !near(cs[0].Details["sideRD"], 0.6*p1.RD+0.4*p2.RD, 1e-9) {
		t.Errorf("pair = %v / %v", cs[0].Details["sideRating"], cs[0].Details["sideRD"])
	}
	// Someone new to singles too carries nothing: capped at 350.
	if p := gl.Transfer(gl.Initial(), at); p.Rating != 1200 || p.RD != 350 {
		t.Errorf("from a new player: %+v", p)
	}
	// Elo starts them at the singles rating, with the provisional K.
	if p := DefaultElo().Transfer(Player{Rating: 1800, Matches: 40}, at); p.Rating != 1800 || p.Matches != 0 {
		t.Errorf("elo transfer: %+v", p)
	}
}

var _ System = DefaultGlicko()
