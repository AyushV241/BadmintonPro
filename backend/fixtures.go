package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Fixture editing: an admin making, changing and cancelling an event's
// matches by hand (design doc, sections 6 and 10). Every action keeps the
// slot rules the matcher keeps (a free court inside the event's window, a
// gap between one player's matches, a free judge who isn't playing,
// max_matches) and leaves an audit row in match_changes.
//
// This file has the types and the pure slot logic; fixtures_postgres.go
// does the rest inside transactions.

var (
	ErrNoSuchMatch = errors.New("no such match")
	// errConflict is a 409 with a message for the admin, like errValidation
	// for a 400.
	errConflict = errors.New("conflict")
)

// conflictError is a 409 with a message for the admin: a court that's
// taken, a player too close to another match, a limit reached.
func conflictError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errConflict, fmt.Sprintf(format, args...))
}

// What happens to the entry a replacement takes out of its match.
const (
	// OutcomeNoShow marks it no_show: the player keeps no refund (section
	// 4.4).
	OutcomeNoShow = "no_show"
	// OutcomeBackToPool returns it to the pool, confirmed and waiting.
	OutcomeBackToPool = "back_to_pool"
)

// Audit actions, as stored in match_changes.action.
const (
	ChangeCreate  = "create"
	ChangeReplace = "replace"
	ChangeMove    = "move"
	ChangeCancel  = "cancel"
)

// matchChangesLimit caps an event's change list.
const matchChangesLimit = 100

// ManualMatchInput is a match an admin makes by hand, or asks a slot for.
type ManualMatchInput struct {
	EventID, Format string
	// Sides are entry IDs: side A then side B when making the match. A
	// suggestion has one group with every entry, sides not yet chosen.
	Sides [][]string
	// CourtID is "" to pick one; StartsAt nil to pick the time.
	CourtID  string
	StartsAt *time.Time
	// AdminID is who made it (for the audit row).
	AdminID string
}

// SlotSuggestion is where a match made by hand would go, before saving.
type SlotSuggestion struct {
	Court    Court     `json:"court"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	// Judge is null when no judge is free; Flags then has "no judge".
	Judge *Judge   `json:"judge"`
	Flags []string `json:"flags"`
}

// FreeSlot is a court free for one slot of a format.
type FreeSlot struct {
	Court    Court     `json:"court"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
}

// ReplaceInput swaps one entry in a match for a waiting one.
type ReplaceInput struct {
	MatchID, AdminID      string
	OutEntryID, InEntryID string
	// Outcome is OutcomeNoShow or OutcomeBackToPool.
	Outcome string
}

// MoveInput gives a match a new court and start.
type MoveInput struct {
	MatchID, AdminID, CourtID string
	StartsAt                  time.Time
}

// MatchChange is one audit row: an admin's change to a match.
type MatchChange struct {
	ID      string     `json:"id"`
	MatchID string     `json:"matchId"`
	Action  string     `json:"action"`
	Admin   PublicUser `json:"admin"`
	// Details has the before and after; the shape depends on the action.
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"createdAt"`
}

// changeSlot is a match's place in an audit row's details.
type changeSlot struct {
	CourtID    string    `json:"courtId"`
	CourtLabel string    `json:"courtLabel"`
	StartsAt   time.Time `json:"startsAt"`
	EndsAt     time.Time `json:"endsAt"`
	// JudgeID is null without a judge.
	JudgeID *string `json:"judgeId"`
}

// changeEntry is an entry and its player in an audit row's details.
type changeEntry struct {
	EntryID string `json:"entryId"`
	UserID  string `json:"userId"`
}

// --- the pure slot logic ---

// playerBusy is a stretch one of the players is taken, with the gap their
// next match must keep from it: the shorter of the two slot lengths for a
// match they play (section 6: never overlapping or back-to-back), none for
// one they judge (they just can't be in two places).
type playerBusy struct {
	busy
	gap time.Duration
	// userID is the player; judging is true for a match they judge.
	userID  string
	judging bool
}

// blocks reports whether a slot [start, end) is too close to it: the two,
// each widened by the gap, overlap.
func (b playerBusy) blocks(start, end time.Time) bool {
	return start.Before(b.end.Add(b.gap)) && end.Add(b.gap).After(b.start)
}

// playersBusy is when the request's players are taken, for a slot of the
// request's length.
func playersBusy(r SlotRequest) []playerBusy {
	var out []playerBusy
	for _, f := range r.Taken {
		b := busy{start: f.StartsAt, end: f.EndsAt}
		for _, u := range r.PlayerIDs {
			switch {
			case slices.Contains(f.UserIDs, u):
				out = append(out, playerBusy{busy: b, gap: min(r.Length, b.length()), userID: u})
			case f.JudgeID == u:
				out = append(out, playerBusy{busy: b, userID: u, judging: true})
			}
		}
	}
	return out
}

// findSlot picks a slot (section 6 rules): the start nearest r.Preferred
// (the earlier on a tie) at which some court is free for the whole slot
// inside the event's window and no player has another match closer than the
// shorter of the two slot lengths, or judges one at the same time. Among
// courts free then, the first in courtOrder. With Preferred at the event's
// start, that is the earliest slot. ok is false when nothing fits.
//
// Every constraint rules out an open stretch of start times, so the free
// starts form closed stretches whose ends are the window's ends or a taken
// match's edges (less the slot, or widened by the gap). The nearest free
// start is the preferred one or one of those ends, so only they are tried.
//
// The judge is pickJudge's.
func findSlot(r SlotRequest) (SlotChoice, bool) {
	length := r.Length
	latest := r.EndsAt.Add(-length)
	if length <= 0 || latest.Before(r.StartsAt) || len(r.Courts) == 0 {
		return SlotChoice{}, false
	}
	courts := courtOrder(r.Courts)
	courtBusy := map[string][]busy{}
	for _, f := range r.Taken {
		courtBusy[f.CourtID] = append(courtBusy[f.CourtID], busy{start: f.StartsAt, end: f.EndsAt})
	}
	players := playersBusy(r)

	candidates := []time.Time{r.Preferred, r.StartsAt, latest}
	for _, c := range courts {
		for _, b := range courtBusy[c.ID] {
			candidates = append(candidates, b.end, b.start.Add(-length))
		}
	}
	for _, b := range players {
		candidates = append(candidates, b.end.Add(b.gap), b.start.Add(-b.gap-length))
	}
	distance := func(t time.Time) time.Duration {
		d := t.Sub(r.Preferred)
		if d < 0 {
			return -d
		}
		return d
	}
	slices.SortFunc(candidates, func(a, b time.Time) int {
		if c := cmp.Compare(distance(a), distance(b)); c != 0 {
			return c
		}
		return a.Compare(b)
	})

	var choice SlotChoice
search:
	for _, t := range candidates {
		if t.Before(r.StartsAt) || t.After(latest) {
			continue
		}
		end := t.Add(length)
		for _, b := range players {
			if b.blocks(t, end) {
				continue search
			}
		}
		for _, c := range courts {
			if !overlapsAny(courtBusy[c.ID], t, end) {
				choice = SlotChoice{CourtID: c.ID, StartsAt: t, EndsAt: end}
				break search
			}
		}
	}
	if choice.CourtID == "" {
		return SlotChoice{}, false
	}
	choice.JudgeID = pickJudge(r.JudgeIDs, r.Taken, r.PlayerIDs, choice.StartsAt, choice.EndsAt)
	return choice, true
}

// judgeFree reports whether judge can take a match [start, end) with these
// players: not one of them, and not playing or judging another match at an
// overlapping time.
func judgeFree(judge string, taken []FixedMatch, players []string, start, end time.Time) bool {
	if slices.Contains(players, judge) {
		return false
	}
	for _, f := range taken {
		if (f.JudgeID == judge || slices.Contains(f.UserIDs, judge)) && start.Before(f.EndsAt) && end.After(f.StartsAt) {
			return false
		}
	}
	return true
}

// pickJudge is the judge for a match [start, end) with these players, as
// the matcher chooses one: one of the event's judges who is free
// (judgeFree), with the fewest matches to judge already (then the lowest
// user ID). "" when none is free.
func pickJudge(judgeIDs []string, taken []FixedMatch, players []string, start, end time.Time) string {
	judged := map[string]int{}
	for _, f := range taken {
		if f.JudgeID != "" {
			judged[f.JudgeID]++
		}
	}
	judges := slices.Clone(judgeIDs)
	slices.Sort(judges)
	judges = slices.Compact(judges)
	chosen := ""
	for _, j := range judges {
		if !judgeFree(j, taken, players, start, end) {
			continue
		}
		if chosen == "" || judged[j] < judged[chosen] {
			chosen = j
		}
	}
	return chosen
}

// playerConflict is the first of the request's players who can't play a
// slot [start, start+Length): the busy stretch in the way (earliest first,
// then by user ID), or nil.
func playerConflict(r SlotRequest, start time.Time) *playerBusy {
	end := start.Add(r.Length)
	var first *playerBusy
	for _, b := range playersBusy(r) {
		if !b.blocks(start, end) {
			continue
		}
		if first == nil || b.start.Before(first.start) || (b.start.Equal(first.start) && b.userID < first.userID) {
			first = &b
		}
	}
	return first
}

// courtFree reports whether the court has no taken match overlapping
// [start, start+Length).
func courtFree(r SlotRequest, courtID string, start time.Time) bool {
	for _, f := range r.Taken {
		if f.CourtID == courtID && start.Before(f.EndsAt) && start.Add(r.Length).After(f.StartsAt) {
			return false
		}
	}
	return true
}

// freeSlots is every start from the event's start, stepping by the slot
// length, at which a court is free for a whole slot inside the window, in
// time order and then courtOrder. Players aren't looked at.
func freeSlots(r SlotRequest) []SlotChoice {
	out := []SlotChoice{}
	if r.Length <= 0 {
		return out
	}
	courts := courtOrder(r.Courts)
	for t := r.StartsAt; !t.Add(r.Length).After(r.EndsAt); t = t.Add(r.Length) {
		for _, c := range courts {
			if courtFree(r, c.ID, t) {
				out = append(out, SlotChoice{CourtID: c.ID, StartsAt: t, EndsAt: t.Add(r.Length)})
			}
		}
	}
	return out
}

// matchFlags are the flags the admin sees on a match: a rating gap over
// ratingGapLimit between two sides, mixed's gender check, and no judge.
// sideRatings has one rating per side, or none when the sides aren't known.
func matchFlags(format string, sideRatings []int, hasJudge bool) []string {
	flags := []string{}
	if len(sideRatings) == 2 {
		if gap := sideRatings[0] - sideRatings[1]; gap > ratingGapLimit || -gap > ratingGapLimit {
			flags = append(flags, fmt.Sprintf("rating gap %d", max(gap, -gap)))
		}
	}
	if format == FormatMixed {
		flags = append(flags, FlagGenderUnchecked)
	}
	if !hasJudge {
		flags = append(flags, FlagNoJudge)
	}
	return flags
}

// playersPerSide is how many entries a side of the format has.
func playersPerSide(format string) int {
	if format == FormatSingles {
		return 1
	}
	return 2
}
