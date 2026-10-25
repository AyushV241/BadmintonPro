package main

import (
	"context"
	"errors"
	"time"
)

// Challenges: one player asks a named opponent to play at an event. Once
// the opponent accepts, each gets a locked entry to pay for, and once both
// have paid the match is fixed at once. See
// docs/design/events-and-matches.md, section 12.

var (
	ErrNoSuchChallenge          = errors.New("no such challenge")
	ErrChallengeNotPending      = errors.New("challenge isn't waiting for an answer")
	ErrChallengeNotWithdrawable = errors.New("challenge can't be withdrawn")
	ErrChallengeExpired         = errors.New("challenge has expired")
	ErrChallengeExists          = errors.New("an open challenge already exists between these players")
	ErrNotChallengeOpponent     = errors.New("only the opponent can answer a challenge")
	// The other player in a challenge, as seen by the one acting: the
	// opponent when sending, the challenger when accepting.
	ErrOtherOutsideBand       = errors.New("other player's rating outside the event's band")
	ErrOtherEntryLimit        = errors.New("other player has too many entries in this event")
	ErrOtherProfileIncomplete = errors.New("other player hasn't finished their profile")
	ErrEntryInChallenge       = errors.New("entry belongs to a challenge")
)

// Challenge statuses, as stored. pending → accepted → scheduled, or it ends
// as declined, withdrawn, expired or failed.
const (
	ChallengePending   = "pending"
	ChallengeAccepted  = "accepted"
	ChallengeScheduled = "scheduled"
	ChallengeDeclined  = "declined"
	ChallengeWithdrawn = "withdrawn"
	ChallengeExpired   = "expired"
	ChallengeFailed    = "failed"
)

// Roles in a challenge, as the API shows them to the viewer.
const (
	RoleChallenger = "challenger"
	RoleOpponent   = "opponent"
)

const (
	// challengeDeadline is how long the opponent has to accept and both
	// players have to pay, from sending (Decided: 4 hours). The event's
	// registration cutoff ends it sooner.
	challengeDeadline = 4 * time.Hour
	// challengeSweepEvery is how often the server expires due challenges.
	// Reads and actions also expire them lazily, so this only bounds how
	// late a refund can start for a challenge nobody looks at.
	challengeSweepEvery = 30 * time.Second
	// userChallengesLimit caps a player's challenge list until it needs
	// paging.
	userChallengesLimit = 50
	// playerSuggestionsLimit caps the recent and nearby player lists.
	playerSuggestionsLimit = 10
	// nearbyPlayerWindow is how far back an entry makes someone a regular at
	// a venue (Proposed: 60 days).
	nearbyPlayerWindow = 60 * 24 * time.Hour
)

// challengeExpiry is the challenge's one deadline: 4 hours from sending, or
// the registration cutoff if sooner.
func challengeExpiry(createdAt, registrationClosesAt time.Time) time.Time {
	deadline := createdAt.Add(challengeDeadline)
	if registrationClosesAt.Before(deadline) {
		return registrationClosesAt
	}
	return deadline
}

// Player is another player as a suggestion or a side of a challenge, with
// their singles rating.
type Player struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Rating   int    `json:"rating"`
}

// ChallengeEvent is the summary of a challenge's event shown with it.
type ChallengeEvent struct {
	ID                   string    `json:"id"`
	Title                string    `json:"title"`
	StartsAt             time.Time `json:"startsAt"`
	EndsAt               time.Time `json:"endsAt"`
	RegistrationClosesAt time.Time `json:"registrationClosesAt"`
	FeePaise             int       `json:"feePaise"`
	Currency             string    `json:"currency"`
	Venue                Venue     `json:"venue"`
}

// ChallengeEntry is the viewer's own locked entry for a challenge: what they
// pay for with POST /api/me/entries/{id}/pay.
type ChallengeEntry struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// HoldExpiresAt is the challenge's deadline.
	HoldExpiresAt *time.Time `json:"holdExpiresAt"`
}

// ChallengeMatch is the match a challenge was fixed into.
type ChallengeMatch struct {
	ID       string    `json:"id"`
	Court    Court     `json:"court"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	// Judge is null when no judge was free; the admin assigns one.
	Judge *Judge `json:"judge"`
}

// Challenge is a challenge as one of its two players sees it.
type Challenge struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Role is the viewer's: "challenger" (sent it) or "opponent".
	Role           string         `json:"role"`
	Format         string         `json:"format"`
	Event          ChallengeEvent `json:"event"`
	Challenger     Player         `json:"challenger"`
	Opponent       Player         `json:"opponent"`
	PreferredStart time.Time      `json:"preferredStart"`
	// ExpiresAt is the one deadline to accept and for both to pay.
	ExpiresAt   time.Time  `json:"expiresAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	RespondedAt *time.Time `json:"respondedAt"`
	// MyEntry is null until the challenge is accepted.
	MyEntry *ChallengeEntry `json:"myEntry"`
	// TheirEntryPaid is true once the other player's entry is paid for.
	TheirEntryPaid bool `json:"theirEntryPaid"`
	// Match is null until both have paid and the match is fixed.
	Match *ChallengeMatch `json:"match"`
}

// ChallengeInput is a validated request to send a challenge.
type ChallengeInput struct {
	ChallengerID, EventID string
	// OpponentUsername is normalised (normaliseUsername).
	OpponentUsername string
	// PreferredStart is checked against the event's window in the store.
	PreferredStart time.Time
	// Now is when it's sent: the cutoff check and the deadline count from it.
	Now time.Time
}

// ChallengeOutcome is a challenge after a read or an action, with any
// payments that must now be refunded (after the transaction committed): a
// challenge that ended with someone already paid.
type ChallengeOutcome struct {
	Challenge Challenge
	Refunds   []PendingRefund
}

// ChallengeStore is what challenges need. Only PostgresStore implements it.
//
// Locks are always taken in the order event row, challenge row, entries,
// payments, the same as ConfirmPayment, so nothing deadlocks.
type ChallengeStore interface {
	// CreateChallenge sends a challenge, pending until the opponent answers.
	// It returns ErrNoSuchEvent (drafts included), ErrEventNotOpen,
	// ErrRegistrationClosed, a validation error (the event doesn't offer
	// singles, a start outside the event's window, challenging yourself),
	// ErrNoSuchUser, ErrOtherProfileIncomplete, ErrOutsideBand,
	// ErrOtherOutsideBand, ErrEntryLimit, ErrOtherEntryLimit, ErrEventFull,
	// ErrNoMatchesLeft or ErrChallengeExists. The two entries a challenge
	// needs must fit in the event when it's sent, though nothing is held
	// until it's accepted.
	CreateChallenge(ctx context.Context, in ChallengeInput) (Challenge, error)

	// GetChallenge returns one of the user's challenges, expiring it first if
	// its deadline has passed. ErrNoSuchChallenge for anyone else's.
	GetChallenge(ctx context.Context, userID, challengeID string, now time.Time) (ChallengeOutcome, error)

	// ListUserChallenges returns the user's challenges, sent and received:
	// open ones (pending, accepted) first, then the rest, newest first, at
	// most userChallengesLimit. Call ExpireChallenges for the user first.
	ListUserChallenges(ctx context.Context, userID string) ([]Challenge, error)

	// AcceptChallenge accepts a pending challenge as its opponent: the rules
	// are checked again, with the event row locked as CreateEntry does, and
	// a locked pending_payment entry is made for each player, held until the
	// challenge's deadline. It returns CreateChallenge's errors (for either
	// player), ErrNoSuchChallenge, ErrNotChallengeOpponent,
	// ErrChallengeNotPending or ErrChallengeExpired. The outcome carries
	// refunds even with an error: a due challenge is expired first.
	AcceptChallenge(ctx context.Context, userID, challengeID string, now time.Time) (ChallengeOutcome, error)

	// DeclineChallenge declines a pending challenge as its opponent. It
	// returns ErrNoSuchChallenge, ErrNotChallengeOpponent,
	// ErrChallengeNotPending or ErrChallengeExpired.
	DeclineChallenge(ctx context.Context, userID, challengeID string, now time.Time) (ChallengeOutcome, error)

	// WithdrawChallenge pulls out of a challenge before its match is fixed:
	// the challenger while it's pending or accepted, the opponent once
	// they've accepted (before that they decline). Locked entries are
	// cancelled, and paid ones refunded. It returns ErrNoSuchChallenge,
	// ErrChallengeNotWithdrawable or ErrChallengeExpired.
	WithdrawChallenge(ctx context.Context, userID, challengeID string, now time.Time) (ChallengeOutcome, error)

	// ExpireChallenges expires every pending or accepted challenge whose
	// deadline is at or before now (only the user's, when userID isn't ""),
	// each in its own transaction: unpaid entries are cancelled, paid ones
	// refunded. It returns how many it expired and the refunds to make.
	ExpireChallenges(ctx context.Context, now time.Time, userID string) (int, []PendingRefund, error)

	// RecentPlayers returns up to playerSuggestionsLimit people the user has
	// shared a match with or challenged (either way), most recent first.
	RecentPlayers(ctx context.Context, userID string) ([]Player, error)

	// NearbyPlayers returns up to playerSuggestionsLimit other players with
	// a finished profile and an entry that isn't cancelled in the last
	// nearbyPlayerWindow, at a venue within radiusKm of near: closest
	// singles rating to the user's first, then the most recent entry.
	NearbyPlayers(ctx context.Context, userID string, near GeoPoint, radiusKm float64, now time.Time) ([]Player, error)
}

// challengeRequest is the body of POST /api/challenges.
type challengeRequest struct {
	EventID string `json:"eventId"`
	// OpponentUsername may start with "@".
	OpponentUsername string `json:"opponentUsername"`
	// PreferredStart is RFC 3339.
	PreferredStart string `json:"preferredStart"`
}

// validateChallenge checks what can be checked without the database.
func validateChallenge(req challengeRequest) (eventID, opponent string, preferred time.Time, err error) {
	if req.EventID == "" {
		return "", "", time.Time{}, validationError("pick an event")
	}
	if opponent = normaliseUsername(req.OpponentUsername); opponent == "" {
		return "", "", time.Time{}, validationError("pick someone to challenge")
	}
	preferred, err = time.Parse(time.RFC3339, req.PreferredStart)
	if err != nil {
		return "", "", time.Time{}, validationError("preferredStart must be a date and time like 2026-10-17T19:30:00+05:30")
	}
	// Slots start on whole minutes.
	return req.EventID, opponent, preferred.Truncate(time.Minute).UTC(), nil
}

// --- fixing the match (pure) ---

// SlotRequest is what fixing a challenge's match looks at.
type SlotRequest struct {
	// StartsAt and EndsAt are the event's window; Length the singles slot.
	StartsAt, EndsAt time.Time
	Length           time.Duration
	Preferred        time.Time
	Courts           []Court
	JudgeIDs         []string
	// PlayerIDs are the challenge's two players.
	PlayerIDs []string
	// Taken are the matches already on the event's courts or in the event.
	Taken []FixedMatch
}

// SlotChoice is where a challenge's match goes.
type SlotChoice struct {
	CourtID          string
	StartsAt, EndsAt time.Time
	// JudgeID is "" when no judge is free.
	JudgeID string
}

// findChallengeSlot picks the challenge's slot (section 12): the start
// nearest the preferred one (the earlier on a tie) at which some court is
// free for the whole slot inside the event's window and neither player has
// another match closer than the shorter of the two slot lengths (section 6:
// never overlapping or back-to-back). Among courts free then, the first in
// courtOrder. ok is false when nothing fits.
//
// The judge is one of the event's judges who isn't one of the players and
// isn't playing or judging another match at an overlapping time, with the
// fewest matches to judge already (then the lowest user ID), as the matcher
// does; none free leaves JudgeID "". Fixture editing picks slots the same
// way: see findSlot.
func findChallengeSlot(r SlotRequest) (SlotChoice, bool) {
	return findSlot(r)
}
