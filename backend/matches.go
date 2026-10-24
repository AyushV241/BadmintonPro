package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Matches and the schedule: previewing and publishing what the matcher
// makes of an event's pool, the walk-in desk's offline payment, and a
// player's own matches. See docs/design/events-and-matches.md, sections 6,
// 7 and 10.

var (
	ErrSchedulePublished    = errors.New("schedule already published")
	ErrScheduleNotPublished = errors.New("schedule not published yet")
	ErrEventNotPublished    = errors.New("event isn't published")
	ErrNothingToSchedule    = errors.New("no matches to schedule")
	ErrEntryNotPending      = errors.New("entry isn't waiting for payment")
	ErrEventOver            = errors.New("event has ended")
	ErrScheduleCourtClashes = errors.New("a court is already taken at one of the scheduled times")
)

// Match statuses, as stored.
const (
	MatchScheduled = "scheduled"
	MatchReady     = "ready"
	MatchLive      = "live"
	MatchCompleted = "completed"
	MatchWalkover  = "walkover"
	MatchCancelled = "cancelled"
)

// MatchPlayer is one player on a side, with their current rating in the
// match's format.
type MatchPlayer struct {
	UserID   string `json:"userId"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Rating   int    `json:"rating"`
	// EntryID is the entry that pays for their place; admins only.
	EntryID string `json:"entryId,omitempty"`
}

// MatchSide is side "A" or "B": one player in singles, two in doubles or
// mixed. Rating is the average of its players'.
type MatchSide struct {
	Side    string        `json:"side"`
	Players []MatchPlayer `json:"players"`
	Rating  int           `json:"rating"`
}

// ScheduleMatch is one match in a previewed or published schedule.
type ScheduleMatch struct {
	// ID is null in a preview: nothing is saved until the schedule is
	// published.
	ID       *string   `json:"id"`
	Format   string    `json:"format"`
	Court    Court     `json:"court"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	// Judge is null when no judge was free; Flags then has "no judge".
	Judge *Judge      `json:"judge"`
	Sides []MatchSide `json:"sides"`
	// Flags are for the admin: "rating gap 240", "gender balance not
	// checked", "no judge".
	Flags []string `json:"flags"`
}

// ScheduleStandby is an entry the matcher couldn't place.
type ScheduleStandby struct {
	EntryID string     `json:"entryId"`
	User    PublicUser `json:"user"`
	// Reason is "no opponent", "no free slot" or "format not offered".
	Reason string `json:"reason"`
}

// ScheduleView is a schedule as admin mode shows it.
type ScheduleView struct {
	EventID string `json:"eventId"`
	// PublishedAt is when the event's schedule was published, or null.
	PublishedAt *time.Time        `json:"publishedAt"`
	Matches     []ScheduleMatch   `json:"matches"`
	Standby     []ScheduleStandby `json:"standby"`
	Warnings    []string          `json:"warnings"`
}

// AdminEntry is an entry as admin mode lists it for its event.
type AdminEntry struct {
	ID   string     `json:"id"`
	User PublicUser `json:"user"`
	// Ratings is the player's current rating in each of the entry's formats.
	Ratings       map[string]int `json:"ratings"`
	Kind          string         `json:"kind"`
	Formats       []string       `json:"formats"`
	Partner       *PublicUser    `json:"partner"`
	Source        string         `json:"source"`
	Status        string         `json:"status"`
	HoldExpiresAt *time.Time     `json:"holdExpiresAt"`
	HoldExpired   bool           `json:"holdExpired"`
	// MatchID is set once scheduled; StandbyReason while on standby.
	MatchID       *string `json:"matchId"`
	StandbyReason *string `json:"standbyReason"`
	// ConfirmedBy is the admin who marked it paid at the desk, or null.
	ConfirmedBy *PublicUser `json:"confirmedBy"`
	ConfirmedAt *time.Time  `json:"confirmedAt"`
	CreatedAt   time.Time   `json:"createdAt"`
}

// MatchEvent is the summary of a match's event shown with it.
type MatchEvent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// PlayerMatch is one of a player's matches.
type PlayerMatch struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Format string `json:"format"`
	// Event is null for a match arranged without one (future work).
	Event    *MatchEvent `json:"event"`
	Venue    Venue       `json:"venue"`
	Court    Court       `json:"court"`
	StartsAt time.Time   `json:"startsAt"`
	EndsAt   time.Time   `json:"endsAt"`
	Judge    *Judge      `json:"judge"`
	Sides    []MatchSide `json:"sides"`
	// MySide is the signed-in player's side, "A" or "B".
	MySide     string  `json:"mySide"`
	WinnerSide *string `json:"winnerSide"`
	// ChallengeID is the challenge the match was fixed for, or null for a
	// match the matcher made.
	ChallengeID *string `json:"challengeId"`
}

// MatchStore is what the schedule, the desk and a player's matches need.
// Only PostgresStore implements it.
type MatchStore interface {
	// PreviewSchedule runs the matcher on the event's confirmed open entries
	// and returns what it would publish, saving nothing. Any status works.
	// now decides whether joining is still open (a warning). It returns
	// ErrNoSuchEvent.
	PreviewSchedule(ctx context.Context, eventID string, now time.Time) (ScheduleView, error)

	// PublishSchedule does the same inside one transaction with the event
	// row locked, and saves it: matches and their sides, entries moved to
	// scheduled (with their match) or standby (with the reason), and the
	// event's schedule_published_at. Version 1 publishes once. It returns
	// ErrNoSuchEvent, ErrEventNotPublished, ErrSchedulePublished or
	// ErrNothingToSchedule.
	PublishSchedule(ctx context.Context, eventID, adminID string, now time.Time) (ScheduleView, error)

	// PublishedSchedule returns the matches a publish saved and the entries
	// it left on standby. It returns ErrNoSuchEvent or
	// ErrScheduleNotPublished.
	PublishedSchedule(ctx context.Context, eventID string) (ScheduleView, error)

	// ListEventEntries returns the event's entries in every status, in the
	// order they joined. It returns ErrNoSuchEvent.
	ListEventEntries(ctx context.Context, eventID string) ([]AdminEntry, error)

	// MarkEntryPaid confirms a pending_payment open entry paid offline at
	// the desk, recording which admin did it (a challenge's locked entries
	// are paid in the app: ErrEntryInChallenge). An expired hold is fine as long as
	// the event still has room, checked with the event row locked as
	// CreateEntry does. It records the event's fee as an offline payments
	// row in the same transaction (none for a free event). It returns
	// ErrNoSuchEntry, ErrEntryInChallenge, ErrEntryNotPending,
	// ErrEventNotOpen, ErrEventOver, ErrEntryLimit, ErrEventFull or
	// ErrNoMatchesLeft.
	MarkEntryPaid(ctx context.Context, entryID, adminID string, now time.Time) (AdminEntry, error)

	// ListUserMatches returns the user's matches in every status, soonest
	// first.
	ListUserMatches(ctx context.Context, userID string) ([]PlayerMatch, error)

	// PlayerStats returns the user's rating in each discipline (1200 where
	// they have none yet) and their finished matches.
	PlayerStats(ctx context.Context, userID string) (PlayerStats, error)
}

// PlayerStats is what a player's profile shows about their game.
type PlayerStats struct {
	// Ratings has every discipline: singles, doubles and mixed.
	Ratings map[string]int `json:"ratings"`
	// Played counts completed matches; walkovers and cancelled ones don't
	// count (they change no rating either).
	Played int `json:"played"`
	Won    int `json:"won"`
}

// scheduleData is an event's matcher input plus what's needed to show the
// result: names, court labels and the event's state.
type scheduleData struct {
	eventID              string
	status               string
	registrationClosesAt time.Time
	publishedAt          *time.Time
	input                MatcherInput
	// users has every player with an entry in the input, and every judge.
	users  map[string]PublicUser
	courts map[string]Court
}

// sideAverage is a side's rating, the average of its players' (rounded half
// up), as the matcher computes it.
func sideAverage(players []MatchPlayer) int {
	if len(players) == 0 {
		return 0
	}
	sum := 0
	for _, p := range players {
		sum += p.Rating
	}
	return (2*sum + len(players)) / (2 * len(players))
}

// renderSchedule turns the matcher's output into the admin's view. ids, when
// given, are the saved matches' IDs in schedule order.
func renderSchedule(d scheduleData, s Schedule, ids []string, now time.Time) ScheduleView {
	view := ScheduleView{
		EventID:     d.eventID,
		PublishedAt: d.publishedAt,
		Matches:     make([]ScheduleMatch, 0, len(s.Matches)),
		Standby:     make([]ScheduleStandby, 0, len(s.Standby)),
		Warnings:    append([]string{}, s.Warnings...),
	}
	for i, m := range s.Matches {
		sm := ScheduleMatch{
			Format: m.Format, Court: d.courts[m.CourtID],
			StartsAt: m.StartsAt.UTC(), EndsAt: m.EndsAt.UTC(),
			Flags: append([]string{}, m.Flags...),
		}
		if ids != nil {
			sm.ID = &ids[i]
		}
		if m.JudgeID != "" {
			u := d.users[m.JudgeID]
			sm.Judge = &Judge{UserID: u.ID, Name: u.Name, Username: u.Username}
		}
		for k, side := range m.Sides {
			ms := MatchSide{Side: sideLabel(k), Rating: side.Rating}
			for _, p := range side.Players {
				u := d.users[p.UserID]
				ms.Players = append(ms.Players, MatchPlayer{
					UserID: p.UserID, Name: u.Name, Username: u.Username, Rating: p.Rating, EntryID: p.EntryID,
				})
			}
			sm.Sides = append(sm.Sides, ms)
		}
		view.Matches = append(view.Matches, sm)
	}
	for _, e := range s.Standby {
		view.Standby = append(view.Standby, ScheduleStandby{EntryID: e.EntryID, User: d.users[e.UserID], Reason: e.Reason})
	}
	switch {
	case d.publishedAt != nil && ids == nil:
		view.Warnings = append(view.Warnings, fmt.Sprintf(
			"the schedule was already published at %s; version 1 publishes once, so this preview can't be published",
			d.publishedAt.UTC().Format(time.RFC3339)))
	case now.Before(d.registrationClosesAt):
		view.Warnings = append(view.Warnings,
			"joining is still open: entries confirmed after publishing wait for a later run, which isn't built yet")
	}
	return view
}

func sideLabel(i int) string {
	if i == 0 {
		return "A"
	}
	return "B"
}
