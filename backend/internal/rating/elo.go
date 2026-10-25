package rating

import (
	"math"
	"time"
)

// Elo is the Elo rating system (design doc, section 9).
//
//	expected_A = 1 / (1 + 10^((rating_B - rating_A) / 400))
//	change     = round(K × (result_A - expected_A) × margin)
//
// A side is rated as one player: in a pair, its rating and its K are the
// partners', weighted towards the stronger (StrongerWeight), and both get the
// side's change.
type Elo struct {
	// StartRating is a new player's rating.
	StartRating float64
	// ProvisionalK applies for a player's first ProvisionalMatches rated
	// matches in a discipline, so newcomers find their level quickly;
	// EstablishedK after that.
	ProvisionalK       float64
	EstablishedK       float64
	ProvisionalMatches int
	// StrongerWeight is the higher-rated partner's share of a pair's rating
	// and K.
	StrongerWeight float64
	Margin         Margin
}

// DefaultElo starts at 1200, with K 40 for a player's first 10 rated
// matches in a discipline and 20 after, and the default margin.
func DefaultElo() Elo {
	return Elo{
		StartRating:        1200,
		ProvisionalK:       40,
		EstablishedK:       20,
		ProvisionalMatches: 10,
		StrongerWeight:     DefaultStrongerWeight,
		Margin:             DefaultMargin(),
	}
}

func (e Elo) Name() string                       { return "elo-1" }
func (e Elo) Initial() Player                    { return Player{Rating: e.StartRating} }
func (e Elo) Carry(rating float64, n int) Player { return Player{Rating: rating, Matches: n} }

// Transfer starts at the other discipline's rating, as a new player there:
// K is the provisional one until they've played enough in it.
func (e Elo) Transfer(from Player, _ time.Time) Player { return Player{Rating: from.Rating} }

func (e Elo) Provisional(p Player) bool      { return p.Matches < e.ProvisionalMatches }
func (e Elo) WinProbability(m Match) float64 { return Expected(e.sideRating(m.A), e.sideRating(m.B)) }

func (e Elo) sideRating(players []Player) float64 {
	return weighted(players, sideWeights(players, e.StrongerWeight), ratingOf)
}

// Expected is side A's expected result against side B.
func Expected(ratingA, ratingB float64) float64 {
	return 1 / (1 + math.Pow(10, (ratingB-ratingA)/400))
}

func (e Elo) k(p Player) float64 {
	if p.Matches < e.ProvisionalMatches {
		return e.ProvisionalK
	}
	return e.EstablishedK
}

// Rate implements System.
func (e Elo) Rate(m Match) []Change {
	ra, rb := e.sideRating(m.A), e.sideRating(m.B)
	expA := Expected(ra, rb)
	dominance := e.Margin.Dominance(m.Games)
	margin := e.Margin.Multiplier(m)

	side := func(players []Player, own, opp, score, expected float64) []Change {
		k := weighted(players, sideWeights(players, e.StrongerWeight), e.k)
		delta := math.Round(k * (score - expected) * margin)
		out := make([]Change, 0, len(players))
		for _, p := range players {
			out = append(out, Change{
				PlayerID: p.ID,
				Before:   p,
				After:    rated(p, p.Rating+delta, 0, m.At),
				Details: map[string]float64{
					"sideRating":     own,
					"opponentRating": opp,
					"expected":       expected,
					"winProbability": expected,
					"score":          score,
					"k":              k,
					"dominance":      dominance,
					"margin":         margin,
					// The change the margin adjusted, for judging it.
					"unadjusted": math.Round(k * (score - expected)),
				},
			})
		}
		return out
	}
	return append(side(m.A, ra, rb, m.ScoreA, expA), side(m.B, rb, ra, 1-m.ScoreA, 1-expA)...)
}
