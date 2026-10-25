package main

import (
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	adminBodyLimit      = 16 << 10
	maxCourtLabelLength = 40
)

// requireAdmin returns the signed-in user if they may use admin mode, or
// writes an error. Every /api/admin route goes through it.
//
// For now admin mode is open to every signed-in user (a product decision for
// the early days). When roles exist, this is the one function that changes:
// check role_assignments here, on every request, so a revoked role takes
// effect at once, and return 403 for users without it.
func (a *API) requireAdmin(w http.ResponseWriter, r *http.Request) (User, bool) {
	return a.requireUser(w, r)
}

// adminRoute wraps a handler so it only runs for an admin.
func (a *API) adminRoute(h func(w http.ResponseWriter, r *http.Request, admin User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if admin, ok := a.requireAdmin(w, r); ok {
			h(w, r, admin)
		}
	}
}

// registerAdminRoutes adds /api/admin/*. Anything that changes data is a
// POST, PUT or DELETE: the session cookie is SameSite=Lax, which still rides
// along on cross-site GET navigations.
func (a *API) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/venues", a.adminRoute(a.handleAdminListVenues))
	mux.HandleFunc("POST /api/admin/venues", a.adminRoute(a.handleAdminCreateVenue))
	mux.HandleFunc("POST /api/admin/venues/{id}/courts", a.adminRoute(a.handleAdminAddCourt))

	mux.HandleFunc("GET /api/admin/events", a.adminRoute(a.handleAdminListEvents))
	mux.HandleFunc("POST /api/admin/events", a.adminRoute(a.handleAdminCreateEvent))
	mux.HandleFunc("GET /api/admin/events/{id}", a.adminRoute(a.handleAdminGetEvent))
	mux.HandleFunc("PUT /api/admin/events/{id}", a.adminRoute(a.handleAdminUpdateEvent))
	mux.HandleFunc("POST /api/admin/events/{id}/publish", a.adminRoute(a.handleAdminPublishEvent))
	mux.HandleFunc("POST /api/admin/events/{id}/cancel", a.adminRoute(a.handleAdminCancelEvent))
	mux.HandleFunc("POST /api/admin/events/{id}/judges", a.adminRoute(a.handleAdminAddJudge))
	mux.HandleFunc("DELETE /api/admin/events/{id}/judges/{userId}", a.adminRoute(a.handleAdminRemoveJudge))

	if a.judges != nil {
		mux.HandleFunc("GET /api/admin/judges", a.adminRoute(a.handleAdminListJudges))
		mux.HandleFunc("POST /api/admin/judges", a.adminRoute(a.handleAdminRegisterJudge))
		mux.HandleFunc("DELETE /api/admin/judges/{userId}", a.adminRoute(a.handleAdminDeleteJudge))
	}

	if a.matches != nil {
		mux.HandleFunc("GET /api/admin/events/{id}/entries", a.adminRoute(a.handleAdminListEventEntries))
		mux.HandleFunc("GET /api/admin/events/{id}/schedule", a.adminRoute(a.handleAdminGetSchedule))
		mux.HandleFunc("POST /api/admin/events/{id}/schedule/preview", a.adminRoute(a.handleAdminPreviewSchedule))
		mux.HandleFunc("POST /api/admin/events/{id}/schedule/publish", a.adminRoute(a.handleAdminPublishSchedule))
		mux.HandleFunc("POST /api/admin/entries/{id}/mark-paid", a.adminRoute(a.handleAdminMarkEntryPaid))

		// Fixture editing by hand (fixtures_handlers.go).
		mux.HandleFunc("POST /api/admin/events/{id}/matches", a.adminRoute(a.handleAdminCreateMatch))
		mux.HandleFunc("POST /api/admin/events/{id}/matches/suggest", a.adminRoute(a.handleAdminSuggestSlot))
		mux.HandleFunc("GET /api/admin/events/{id}/free-slots", a.adminRoute(a.handleAdminFreeSlots))
		mux.HandleFunc("GET /api/admin/events/{id}/changes", a.adminRoute(a.handleAdminListMatchChanges))
		mux.HandleFunc("POST /api/admin/matches/{id}/replace", a.adminRoute(a.handleAdminReplacePlayer))
		mux.HandleFunc("POST /api/admin/matches/{id}/move", a.adminRoute(a.handleAdminMoveMatch))
		mux.HandleFunc("POST /api/admin/matches/{id}/cancel", a.adminRoute(a.handleAdminCancelMatch))
	}
}

type venuesResponse struct {
	Venues []AdminVenue `json:"venues"`
}

type venueResponse struct {
	Venue AdminVenue `json:"venue"`
}

type courtResponse struct {
	Court AdminCourt `json:"court"`
}

type adminEventResponse struct {
	Event  Event   `json:"event"`
	Judges []Judge `json:"judges"`
}

type judgeResponse struct {
	Judge Judge `json:"judge"`
}

// writeAdminError maps store and validation errors to responses; anything
// unexpected is logged and becomes a 500.
func writeAdminError(w http.ResponseWriter, err error, action string) {
	var busy *CourtBusyError
	switch {
	case errors.As(err, &busy):
		writeError(w, http.StatusConflict, busy.Error())
	case errors.Is(err, errValidation):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), errValidation.Error()+": "))
	case errors.Is(err, ErrNoSuchEvent):
		writeError(w, http.StatusNotFound, "event not found")
	case errors.Is(err, ErrNoSuchVenue):
		writeError(w, http.StatusNotFound, "venue not found")
	case errors.Is(err, ErrNoSuchUser):
		writeError(w, http.StatusNotFound, "no user with that username")
	case errors.Is(err, ErrNotAJudge):
		writeError(w, http.StatusBadRequest, "that person isn't a registered judge. Add them under Judges first")
	case errors.Is(err, ErrCourtLabelTaken):
		writeError(w, http.StatusConflict, "this venue already has a court with that name")
	case errors.Is(err, ErrEventNotEditable):
		writeError(w, http.StatusConflict, "only draft events can be edited")
	default:
		log.Printf("admin %s: %v", action, err)
		writeError(w, http.StatusInternalServerError, "couldn't "+action)
	}
}

func (a *API) handleAdminListVenues(w http.ResponseWriter, r *http.Request, _ User) {
	venues, err := a.admin.ListVenues(r.Context())
	if err != nil {
		writeAdminError(w, err, "load venues")
		return
	}
	if venues == nil {
		venues = []AdminVenue{}
	}
	writeJSON(w, http.StatusOK, venuesResponse{Venues: venues})
}

func (a *API) handleAdminCreateVenue(w http.ResponseWriter, r *http.Request, _ User) {
	var req venueRequest
	if !decodeJSONBody(w, r, &req, adminBodyLimit) {
		return
	}
	in, err := validateVenue(req)
	if err != nil {
		writeAdminError(w, err, "create the venue")
		return
	}
	venue, err := a.admin.CreateVenue(r.Context(), in)
	if err != nil {
		writeAdminError(w, err, "create the venue")
		return
	}
	writeJSON(w, http.StatusCreated, venueResponse{Venue: venue})
}

func (a *API) handleAdminAddCourt(w http.ResponseWriter, r *http.Request, _ User) {
	var req struct {
		Label string `json:"label"`
	}
	if !decodeJSONBody(w, r, &req, adminBodyLimit) {
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" || utf8.RuneCountInString(label) > maxCourtLabelLength {
		writeError(w, http.StatusBadRequest, "label is required (at most 40 characters)")
		return
	}
	court, err := a.admin.AddCourt(r.Context(), r.PathValue("id"), label)
	if err != nil {
		writeAdminError(w, err, "add the court")
		return
	}
	writeJSON(w, http.StatusCreated, courtResponse{Court: court})
}

var eventStatuses = []string{EventDraft, EventPublished, EventCancelled, EventCompleted}

func (a *API) handleAdminListEvents(w http.ResponseWriter, r *http.Request, _ User) {
	status := r.URL.Query().Get("status")
	if status != "" && !slices.Contains(eventStatuses, status) {
		writeError(w, http.StatusBadRequest, "status must be draft, published, cancelled or completed")
		return
	}
	events, err := a.admin.ListAllEvents(r.Context(), status)
	if err != nil {
		writeAdminError(w, err, "load events")
		return
	}
	if events == nil {
		events = []Event{}
	}
	writeJSON(w, http.StatusOK, eventsResponse{Events: events})
}

// writeAdminEvent responds with the event (any status) and its judges.
func (a *API) writeAdminEvent(w http.ResponseWriter, r *http.Request, status int, id string) {
	event, err := a.events.GetEvent(r.Context(), id)
	if err != nil {
		writeAdminError(w, err, "load the event")
		return
	}
	judges, err := a.admin.ListEventJudges(r.Context(), id)
	if err != nil {
		writeAdminError(w, err, "load the event")
		return
	}
	writeJSON(w, status, adminEventResponse{Event: event, Judges: judges})
}

// readEventRequest decodes and validates a create or update body against
// its venue.
func (a *API) readEventRequest(w http.ResponseWriter, r *http.Request) (EventInput, bool) {
	var req eventRequest
	if !decodeJSONBody(w, r, &req, adminBodyLimit) {
		return EventInput{}, false
	}
	venue, err := a.admin.GetVenue(r.Context(), req.VenueID)
	if errors.Is(err, ErrNoSuchVenue) {
		writeError(w, http.StatusBadRequest, "venueId doesn't match a venue")
		return EventInput{}, false
	}
	if err != nil {
		writeAdminError(w, err, "save the event")
		return EventInput{}, false
	}
	in, err := validateEvent(req, venue, time.Now())
	if err != nil {
		writeAdminError(w, err, "save the event")
		return EventInput{}, false
	}
	return in, true
}

// handleAdminCreateEvent saves a new event as a draft; publishing is a
// separate step.
func (a *API) handleAdminCreateEvent(w http.ResponseWriter, r *http.Request, admin User) {
	in, ok := a.readEventRequest(w, r)
	if !ok {
		return
	}
	id, err := a.admin.CreateEvent(r.Context(), in, admin.ID)
	if err != nil {
		writeAdminError(w, err, "create the event")
		return
	}
	a.writeAdminEvent(w, r, http.StatusCreated, id)
}

func (a *API) handleAdminGetEvent(w http.ResponseWriter, r *http.Request, _ User) {
	a.writeAdminEvent(w, r, http.StatusOK, r.PathValue("id"))
}

// handleAdminUpdateEvent replaces a draft's details. Once published, players
// may have joined, so the event is no longer editable here.
func (a *API) handleAdminUpdateEvent(w http.ResponseWriter, r *http.Request, _ User) {
	id := r.PathValue("id")
	in, ok := a.readEventRequest(w, r)
	if !ok {
		return
	}
	if err := a.admin.UpdateEvent(r.Context(), id, in); err != nil {
		writeAdminError(w, err, "update the event")
		return
	}
	a.writeAdminEvent(w, r, http.StatusOK, id)
}

// handleAdminPublishEvent makes a draft visible to players.
func (a *API) handleAdminPublishEvent(w http.ResponseWriter, r *http.Request, _ User) {
	id := r.PathValue("id")
	event, err := a.events.GetEvent(r.Context(), id)
	if err != nil {
		writeAdminError(w, err, "publish the event")
		return
	}
	if !event.StartsAt.After(time.Now()) {
		writeError(w, http.StatusConflict, "this event has already started; edit its date first")
		return
	}
	a.changeStatus(w, r, id, []string{EventDraft}, EventPublished, "only a draft can be published")
}

// handleAdminCancelEvent cancels a draft or published event. Events are never
// deleted: registrations and payments will refer to them.
func (a *API) handleAdminCancelEvent(w http.ResponseWriter, r *http.Request, _ User) {
	a.changeStatus(w, r, r.PathValue("id"), []string{EventDraft, EventPublished}, EventCancelled,
		"only draft or published events can be cancelled")
}

func (a *API) changeStatus(w http.ResponseWriter, r *http.Request, id string, from []string, to, conflict string) {
	err := a.admin.SetEventStatus(r.Context(), id, from, to)
	if errors.Is(err, ErrStatusChange) {
		writeError(w, http.StatusConflict, conflict)
		return
	}
	if err != nil {
		writeAdminError(w, err, "change the event's status")
		return
	}
	a.writeAdminEvent(w, r, http.StatusOK, id)
}

func (a *API) handleAdminAddJudge(w http.ResponseWriter, r *http.Request, _ User) {
	var req struct {
		// UserID is a registered judge's, from GET /api/admin/judges.
		UserID string `json:"userId"`
	}
	if !decodeJSONBody(w, r, &req, adminBodyLimit) {
		return
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}
	judge, err := a.admin.AddEventJudge(r.Context(), r.PathValue("id"), userID)
	if err != nil {
		writeAdminError(w, err, "add the judge")
		return
	}
	writeJSON(w, http.StatusCreated, judgeResponse{Judge: judge})
}

func (a *API) handleAdminRemoveJudge(w http.ResponseWriter, r *http.Request, _ User) {
	if err := a.admin.RemoveEventJudge(r.Context(), r.PathValue("id"), r.PathValue("userId")); err != nil {
		writeAdminError(w, err, "remove the judge")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
