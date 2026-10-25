package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Live scoring (design doc, section 8). The judge starts a match and scores
// it rally by rally; everyone else watches the score update in real time.
// The score is never stored: it is replayed from the append-only point log
// every time, so an undo or a resent point can never leave it wrong.

// BWF rules: rally scoring to 21, win by 2, capped at 30, best of 3.
const (
	pointsToWin = 21
	pointsCap   = 30
	gamesToWin  = 2
)

// startEarly is how long before its slot a judge may start a match, for
// players who are ready early.
const startEarly = 60 * time.Minute

// How a match can end besides being played out (design doc, section 9).
// MatchWalkover and the others are in matches.go.
const (
	// MatchRetired: a side couldn't go on (injury). The other side wins,
	// and the match is rated on the points played.
	MatchRetired = "retired"
	// MatchVoid: an admin struck the match off. It doesn't count, and any
	// rating change it made is reversed.
	MatchVoid = "void"
)

// Point log kinds.
const (
	PointRally = "point"
	PointUndo  = "undo"
)

var (
	ErrNotYourMatch      = errors.New("you aren't this match's judge")
	ErrMatchNotStartable = errors.New("only a scheduled match can be started")
	ErrMatchNotLive      = errors.New("the match isn't live")
	ErrMatchDecided      = errors.New("the match is already decided: finish it, or undo the last point")
	ErrMatchNotDecided   = errors.New("the match isn't decided yet")
	ErrNothingToUndo     = errors.New("there's no point to undo")
	ErrNoWalkover        = errors.New("a walkover is only for a match that hasn't started")
	ErrNotVoidable       = errors.New("only a match that has started or ended can be voided")
)

// TooEarlyError means the judge tried to start before startEarly ahead of
// the slot.
type TooEarlyError struct{ From time.Time }

func (e *TooEarlyError) Error() string {
	return fmt.Sprintf("too early: the match can be started from %s", e.From.UTC().Format(time.RFC3339))
}

// SeqMismatchError means a point arrived out of turn. The judge's app resends
// its queue from Expected.
type SeqMismatchError struct{ Expected int }

func (e *SeqMismatchError) Error() string {
	return fmt.Sprintf("out of order: the next point must be number %d", e.Expected)
}

// PointEvent is one row of the point log.
type PointEvent struct {
	Seq  int       `json:"seq"`
	Kind string    `json:"kind"`
	Side string    `json:"side,omitempty"`
	At   time.Time `json:"at"`
}

// GameScore is one game. Winner is "" while it's being played.
type GameScore struct {
	A      int    `json:"a"`
	B      int    `json:"b"`
	Winner string `json:"winner,omitempty"`
}

// Rally is one point that counts, with the score it made.
type Rally struct {
	Seq  int       `json:"seq"`
	Side string    `json:"side"`
	Game int       `json:"game"` // 1-based
	A    int       `json:"a"`
	B    int       `json:"b"`
	At   time.Time `json:"at"`
}

// Score is the match state the point log adds up to.
type Score struct {
	// Games are the finished games, then the one in play (if any).
	Games  []GameScore `json:"games"`
	GamesA int         `json:"gamesA"`
	GamesB int         `json:"gamesB"`
	// Server is the side serving the next rally; "" before the start.
	Server string `json:"server"`
	// Winner is the match winner once decided, before the judge finishes it.
	Winner string `json:"winner,omitempty"`
	// Rallies are the points that count, newest first.
	Rallies []Rally `json:"rallies"`
	// points is how many rallies count; undo needs at least one.
	points int
}

func gameWon(a, b int) bool {
	if a == pointsCap || b == pointsCap {
		return true
	}
	return (a >= pointsToWin || b >= pointsToWin) && (a-b >= 2 || b-a >= 2)
}

// computeScore replays the log. firstServer is "" until the match starts.
// An undo cancels the most recent point that still counts.
func computeScore(firstServer string, events []PointEvent) Score {
	var counted []PointEvent
	for _, ev := range events {
		switch ev.Kind {
		case PointRally:
			counted = append(counted, ev)
		case PointUndo:
			if len(counted) > 0 {
				counted = counted[:len(counted)-1]
			}
		}
	}

	s := Score{Server: firstServer, Games: []GameScore{}, Rallies: []Rally{}}
	if firstServer == "" {
		return s
	}
	s.Games = append(s.Games, GameScore{})
	for _, ev := range counted {
		if s.Winner != "" {
			break // never stored: AddPoint refuses points once decided
		}
		cur := &s.Games[len(s.Games)-1]
		if ev.Side == "A" {
			cur.A++
		} else {
			cur.B++
		}
		// Rally scoring: whoever wins the rally serves the next one.
		s.Server = ev.Side
		s.points++
		s.Rallies = append(s.Rallies, Rally{Seq: ev.Seq, Side: ev.Side, Game: len(s.Games), A: cur.A, B: cur.B, At: ev.At})

		if gameWon(cur.A, cur.B) {
			cur.Winner = ev.Side
			if ev.Side == "A" {
				s.GamesA++
			} else {
				s.GamesB++
			}
			if s.GamesA == gamesToWin || s.GamesB == gamesToWin {
				s.Winner = ev.Side
			} else {
				// The game's winner serves first in the next one.
				s.Games = append(s.Games, GameScore{})
			}
		}
	}
	// Newest first, the way the players' screen lists them.
	for i, j := 0, len(s.Rallies)-1; i < j; i, j = i+1, j-1 {
		s.Rallies[i], s.Rallies[j] = s.Rallies[j], s.Rallies[i]
	}
	return s
}

// LiveMatch is a match as the judge console and the live screen show it.
type LiveMatch struct {
	ID       string      `json:"id"`
	Status   string      `json:"status"`
	Format   string      `json:"format"`
	Event    *MatchEvent `json:"event"`
	Venue    Venue       `json:"venue"`
	Court    Court       `json:"court"`
	StartsAt time.Time   `json:"startsAt"`
	EndsAt   time.Time   `json:"endsAt"`
	// StartedAt and CompletedAt are null until the judge starts or
	// finishes the match.
	StartedAt   *time.Time  `json:"startedAt"`
	CompletedAt *time.Time  `json:"completedAt"`
	Judge       *Judge      `json:"judge"`
	Sides       []MatchSide `json:"sides"`
	// FirstServer is who served the first rally; "" until started.
	FirstServer string  `json:"firstServer"`
	WinnerSide  *string `json:"winnerSide"`
	Score       Score   `json:"score"`
	// NextSeq is the number the judge's next point or undo must carry.
	NextSeq int `json:"nextSeq"`
	// CanStartFrom is the earliest the judge may start it.
	CanStartFrom time.Time `json:"canStartFrom"`
	// RetiredSide is the side that retired; null unless it did.
	RetiredSide *string `json:"retiredSide"`
	// Void is set once an admin voids the match.
	Void *MatchVoidInfo `json:"void"`
	// RatingSystem is the rating system players see, e.g. "glicko-1".
	RatingSystem string `json:"ratingSystem"`
	// RatingChanges are the match's rating changes in every system, oldest
	// first: one per player and system if it was rated, then one each if
	// it was voided. Filter by System for the one players see.
	RatingChanges []LiveRatingChange `json:"ratingChanges"`
}

// MatchVoidInfo is why and when a match was voided.
type MatchVoidInfo struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
	// From is the status it had before: live, completed, retired or
	// walkover.
	From string `json:"from"`
}

// PointInput is one point or undo from the judge's app.
type PointInput struct {
	Seq      int
	Kind     string
	Side     string
	ClientID string
}

// ScoringStore is what live scoring needs. Only PostgresStore implements it.
type ScoringStore interface {
	// LiveMatch returns ErrNoSuchMatch for an unknown ID.
	LiveMatch(ctx context.Context, matchID string) (LiveMatch, error)
	// StartMatch moves a scheduled or ready match to live, with who serves
	// first. It returns ErrNoSuchMatch, ErrNotYourMatch,
	// ErrMatchNotStartable or a *TooEarlyError.
	StartMatch(ctx context.Context, matchID, judgeID, firstServer string, now time.Time) (LiveMatch, error)
	// AddPoint appends a point or an undo. A client ID already in the log is
	// a resend: it returns the current state and adds nothing. Otherwise it
	// returns ErrNoSuchMatch, ErrNotYourMatch, ErrMatchNotLive, a
	// *SeqMismatchError, ErrMatchDecided or ErrNothingToUndo.
	AddPoint(ctx context.Context, matchID, judgeID string, in PointInput) (LiveMatch, error)
	// FinishMatch records the result of a decided live match and rates it.
	// It returns ErrNoSuchMatch, ErrNotYourMatch, ErrMatchNotLive or
	// ErrMatchNotDecided.
	FinishMatch(ctx context.Context, matchID, judgeID string, now time.Time) (LiveMatch, error)
	// RetireMatch ends a live, undecided match because retiredSide can't go
	// on. The other side wins; the match is rated on each side's share of
	// the points played, and not at all if none were. It returns
	// ErrNoSuchMatch, ErrNotYourMatch, ErrMatchNotLive or ErrMatchDecided.
	RetireMatch(ctx context.Context, matchID, judgeID, retiredSide string, now time.Time) (LiveMatch, error)
	// Walkover gives a match that never started to winnerSide because the
	// other side didn't turn up. It isn't rated; the absent players' entries
	// become no_show. judgeID is the match's judge, or "" for an admin, who
	// may record any match's. It returns ErrNoSuchMatch, ErrNotYourMatch,
	// ErrNoWalkover or a *TooEarlyError (same window as starting).
	Walkover(ctx context.Context, matchID, judgeID, winnerSide string, now time.Time) (LiveMatch, error)
	// VoidMatch strikes off a match that started or ended, for an admin.
	// Rating changes it made are reversed, and a walkover's no_show entries
	// go back to scheduled. It returns ErrNoSuchMatch or ErrNotVoidable.
	VoidMatch(ctx context.Context, matchID, adminID, reason string, now time.Time) (LiveMatch, error)
	// PlayerRatings is what every rating system says about a player. It
	// returns ErrNoSuchUser for an unknown ID.
	PlayerRatings(ctx context.Context, userID string, now time.Time) (PlayerRatings, error)
}
