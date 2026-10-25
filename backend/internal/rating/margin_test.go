package rating

import (
	"math"
	"testing"
)

func TestDominance(t *testing.T) {
	mg := DefaultMargin()
	big := mg.Dominance([]Game{{21, 10}, {21, 11}})
	close := mg.Dominance([]Game{{21, 19}, {21, 19}})
	threeGames := mg.Dominance([]Game{{21, 19}, {19, 21}, {21, 19}})
	if !(big > close && close > threeGames) {
		t.Errorf("dominance: 21–10 21–11 %.3f, 21–19 21–19 %.3f, three close games %.3f", big, close, threeGames)
	}
	// It's the same from either side.
	if flipped := mg.Dominance([]Game{{10, 21}, {11, 21}}); math.Abs(flipped-big) > 1e-9 {
		t.Errorf("dominance isn't symmetric: %v vs %v", flipped, big)
	}
	if mg.Dominance(nil) != 0 || mg.Dominance([]Game{{0, 0}}) != 0 {
		t.Error("no points should be 0")
	}
}

func TestMarginMultiplier(t *testing.T) {
	mg := DefaultMargin()
	win := func(games ...Game) float64 { return mg.Multiplier(Match{ScoreA: 1, Games: games}) }
	cases := []struct {
		name     string
		got      float64
		low, top float64
	}{
		{"2–1 win", win(Game{21, 15}, Game{18, 21}, Game{21, 17}), 0.94, 0.97},
		{"typical 2–0", win(Game{21, 15}, Game{21, 16}), 0.99, 1.01},
		{"rout", win(Game{21, 10}, Game{21, 11}), 1.01, 1.03},
		{"21–0, 21–0 hits the cap", win(Game{21, 0}, Game{21, 0}), 1.1, 1.1},
	}
	for _, c := range cases {
		if c.got < c.low-1e-9 || c.got > c.top+1e-9 {
			t.Errorf("%s: %.4f, want %.2f–%.2f", c.name, c.got, c.low, c.top)
		}
	}
	// Never applied to a share of points (a retirement), nor without scores.
	if m := mg.Multiplier(Match{ScoreA: 0.75, Games: []Game{{6, 2}}}); m != 1 {
		t.Errorf("retirement: %v, want 1", m)
	}
	if m := mg.Multiplier(Match{ScoreA: 1}); m != 1 {
		t.Errorf("no scores: %v, want 1", m)
	}
	mg.Weight = 0
	if m := mg.Multiplier(Match{ScoreA: 1, Games: []Game{{21, 0}, {21, 0}}}); m != 1 {
		t.Errorf("off: %v, want 1", m)
	}
}
