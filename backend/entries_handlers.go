package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type entryResponse struct {
	Entry Entry `json:"entry"`
}

type entriesResponse struct {
	Entries []Entry `json:"entries"`
}

// writeEntryError maps entry errors to responses, like writeAdminError. A
// rule that stops a valid request (full, closed, over a limit) is a 409.
func writeEntryError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, errValidation):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), errValidation.Error()+": "))
	case errors.Is(err, ErrNoSuchEvent):
		writeError(w, http.StatusNotFound, "event not found")
	case errors.Is(err, ErrNoSuchEntry):
		writeError(w, http.StatusNotFound, "entry not found")
	case errors.Is(err, ErrNoSuchUser):
		writeError(w, http.StatusNotFound, "no player with that username")
	case errors.Is(err, ErrEventNotOpen):
		writeError(w, http.StatusConflict, "this event isn't open for joining")
	case errors.Is(err, ErrRegistrationClosed):
		writeError(w, http.StatusConflict, "joining has closed for this event")
	case errors.Is(err, ErrOutsideBand):
		writeError(w, http.StatusConflict, "your rating is outside this event's rating band")
	case errors.Is(err, ErrEntryLimit):
		writeError(w, http.StatusConflict, fmt.Sprintf("you already have %d entries in this event, the most allowed", maxEntriesPerPlayer))
	case errors.Is(err, ErrEventFull):
		writeError(w, http.StatusConflict, "this event is full")
	case errors.Is(err, ErrNoMatchesLeft):
		writeError(w, http.StatusConflict, "this event has no matches left to give out")
	case errors.Is(err, ErrEntryNotCancellable):
		writeError(w, http.StatusConflict, "only an entry waiting for payment can be cancelled")
	default:
		log.Printf("entries %s: %v", action, err)
		writeError(w, http.StatusInternalServerError, "couldn't "+action)
	}
}

// handleCreateEntry joins an event with an open entry: one match, in one of
// the chosen formats. The entry holds its place for 10 minutes while the
// player pays.
//
//	POST /api/events/{id}/entries  {"formats":["singles","doubles"],"partnerUsername":"@asha"}
func (a *API) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	// Matches, judges and partners need to know who a player is.
	if !user.ProfileComplete {
		writeError(w, http.StatusForbidden, "finish setting up your profile before joining an event")
		return
	}
	var req entryRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	formats, partner, err := validateEntry(req)
	if err != nil {
		writeEntryError(w, err, "join the event")
		return
	}

	entry, err := a.entries.CreateEntry(r.Context(), EntryInput{
		EventID:         r.PathValue("id"),
		UserID:          user.ID,
		Formats:         formats,
		PartnerUsername: partner,
		Now:             time.Now(),
	})
	if err != nil {
		writeEntryError(w, err, "join the event")
		return
	}
	// The page pays with POST /api/me/entries/{id}/pay next; until then, or
	// if it never does, the hold simply runs out.
	writeJSON(w, http.StatusCreated, entryResponse{Entry: entry})
}

// handleListMyEntries lists the caller's entries, newest first, each with
// its event.
//
//	GET /api/me/entries
func (a *API) handleListMyEntries(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	entries, err := a.entries.ListUserEntries(r.Context(), user.ID)
	if err != nil {
		writeEntryError(w, err, "load your entries")
		return
	}
	writeJSON(w, http.StatusOK, entriesResponse{Entries: entries})
}

// handleCancelEntry cancels the caller's own unpaid entry, freeing its place
// at once. The entry is kept, as cancelled, and its open order is marked
// failed; a payment that still arrives for it is refunded.
//
//	DELETE /api/me/entries/{id}
func (a *API) handleCancelEntry(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	entry, err := a.entries.CancelEntry(r.Context(), user.ID, r.PathValue("id"))
	if err != nil {
		writeEntryError(w, err, "cancel the entry")
		return
	}
	writeJSON(w, http.StatusOK, entryResponse{Entry: entry})
}
