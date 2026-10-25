package rating

import (
	"math"
	"time"
)

// Glicko is Mark Glickman's original Glicko system
// (https://www.glicko.net/glicko/glicko.pdf), not Glicko-2, applied after
// every match (design doc, section 9). Each player has a rating and a rating
// deviation (RD): how sure the system is. A high RD moves the rating a lot
// and makes beating that player count for less; RD shrinks with every match
// and grows while a player is away.
//
// On top of the paper, this app's extensions: in doubles a pair is rated as
// one player (see Rate), and the margin multiplier scales the rating change,
// never the RD.
type Glicko struct {
	// StartRating and StartRD are a new player's state.
	StartRating float64
	StartRD     float64
	// MaxRD caps RD: nobody is less certain than a new player.
	MaxRD float64
	// C is how fast RD grows while a player doesn't play, per Period:
	//
	//	RD* = min(sqrt(RD² + C² × periods since last rated), MaxRD)
	C      float64
	Period time.Duration
	// ProvisionalRD: a rating is provisional while its RD is at least this.
	ProvisionalRD float64
	// TransferRD is the extra uncertainty in carrying a rating to another
	// discipline: singles form says a lot about doubles, not everything.
	TransferRD float64
	// StrongerWeight is the higher-rated partner's share of a pair's rating
	// and RD.
	StrongerWeight float64
	Margin         Margin
}

// DefaultGlicko starts at 1200 with RD 350, grows RD with C = 8 a day
// (a settled RD of 60 is back to 350 after about five years away), is
// provisional from RD 130, adds RD 150 when a rating is carried to another
// discipline, weights a pair 60/40 towards the stronger partner, and uses
// the default margin.
func DefaultGlicko() Glicko {
	return Glicko{
		StartRating:    1200,
		StartRD:        350,
		MaxRD:          350,
		C:              8,
		Period:         24 * time.Hour,
		ProvisionalRD:  130,
		TransferRD:     150,
		StrongerWeight: DefaultStrongerWeight,
		Margin:         DefaultMargin(),
	}
}

func (gl Glicko) Name() string    { return "glicko-1" }
func (gl Glicko) Initial() Player { return Player{Rating: gl.StartRating, RD: gl.StartRD} }

// Carry gives a rating from earlier matches the RD it would have after that
// many matches against settled opponents, roughly StartRD / √(1 + matches).
func (gl Glicko) Carry(rating float64, matches int) Player {
	return Player{Rating: rating, RD: math.Min(gl.StartRD, gl.StartRD/math.Sqrt(1+float64(matches))), Matches: matches}
}

func (gl Glicko) Provisional(p Player) bool { return p.RD >= gl.ProvisionalRD }

// Transfer starts at the other discipline's rating, with its RD (grown for
// the time since) and TransferRD added in quadrature:
//
//	RD = min(√(RD*² + TransferRD²), MaxRD)
//
// A settled singles player (RD 60–90) starts doubles at about RD 160–175:
// provisional, so their first doubles matches move them quickly.
func (gl Glicko) Transfer(from Player, at time.Time) Player {
	rd := gl.Current(from, at)
	return Player{Rating: from.Rating, RD: math.Min(math.Sqrt(rd*rd+gl.TransferRD*gl.TransferRD), gl.MaxRD)}
}

// q is ln(10)/400, the paper's scale constant.
var q = math.Ln10 / 400

// g discounts a result against an opponent whose rating is uncertain.
func g(rd float64) float64 {
	return 1 / math.Sqrt(1+3*q*q*rd*rd/(math.Pi*math.Pi))
}

// expectedScore is the paper's E: a player rated r against an opponent
// rated rOpp with deviation rdOpp.
func expectedScore(r, rOpp, rdOpp float64) float64 {
	return 1 / (1 + math.Pow(10, -g(rdOpp)*(r-rOpp)/400))
}

// Observation is one result against one opponent in a rating period.
type Observation struct {
	Rating, RD, Score float64
}

// Update is the paper's rating-period update, steps 2 to 3: a player rated r
// with deviation rd (already grown for inactivity) against these results
// gets newRating and newRD. dSquared is the variance of the evidence.
func Update(r, rd float64, results []Observation) (newRating, newRD, dSquared float64) {
	var info, sum float64
	for _, o := range results {
		e := expectedScore(r, o.Rating, o.RD)
		info += g(o.RD) * g(o.RD) * e * (1 - e)
		sum += g(o.RD) * (o.Score - e)
	}
	dSquared = 1 / (q * q * info)
	precision := 1/(rd*rd) + 1/dSquared
	return r + q/precision*sum, math.Sqrt(1 / precision), dSquared
}

// Current is a player's RD grown for the time since they were last rated:
// the paper's step 1.
func (gl Glicko) Current(p Player, at time.Time) float64 {
	rd := p.RD
	if !p.LastRated.IsZero() && at.After(p.LastRated) && gl.Period > 0 {
		periods := float64(at.Sub(p.LastRated)) / float64(gl.Period)
		rd = math.Sqrt(rd*rd + gl.C*gl.C*periods)
	}
	return math.Min(rd, gl.MaxRD)
}

// side is a team's strength as one player: in a pair, the partners' ratings
// and RDs (grown for inactivity), each weighted towards the stronger:
//
//	rating = w × rating_stronger + (1 - w) × rating_weaker
//	RD     = w × RD_stronger     + (1 - w) × RD_weaker
//
// The RD stays on a player's scale, so a pair's rating moves about as much
// as one of its players' would.
func (gl Glicko) side(players []Player, at time.Time) (rating, rd float64) {
	if len(players) == 0 {
		return 0, gl.MaxRD
	}
	w := sideWeights(players, gl.StrongerWeight)
	return weighted(players, w, ratingOf), weighted(players, w, func(p Player) float64 { return gl.Current(p, at) })
}

// WinProbability is the paper's prediction for a game between the sides,
// which, unlike the update's E, counts both sides' uncertainty.
func (gl Glicko) WinProbability(m Match) float64 {
	ra, rda := gl.side(m.A, m.At)
	rb, rdb := gl.side(m.B, m.At)
	return 1 / (1 + math.Pow(10, -g(math.Sqrt(rda*rda+rdb*rdb))*(ra-rb)/400))
}

// Rate implements System. Each side is rated as one player, as in singles
// (see side for a pair's rating and RD), and the paper's update is worked out
// once for the side. Both partners get the side's change, and each
// partner's RD shrinks by the side's factor, so the pair's RD afterwards is
// exactly the update's. In singles that's the paper exactly.
func (gl Glicko) Rate(m Match) []Change {
	ra, rda := gl.side(m.A, m.At)
	rb, rdb := gl.side(m.B, m.At)
	pA := gl.WinProbability(m)
	dominance := gl.Margin.Dominance(m.Games)
	margin := gl.Margin.Multiplier(m)

	side := func(players []Player, own, ownRD, opp, oppRD, score, win float64) []Change {
		updated, sideRD, d2 := Update(own, ownRD, []Observation{{Rating: opp, RD: oppRD, Score: score}})
		delta := updated - own
		shrink := sideRD / ownRD
		out := make([]Change, 0, len(players))
		for _, p := range players {
			pre := gl.Current(p, m.At)
			out = append(out, Change{
				PlayerID: p.ID,
				Before:   p,
				After:    rated(p, p.Rating+delta*margin, pre*shrink, m.At),
				Details: map[string]float64{
					"sideRating":     own,
					"sideRD":         ownRD,
					"opponentRating": opp,
					"opponentRD":     oppRD,
					"rdBeforeMatch":  pre,
					"expected":       expectedScore(own, opp, oppRD),
					"winProbability": win,
					"score":          score,
					"g":              g(oppRD),
					"dSquared":       d2,
					"dominance":      dominance,
					"margin":         margin,
					// The change the margin adjusted, for judging it.
					"unadjusted": delta,
				},
			})
		}
		return out
	}
	return append(side(m.A, ra, rda, rb, rdb, m.ScoreA, pA), side(m.B, rb, rdb, ra, rda, 1-m.ScoreA, 1-pA)...)
}
