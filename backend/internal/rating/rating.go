// Package rating turns match results into player ratings. The app depends on
// the System interface only: Elo and Glicko are both implementations, every
// system rates every match side by side, and every stored change records
// which system made it. One of them is the rating players see; the others
// are kept for comparison.
package rating

import "time"

// Player is one player's state in a system and discipline: what it knew of
// them just before a match, or after it.
type Player struct {
	ID     string
	Rating float64
	// RD is the rating deviation, how uncertain the rating is, for a system
	// that models it (Glicko). 0 for one that doesn't (Elo).
	RD float64
	// Matches is how many rated matches they've played in the discipline.
	Matches int
	// LastRated is when the system last rated them; zero if never. Glicko's
	// uncertainty grows with the time since.
	LastRated time.Time
}

// Game is one game's score: finished, or in play when the match ended.
type Game struct{ A, B int }

// Match is a result to rate. Each side has one player in singles, two in
// doubles and mixed.
type Match struct {
	A, B []Player
	// ScoreA is side A's result: 1 if A won, 0 if A lost, and in between
	// for a match decided another way (a retirement is rated on A's share
	// of the points played). Side B's is 1 - ScoreA. The margin of victory
	// only scales a win or a loss: a share already reflects the score.
	ScoreA float64
	// Games are the scores, for the margin of victory.
	Games []Game
	// At is when the match ended; it becomes each player's LastRated.
	At time.Time
}

// Change is one player's state before and after a match, with the working
// that produced it, kept in the rating history so a surprising change can be
// explained later.
type Change struct {
	PlayerID string
	Before   Player
	After    Player
	Details  map[string]float64
}

// System is a rating system.
type System interface {
	// Name identifies the system and its version in the rating history,
	// e.g. "glicko-1".
	Name() string
	// Initial is a new player's state.
	Initial() Player
	// Carry is the state for a player this system hasn't rated yet who
	// already has a rating from matches before it (seeded, or set by
	// another system).
	Carry(rating float64, matches int) Player
	// Transfer is a player's starting state in a discipline they haven't
	// played, from their state in one they have (their singles rating for
	// their first doubles or mixed match), as of at.
	Transfer(from Player, at time.Time) Player
	// Rate returns every player's change. Every side's expectation comes
	// from the players' states before the match, never from one already
	// updated.
	Rate(m Match) []Change
	// WinProbability is side A's chance of winning, worked out before the
	// result: the prediction the systems are compared on.
	WinProbability(m Match) float64
	// Provisional says whether a rating is still settling.
	Provisional(p Player) bool
}

// decisive is a plain win or loss, as opposed to a share of points.
func decisive(score float64) bool { return score == 0 || score == 1 }

// DefaultStrongerWeight is the stronger partner's share of a pair's rating
// and RD (design doc, section 9).
const DefaultStrongerWeight = 0.6

// sideWeights are each player's share of their side. Alone, all of it. In
// a pair, the higher-rated partner gets stronger and the other the rest;
// level partners get half each.
func sideWeights(players []Player, stronger float64) []float64 {
	w := make([]float64, len(players))
	if len(players) == 2 && players[0].Rating != players[1].Rating {
		hi := 0
		if players[1].Rating > players[0].Rating {
			hi = 1
		}
		w[hi], w[1-hi] = stronger, 1-stronger
		return w
	}
	for i := range w {
		w[i] = 1 / float64(len(players))
	}
	return w
}

// weighted is Σ wᵢ × value(playerᵢ).
func weighted(players []Player, w []float64, value func(Player) float64) float64 {
	sum := 0.0
	for i, p := range players {
		sum += w[i] * value(p)
	}
	return sum
}

func ratingOf(p Player) float64 { return p.Rating }

// rated is the player after a match: the new rating and RD, one more match,
// rated at the match's time.
func rated(p Player, rating, rd float64, at time.Time) Player {
	p.Rating, p.RD = rating, rd
	p.Matches++
	p.LastRated = at
	return p
}
