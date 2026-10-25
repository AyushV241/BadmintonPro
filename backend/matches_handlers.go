package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type scheduleResponse struct {
	Schedule ScheduleView `json:"schedule"`
}

type adminEntriesResponse struct {
	Entries []AdminEntry `json:"entries"`
}

type adminEntryResponse struct {
	Entry AdminEntry `json:"entry"`
}

type matchesResponse struct {
	Matches []PlayerMatch `json:"matches"`
}

// writeMatchError maps schedule, desk and match errors to responses, like
// writeEntryError but worded for the admin.
func writeMatchError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, errValidation):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), errValidation.Error()+": "))
	case errors.Is(err, errConflict):
		writeError(w, http.StatusConflict, strings.TrimPrefix(err.Error(), errConflict.Error()+": "))
	case errors.Is(err, ErrNoSuchEvent):
		writeError(w, http.StatusNotFound, "event not found")
	case errors.Is(err, ErrNoSuchEntry):
		writeError(w, http.StatusNotFound, "entry not found")
	case errors.Is(err, ErrNoSuchMatch):
		writeError(w, http.StatusNotFound, "match not found")
	case errors.Is(err, ErrEventNotPublished):
		writeError(w, http.StatusConflict, "only a published event's schedule can be published")
	case errors.Is(err, ErrScheduleNotPublished):
		writeError(w, http.StatusNotFound, "this event's schedule isn't published yet")
	case errors.Is(err, ErrSchedulePublished):
		writeError(w, http.StatusConflict, "this event's schedule is already published")
	case errors.Is(err, ErrNothingToSchedule):
		writeError(w, http.StatusConflict, "there's nothing to schedule: the confirmed entries don't make a single match")
	case errors.Is(err, ErrScheduleCourtClashes):
		writeError(w, http.StatusConflict, ErrScheduleCourtClashes.Error())
	case errors.Is(err, ErrEntryInChallenge):
		writeError(w, http.StatusConflict, "this entry is part of a challenge; the player pays for it in the app")
	case errors.Is(err, ErrEntryNotPending):
		writeError(w, http.StatusConflict, "only an entry waiting for payment can be marked paid")
	case errors.Is(err, ErrEventNotOpen):
		writeError(w, http.StatusConflict, "this event isn't open: it's a draft, cancelled or completed")
	case errors.Is(err, ErrEventOver):
		writeError(w, http.StatusConflict, "this event has ended")
	case errors.Is(err, ErrEntryLimit):
		writeError(w, http.StatusConflict, fmt.Sprintf("the player already has %d entries in this event, the most allowed", maxEntriesPerPlayer))
	case errors.Is(err, ErrEventFull):
		writeError(w, http.StatusConflict, "this event is full")
	case errors.Is(err, ErrNoMatchesLeft):
		writeError(w, http.StatusConflict, "this event has no matches left to give out")
	default:
		log.Printf("matches %s: %v", action, err)
		writeError(w, http.StatusInternalServerError, "couldn't "+action)
	}
}

// handleAdminPreviewSchedule runs the matcher on the event's confirmed
// entries and returns the schedule it would publish. Nothing is saved, so
// it can be run as often as the admin likes. It's a POST because it is an
// action, and so it never rides along on a cross-site GET.
//
//	POST /api/admin/events/{id}/schedule/preview
func (a *API) handleAdminPreviewSchedule(w http.ResponseWriter, r *http.Request, _ User) {
	view, err := a.matches.PreviewSchedule(r.Context(), r.PathValue("id"), time.Now())
	if err != nil {
		writeMatchError(w, err, "preview the schedule")
		return
	}
	writeJSON(w, http.StatusOK, scheduleResponse{Schedule: view})
}

// handleAdminPublishSchedule runs the matcher again on the current entries
// and saves the result: the matches, and every confirmed entry moved to
// scheduled or standby. Version 1 publishes once per event.
//
// TODO(step 11, notifications): tell each player their opponent, court,
// time and judge (section 4.3). Until then they see it in GET
// /api/me/matches.
//
//	POST /api/admin/events/{id}/schedule/publish
func (a *API) handleAdminPublishSchedule(w http.ResponseWriter, r *http.Request, admin User) {
	view, err := a.matches.PublishSchedule(r.Context(), r.PathValue("id"), admin.ID, time.Now())
	if err != nil {
		writeMatchError(w, err, "publish the schedule")
		return
	}
	writeJSON(w, http.StatusCreated, scheduleResponse{Schedule: view})
}

// handleAdminGetSchedule returns the saved schedule: the event's matches
// that aren't cancelled (published ones and any made by hand), with each
// player's current rating. Before a publish publishedAt is null; 404 while
// nothing is published and no match has been made by hand.
//
//	GET /api/admin/events/{id}/schedule
func (a *API) handleAdminGetSchedule(w http.ResponseWriter, r *http.Request, _ User) {
	view, err := a.matches.PublishedSchedule(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMatchError(w, err, "load the schedule")
		return
	}
	writeJSON(w, http.StatusOK, scheduleResponse{Schedule: view})
}

// handleAdminListEventEntries lists an event's entries in every status, in
// the order they joined, with each player's ratings.
//
//	GET /api/admin/events/{id}/entries
func (a *API) handleAdminListEventEntries(w http.ResponseWriter, r *http.Request, _ User) {
	entries, err := a.matches.ListEventEntries(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMatchError(w, err, "load the entries")
		return
	}
	writeJSON(w, http.StatusOK, adminEntriesResponse{Entries: entries})
}

// handleAdminMarkEntryPaid confirms an unpaid entry that the player paid for
// at the venue, in cash or by UPI (section 7, desk-assisted walk-ins).
//
// The payment is recorded as an offline payments row, noting the admin.
// Online payments confirm entries through /api/payments instead.
//
//	POST /api/admin/entries/{id}/mark-paid
func (a *API) handleAdminMarkEntryPaid(w http.ResponseWriter, r *http.Request, admin User) {
	entry, err := a.matches.MarkEntryPaid(r.Context(), r.PathValue("id"), admin.ID, time.Now())
	if err != nil {
		writeMatchError(w, err, "mark the entry paid")
		return
	}
	writeJSON(w, http.StatusOK, adminEntryResponse{Entry: entry})
}

// handleListMyMatches lists the caller's matches, soonest first, with both
// sides, the court and the judge.
//
//	GET /api/me/matches
func (a *API) handleListMyMatches(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	matches, err := a.matches.ListUserMatches(r.Context(), user.ID)
	if err != nil {
		writeMatchError(w, err, "load your matches")
		return
	}
	writeJSON(w, http.StatusOK, matchesResponse{Matches: matches})
}

// handleMyStats returns the caller's ratings and finished-match counts, for
// their profile.
//
//	GET /api/me/stats
func (a *API) handleMyStats(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	stats, err := a.matches.PlayerStats(r.Context(), user.ID)
	if err != nil {
		writeMatchError(w, err, "load your stats")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
