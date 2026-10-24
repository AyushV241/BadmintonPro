package main

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Judge accounts (design doc, section 10, "Judge accounts").
//
// An admin registers a judge by phone number and name. That finds the
// number's phone account, or creates it if the number has never signed in,
// and records the account in the judges table. The person then signs in with
// "Log in as judge", which is phone only and lands on the judge home page.
// A judge is still an ordinary account, so the same person can sign in
// normally and play.

// Session modes. The mode picks the home page; it never grants anything:
// judge endpoints check the judges table on every request.
const (
	SessionPlayer = "player"
	SessionJudge  = "judge"
)

var (
	// ErrNotAJudge means the account or number isn't a registered judge.
	ErrNotAJudge = errors.New("not a registered judge")
	// ErrJudgeExists means the account is already registered.
	ErrJudgeExists = errors.New("already a registered judge")
)

// RegisteredJudge is one entry in the admin's judge list.
type RegisteredJudge struct {
	UserID string `json:"userId"`
	// Name and Phone (E.164) are as the admin entered them.
	Name  string `json:"name"`
	Phone string `json:"phone"`
	// Username is empty until the person sets up a player profile.
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
}

// JudgeEvent is an event whose judge list includes the judge.
type JudgeEvent struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Status   string    `json:"status"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	Venue    Venue     `json:"venue"`
}

// JudgeHome is what the judge home page shows.
type JudgeHome struct {
	Judge RegisteredJudge `json:"judge"`
	// Events are the ones the judge is listed for and that haven't ended or
	// been cancelled, soonest first.
	Events []JudgeEvent `json:"events"`
	// Matches are the ones assigned to the judge, soonest first, cancelled
	// ones left out. Each has MySide empty: the judge plays on neither side.
	Matches []PlayerMatch `json:"matches"`
}

// JudgeStore is what judge registration, judge sign-in and the judge home
// page need. Only PostgresStore implements it.
type JudgeStore interface {
	ListJudges(ctx context.Context) ([]RegisteredJudge, error)
	// AddJudge registers an existing account, keeping the name and phone the
	// admin entered. It returns ErrJudgeExists if it's already registered and
	// ErrNoSuchUser for an unknown account.
	AddJudge(ctx context.Context, userID, name, phone, createdBy string) (RegisteredJudge, error)
	// RemoveJudge deletes the registration and takes the person off the
	// judge list of every event that hasn't ended. Matches already scheduled
	// keep their judge (they are never moved). It returns ErrNotAJudge for
	// an account that isn't registered.
	RemoveJudge(ctx context.Context, userID string) error
	// JudgeForPhone returns the account that signs in with this E.164
	// number, if it's a registered judge, and ErrNotAJudge otherwise. It
	// never creates an account.
	JudgeForPhone(ctx context.Context, phone string) (User, error)
	IsJudge(ctx context.Context, userID string) (bool, error)
	// JudgeHome returns ErrNotAJudge for an account that isn't registered.
	JudgeHome(ctx context.Context, userID string, now time.Time) (JudgeHome, error)
}

// validateJudgeName returns a problem with the name, or "".
func validateJudgeName(name string) string {
	switch n := utf8.RuneCountInString(strings.TrimSpace(name)); {
	case n == 0:
		return "name is required"
	case n > maxNameLength:
		return "name must be at most 100 characters"
	}
	return ""
}
