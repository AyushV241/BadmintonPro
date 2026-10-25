package main

import (
	"net/http"
	"strings"
	"time"
)

// Fixture editing: /api/admin/events/{id}/matches… and /api/admin/matches/…
// (fixtures.go). Registered with the schedule routes in registerAdminRoutes.

type matchResponse struct {
	Match ScheduleMatch `json:"match"`
}

type slotResponse struct {
	Slot SlotSuggestion `json:"slot"`
}

type freeSlotsResponse struct {
	Slots []FreeSlot `json:"slots"`
}

type matchChangesResponse struct {
	Changes []MatchChange `json:"changes"`
}

// createMatchRequest is the body of POST /api/admin/events/{id}/matches.
type createMatchRequest struct {
	Format string   `json:"format"`
	SideA  []string `json:"sideA"`
	SideB  []string `json:"sideB"`
	// CourtID and StartsAt (RFC 3339) are optional: left out, the earliest
	// slot that keeps every rule is picked.
	CourtID  string `json:"courtId"`
	StartsAt string `json:"startsAt"`
}

// suggestRequest is the body of POST /api/admin/events/{id}/matches/suggest.
type suggestRequest struct {
	Format   string   `json:"format"`
	EntryIDs []string `json:"entryIds"`
	CourtID  string   `json:"courtId"`
	StartsAt string   `json:"startsAt"`
}

type replaceRequest struct {
	OutEntryID string `json:"outEntryId"`
	InEntryID  string `json:"inEntryId"`
	// Outcome is "no_show" or "back_to_pool".
	Outcome string `json:"outcome"`
}

type moveRequest struct {
	CourtID  string `json:"courtId"`
	StartsAt string `json:"startsAt"`
}

// parseStart reads an optional RFC 3339 start: nil when s is empty.
func parseStart(s string) (*time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, validationError("startsAt must be a date and time like 2026-10-17T19:30:00+05:30")
	}
	return &t, nil
}

// handleAdminCreateMatch makes a match by hand from waiting paid entries
// (section 10, editing fixtures), before or after the schedule is published.
//
//	POST /api/admin/events/{id}/matches
func (a *API) handleAdminCreateMatch(w http.ResponseWriter, r *http.Request, admin User) {
	var req createMatchRequest
	if !decodeJSONBody(w, r, &req, adminBodyLimit) {
		return
	}
	start, err := parseStart(req.StartsAt)
	if err != nil {
		writeMatchError(w, err, "create the match")
		return
	}
	match, err := a.matches.CreateMatch(r.Context(), ManualMatchInput{
		EventID: r.PathValue("id"), Format: req.Format, Sides: [][]string{req.SideA, req.SideB},
		CourtID: strings.TrimSpace(req.CourtID), StartsAt: start, AdminID: admin.ID,
	})
	if err != nil {
		writeMatchError(w, err, "create the match")
		return
	}
	writeJSON(w, http.StatusCreated, matchResponse{Match: match})
}

// handleAdminSuggestSlot answers where a match made by hand would go, with
// its judge, without saving: the same checks and choice as creating it.
// It's a POST so the entry list fits in a body.
//
//	POST /api/admin/events/{id}/matches/suggest
func (a *API) handleAdminSuggestSlot(w http.ResponseWriter, r *http.Request, _ User) {
	var req suggestRequest
	if !decodeJSONBody(w, r, &req, adminBodyLimit) {
		return
	}
	start, err := parseStart(req.StartsAt)
	if err != nil {
		writeMatchError(w, err, "suggest a slot")
		return
	}
	slot, err := a.matches.SuggestSlot(r.Context(), ManualMatchInput{
		EventID: r.PathValue("id"), Format: req.Format, Sides: [][]string{req.EntryIDs},
		CourtID: strings.TrimSpace(req.CourtID), StartsAt: start,
	})
	if err != nil {
		writeMatchError(w, err, "suggest a slot")
		return
	}
	writeJSON(w, http.StatusOK, slotResponse{Slot: slot})
}

// handleAdminFreeSlots lists the court-free starts for the time picker.
//
//	GET /api/admin/events/{id}/free-slots?format=singles&matchId=
func (a *API) handleAdminFreeSlots(w http.ResponseWriter, r *http.Request, _ User) {
	q := r.URL.Query()
	if q.Get("format") == "" {
		writeError(w, http.StatusBadRequest, "format is required (singles, doubles or mixed)")
		return
	}
	slots, err := a.matches.FreeSlots(r.Context(), r.PathValue("id"), q.Get("format"), q.Get("matchId"))
	if err != nil {
		writeMatchError(w, err, "load the free slots")
		return
	}
	writeJSON(w, http.StatusOK, freeSlotsResponse{Slots: slots})
}

// handleAdminReplacePlayer swaps a player of a scheduled match for a
// waiting entry; the one taken out is a no-show or goes back to the pool.
//
//	POST /api/admin/matches/{id}/replace
func (a *API) handleAdminReplacePlayer(w http.ResponseWriter, r *http.Request, admin User) {
	var req replaceRequest
	if !decodeJSONBody(w, r, &req, adminBodyLimit) {
		return
	}
	switch {
	case req.OutEntryID == "" || req.InEntryID == "":
		writeError(w, http.StatusBadRequest, "outEntryId and inEntryId are required")
		return
	case req.Outcome != OutcomeNoShow && req.Outcome != OutcomeBackToPool:
		writeError(w, http.StatusBadRequest, `outcome must be "no_show" or "back_to_pool"`)
		return
	}
	match, err := a.matches.ReplacePlayer(r.Context(), ReplaceInput{
		MatchID: r.PathValue("id"), AdminID: admin.ID,
		OutEntryID: req.OutEntryID, InEntryID: req.InEntryID, Outcome: req.Outcome,
	})
	if err != nil {
		writeMatchError(w, err, "replace the player")
		return
	}
	writeJSON(w, http.StatusOK, matchResponse{Match: match})
}

// handleAdminMoveMatch gives a scheduled match a new court and start.
//
//	POST /api/admin/matches/{id}/move
func (a *API) handleAdminMoveMatch(w http.ResponseWriter, r *http.Request, admin User) {
	var req moveRequest
	if !decodeJSONBody(w, r, &req, adminBodyLimit) {
		return
	}
	start, err := parseStart(req.StartsAt)
	if err == nil && (start == nil || strings.TrimSpace(req.CourtID) == "") {
		err = validationError("courtId and startsAt are required")
	}
	if err != nil {
		writeMatchError(w, err, "move the match")
		return
	}
	match, err := a.matches.MoveMatch(r.Context(), MoveInput{
		MatchID: r.PathValue("id"), AdminID: admin.ID, CourtID: strings.TrimSpace(req.CourtID), StartsAt: *start,
	})
	if err != nil {
		writeMatchError(w, err, "move the match")
		return
	}
	writeJSON(w, http.StatusOK, matchResponse{Match: match})
}

// handleAdminCancelMatch cancels a scheduled match; its entries go back to
// the pool. Matches are never deleted (rule 5).
//
//	POST /api/admin/matches/{id}/cancel
func (a *API) handleAdminCancelMatch(w http.ResponseWriter, r *http.Request, admin User) {
	match, err := a.matches.CancelMatch(r.Context(), r.PathValue("id"), admin.ID)
	if err != nil {
		writeMatchError(w, err, "cancel the match")
		return
	}
	writeJSON(w, http.StatusOK, matchResponse{Match: match})
}

// handleAdminListMatchChanges lists the event's fixture edits, newest first.
//
//	GET /api/admin/events/{id}/changes
func (a *API) handleAdminListMatchChanges(w http.ResponseWriter, r *http.Request, _ User) {
	changes, err := a.matches.ListMatchChanges(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMatchError(w, err, "load the changes")
		return
	}
	writeJSON(w, http.StatusOK, matchChangesResponse{Changes: changes})
}
