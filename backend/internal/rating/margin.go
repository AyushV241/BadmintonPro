package rating

import "math"

// Margin is the margin-of-victory multiplier both systems share (design doc,
// section 9). It scales the size of a win's or a loss's change, never its
// direction, so a win can't lower a rating:
//
//	dominance  = SetWeight × games / GamesToWin + (1 - SetWeight) × points
//	multiplier = clamp(1 + Weight × (dominance - Pivot), 1 - Cap, 1 + Cap)
//
// where games is the difference in games won and points is the mean over
// games of |a - b| / (a + b). With the defaults, a 2–1 win comes out about
// 0.95, a typical 2–0 about 1, and only a 21–0, 21–0 reaches 1.1.
type Margin struct {
	// Weight is how strongly dominance moves the multiplier; 0 turns it
	// off.
	Weight float64
	// Cap bounds the multiplier to [1 - Cap, 1 + Cap].
	Cap float64
	// Pivot is the dominance at which the multiplier is exactly 1.
	Pivot float64
	// SetWeight is how much of dominance comes from games won rather than
	// points (the rest).
	SetWeight float64
	// GamesToWin is 2 for best of 3.
	GamesToWin int
}

// DefaultMargin is on, at most ±10%.
func DefaultMargin() Margin {
	return Margin{Weight: 0.2, Cap: 0.1, Pivot: 0.5, SetWeight: 0.4, GamesToWin: 2}
}

// Dominance is how one-sided the games were, from 0 (dead even) to 1 (a
// whitewash). It's the same from either side, and 0 with no points played.
func (mg Margin) Dominance(games []Game) float64 {
	d, _ := mg.dominance(games)
	return d
}

// dominance also says whether any point was played.
func (mg Margin) dominance(games []Game) (float64, bool) {
	var wonA, wonB, played int
	var points float64
	for _, g := range games {
		total := g.A + g.B
		if total == 0 {
			continue
		}
		played++
		points += math.Abs(float64(g.A-g.B)) / float64(total)
		if g.A > g.B {
			wonA++
		} else if g.B > g.A {
			wonB++
		}
	}
	if played == 0 {
		return 0, false
	}
	points /= float64(played)
	sets := 0.0
	if mg.GamesToWin > 0 {
		sets = math.Min(1, math.Abs(float64(wonA-wonB))/float64(mg.GamesToWin))
	}
	return mg.SetWeight*sets + (1-mg.SetWeight)*points, true
}

// Multiplier scales every player's change in the match alike: a convincing
// loss costs more just as a convincing win earns more. It's 1 when the
// margin is off, for a result that isn't a plain win or loss, and when
// there are no scores to judge.
func (mg Margin) Multiplier(m Match) float64 {
	d, ok := mg.dominance(m.Games)
	if mg.Weight == 0 || !decisive(m.ScoreA) || !ok {
		return 1
	}
	raw := 1 + mg.Weight*(d-mg.Pivot)
	return math.Max(1-mg.Cap, math.Min(1+mg.Cap, raw))
}
