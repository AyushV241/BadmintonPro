package main

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/rating"
)

// Ratings (design doc, section 9). Every rating system rates every match, in
// the transaction that ends it, and keeps its own state and history; the
// default system's rating is the one players see, mirrored into
// player_ratings. The others are published alongside for comparison.

// Rating change kinds in rating_changes.
const (
	ratingKindMatch  = "match"
	ratingKindVoid   = "void"
	ratingKindAdjust = "adjust"
)

// ratingConfig is the systems in use and which one players see.
type ratingConfig struct {
	systems []rating.System
	primary rating.System
}

// allRatingSystems are every system the app runs, starting players at the
// app's starting rating.
func allRatingSystems() []rating.System {
	gl := rating.DefaultGlicko()
	gl.StartRating = startingRating
	elo := rating.DefaultElo()
	elo.StartRating = startingRating
	return []rating.System{gl, elo}
}

// defaultRatingSystem is the one players see unless RATING_SYSTEM says
// otherwise.
const defaultRatingSystem = "glicko-1"

// newRatingConfig shows primary ("" for the default) to players, and runs
// the rest alongside.
func newRatingConfig(primary string) (ratingConfig, error) {
	if primary == "" {
		primary = defaultRatingSystem
	}
	systems := allRatingSystems()
	var names []string
	for _, s := range systems {
		if s.Name() == primary {
			return ratingConfig{systems: systems, primary: s}, nil
		}
		names = append(names, s.Name())
	}
	return ratingConfig{}, fmt.Errorf("unknown RATING_SYSTEM %q (want %s)", primary, strings.Join(names, " or "))
}

func mustRatingConfig(primary string) ratingConfig {
	c, err := newRatingConfig(primary)
	if err != nil {
		panic(err)
	}
	return c
}

func (c ratingConfig) system(name string) (rating.System, bool) {
	i := slices.IndexFunc(c.systems, func(s rating.System) bool { return s.Name() == name })
	if i < 0 {
		return nil, false
	}
	return c.systems[i], true
}

// gamesOf turns a computed score into the rating package's games.
func gamesOf(s Score) []rating.Game {
	games := make([]rating.Game, 0, len(s.Games))
	for _, g := range s.Games {
		games = append(games, rating.Game{A: g.A, B: g.B})
	}
	return games
}

// retirementScore is side A's result in a match a side retired from: its
// share of the points played, so the match is rated on the score so far.
// ok is false when no point was played, and the match isn't rated.
func retirementScore(s Score) (scoreA float64, ok bool) {
	var a, b int
	for _, g := range s.Games {
		a += g.A
		b += g.B
	}
	if a+b == 0 {
		return 0, false
	}
	return float64(a) / float64(a+b), true
}

// shown is a rating as players see it: a whole number, at least 1.
func shown(r float64) int { return max(1, int(math.Round(r))) }

// shownRD is an RD rounded for display; nil for a system without one.
func shownRD(rd *float64) *int {
	if rd == nil {
		return nil
	}
	v := int(math.Round(*rd))
	return &v
}

// LiveRatingChange is one rating change shown with a match.
type LiveRatingChange struct {
	UserID string `json:"userId"`
	// System is which rating system made it, e.g. "glicko-1".
	System string `json:"system"`
	// Kind is "match" when the match was rated, "void" when a void undid
	// it.
	Kind string `json:"kind"`
	// Before, After and Change are as players see them: whole numbers, with
	// Change = After - Before.
	Before int `json:"before"`
	After  int `json:"after"`
	Change int `json:"change"`
	// RDBefore and RDAfter are Glicko's uncertainty; null for Elo.
	RDBefore *int `json:"rdBefore"`
	RDAfter  *int `json:"rdAfter"`
	// WinProbability is the system's prediction, before the match, that
	// this player's side would win; null on a void.
	WinProbability *float64 `json:"winProbability"`
}

// PlayerRating is a player's current rating in one system and discipline.
type PlayerRating struct {
	System        string `json:"system"`
	Discipline    string `json:"discipline"`
	Rating        int    `json:"rating"`
	MatchesPlayed int    `json:"matchesPlayed"`
	Provisional   bool   `json:"provisional"`
	// RD is Glicko's uncertainty after their last match, and RDNow what it
	// has grown to since; both null for Elo.
	RD          *int       `json:"rd"`
	RDNow       *int       `json:"rdNow"`
	LastRatedAt *time.Time `json:"lastRatedAt"`
}

// RatingHistoryEntry is one change to a player's rating.
type RatingHistoryEntry struct {
	MatchID    string `json:"matchId"`
	System     string `json:"system"`
	Discipline string `json:"discipline"`
	// Kind is "match", "void", or "adjust": a void replayed later matches
	// and this player's rating moved, though they weren't in it.
	Kind           string    `json:"kind"`
	Before         int       `json:"before"`
	After          int       `json:"after"`
	Change         int       `json:"change"`
	RDBefore       *int      `json:"rdBefore"`
	RDAfter        *int      `json:"rdAfter"`
	WinProbability *float64  `json:"winProbability"`
	At             time.Time `json:"at"`
}

// PlayerRatings is everything the rating systems say about a player.
type PlayerRatings struct {
	UserID string `json:"userId"`
	// Default is the system players see.
	Default string         `json:"default"`
	Ratings []PlayerRating `json:"ratings"`
	// History is newest first, up to ratingHistoryLimit entries.
	History []RatingHistoryEntry `json:"history"`
}

// ratingHistoryLimit is how much of a player's history the API returns.
const ratingHistoryLimit = 100
