package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Entries: one match for one player in an event. See
// docs/design/events-and-matches.md, sections 4.2 and 5.

var (
	ErrNoSuchEntry         = errors.New("no such entry")
	ErrEventNotOpen        = errors.New("event isn't open for joining")
	ErrRegistrationClosed  = errors.New("registration closed")
	ErrOutsideBand         = errors.New("rating outside the event's band")
	ErrEntryLimit          = errors.New("too many entries in this event")
	ErrEventFull           = errors.New("event full")
	ErrNoMatchesLeft       = errors.New("no matches left")
	ErrEntryNotCancellable = errors.New("entry can't be cancelled")
)

// Entry kinds, sources and statuses, as stored.
const (
	EntryOpen   = "open"
	EntryLocked = "locked" // from a challenge (step 7)

	SourceOnline = "online"
	SourceWalkIn = "walk_in" // step 10; not made yet

	EntryPendingPayment = "pending_payment"
	EntryConfirmed      = "confirmed"
	EntryStandby        = "standby"
	EntryScheduled      = "scheduled"
	EntryCancelled      = "cancelled"
	EntryRefunded       = "refunded"
	EntryNoShow         = "no_show"
)

const (
	// entryHold is how long a pending_payment entry keeps its place while the
	// player pays (Proposed: 10 minutes).
	entryHold = 10 * time.Minute
	// maxEntriesPerPlayer is how many live entries one player may hold in one
	// event (Proposed: 3).
	maxEntriesPerPlayer = 3
	// startingRating is every player's Elo in each discipline (Decided: 1200).
	startingRating = 1200
)

// Entry is one of a player's entries, with the event it's for.
//
// An entry is live, holding a place in its event, while it is confirmed,
// standby or scheduled, or pending_payment with HoldExpiresAt still ahead.
type Entry struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// ChallengeID is the challenge a locked entry belongs to; null for an
	// open entry.
	ChallengeID *string  `json:"challengeId"`
	Formats     []string `json:"formats"`
	// Partner is the doubles or mixed partner chosen when joining, or nil.
	Partner *PublicUser `json:"partner"`
	Source  string      `json:"source"`
	Status  string      `json:"status"`
	// HoldExpiresAt is when an unpaid entry gives up its place.
	HoldExpiresAt *time.Time `json:"holdExpiresAt"`
	// HoldExpired is true for a pending_payment entry whose hold has run out:
	// it no longer counts towards the event and can't be paid for (a payment
	// already under way is still confirmed if there's room). The status
	// stays pending_payment; nothing sweeps it.
	HoldExpired bool       `json:"holdExpired"`
	CreatedAt   time.Time  `json:"createdAt"`
	Event       EntryEvent `json:"event"`
}

// EntryEvent is the summary of an entry's event shown with it.
type EntryEvent struct {
	ID                   string    `json:"id"`
	Title                string    `json:"title"`
	Status               string    `json:"status"`
	StartsAt             time.Time `json:"startsAt"`
	EndsAt               time.Time `json:"endsAt"`
	RegistrationClosesAt time.Time `json:"registrationClosesAt"`
	FeePaise             int       `json:"feePaise"`
	Currency             string    `json:"currency"`
	Venue                Venue     `json:"venue"`
}

// EntryInput is a validated request to join an event with an open entry.
type EntryInput struct {
	EventID, UserID string
	// Formats are normalised (normaliseFormats).
	Formats []string
	// PartnerUsername is normalised (normaliseUsername); "" for no partner.
	PartnerUsername string
	// Now is "the current time" for the registration cutoff, holds and the
	// new hold's expiry; a parameter so tests control it.
	Now time.Time
}

// EntryStore is what joining needs. Only PostgresStore implements it.
type EntryStore interface {
	// CreateEntry adds an open, online entry as pending_payment, holding its
	// place for entryHold. The event's rules are checked in the same
	// transaction as the insert, with the event row locked, so two players
	// can't both take the last place. It returns ErrNoSuchEvent (drafts
	// included), ErrEventNotOpen, ErrRegistrationClosed, a validation error
	// (formats the event doesn't offer, a bad partner), ErrNoSuchUser (no
	// partner with that username), ErrOutsideBand, ErrEntryLimit,
	// ErrEventFull or ErrNoMatchesLeft.
	CreateEntry(ctx context.Context, in EntryInput) (Entry, error)

	// ListUserEntries returns the user's entries in every status, newest
	// first.
	ListUserEntries(ctx context.Context, userID string) ([]Entry, error)

	// CancelEntry cancels the user's own pending_payment entry; the row is
	// kept. It returns ErrNoSuchEntry (also for someone else's entry),
	// ErrEntryInChallenge (a locked entry: withdraw or decline the challenge
	// instead) or ErrEntryNotCancellable.
	CancelEntry(ctx context.Context, userID, entryID string) (Entry, error)

	// Paying for an entry, and the confirmation that follows, is
	// PaymentStore's (payments.go).
}

// entryRequest is the body of POST /api/events/{id}/entries.
type entryRequest struct {
	Formats []string `json:"formats"`
	// PartnerUsername is optional, for doubles or mixed; "@" is allowed.
	PartnerUsername string `json:"partnerUsername"`
}

// validateEntry checks what can be checked without the event: formats are
// known, and a partner comes with a pairs format.
func validateEntry(req entryRequest) (formats []string, partner string, err error) {
	if formats, err = normaliseFormats(req.Formats); err != nil {
		return nil, "", err
	}
	partner = normaliseUsername(req.PartnerUsername)
	if partner != "" && !slices.Contains(formats, FormatDoubles) && !slices.Contains(formats, FormatMixed) {
		return nil, "", validationError("a partner is only for doubles or mixed")
	}
	return formats, partner, nil
}

// normaliseUsername turns what a person typed into the stored form: trimmed,
// without a leading "@", lowercase.
func normaliseUsername(s string) string {
	return strings.ToLower(strings.TrimLeft(strings.TrimSpace(s), "@"))
}

// bandRatings are the player's ratings checked against an event's Elo band
// when they join with these formats, one per format. Every one must be in
// the band (Proposed): the matcher may place the entry in any of its
// formats.
func bandRatings(ctx context.Context, q rowsQuerier, userID string, formats []string) ([]int, error) {
	ratings, err := loadRatings(ctx, q, []string{userID})
	if err != nil {
		return nil, err
	}
	out := make([]int, len(formats))
	for i, f := range formats {
		out[i] = ratings.of(userID, f)
	}
	return out, nil
}

// ratingTable is players' ratings by user ID, then discipline.
type ratingTable map[string]map[string]int

// of is the user's rating in a discipline: startingRating until they have a
// row. Rows are created lazily, on a player's first rated match (step 9).
func (t ratingTable) of(userID, discipline string) int {
	if r, ok := t[userID][discipline]; ok {
		return r
	}
	return startingRating
}

// loadRatings reads player_ratings for these users, every discipline.
func loadRatings(ctx context.Context, q rowsQuerier, userIDs []string) (ratingTable, error) {
	rows, err := q.Query(ctx, `SELECT user_id, discipline, rating FROM player_ratings WHERE user_id = ANY($1)`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("load ratings: %w", err)
	}
	defer rows.Close()
	t := ratingTable{}
	for rows.Next() {
		var userID, discipline string
		var rating int
		if err := rows.Scan(&userID, &discipline, &rating); err != nil {
			return nil, fmt.Errorf("load ratings: %w", err)
		}
		if t[userID] == nil {
			t[userID] = map[string]int{}
		}
		t[userID][discipline] = rating
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load ratings: %w", err)
	}
	return t, nil
}

// inBand reports whether rating is inside [min, max]; nil means no limit.
func inBand(rating int, ratingMin, ratingMax *int) bool {
	return (ratingMin == nil || rating >= *ratingMin) && (ratingMax == nil || rating <= *ratingMax)
}
