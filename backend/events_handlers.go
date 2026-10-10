package main

import (
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEventsLimit = 20
	maxEventsLimit     = 50
)

var citySlug = regexp.MustCompile(`^[a-z0-9-]+$`)

type eventsResponse struct {
	Events []Event `json:"events"`
	// NextCursor fetches the following page; null on the last one.
	NextCursor *string `json:"nextCursor"`
}

type eventResponse struct {
	Event Event `json:"event"`
}

// encodeCursor makes the opaque ?cursor= value for the event after which the
// next page starts. Clients pass it back unchanged.
func encodeCursor(e Event) string {
	raw := e.StartsAt.UTC().Format(time.RFC3339Nano) + "|" + e.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (*EventCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	start, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return nil, errors.New("malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return nil, err
	}
	return &EventCursor{StartsAt: t, ID: id}, nil
}

// handleListEvents is public: the list carries no information about who has
// joined, so it can back a signed-out page too.
//
//	GET /api/events?city=bangalore&limit=20&cursor=…
func (a *API) handleListEvents(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	q := EventQuery{Now: time.Now(), Limit: defaultEventsLimit}

	if city := strings.ToLower(strings.TrimSpace(params.Get("city"))); city != "" {
		if !citySlug.MatchString(city) {
			writeError(w, http.StatusBadRequest, "city must be a slug like \"bangalore\"")
			return
		}
		q.City = city
	}
	if s := params.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxEventsLimit {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and "+strconv.Itoa(maxEventsLimit))
			return
		}
		q.Limit = n
	}
	if s := params.Get("cursor"); s != "" {
		cursor, err := decodeCursor(s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		q.After = cursor
	}

	events, more, err := a.events.ListEvents(r.Context(), q)
	if err != nil {
		log.Printf("list events: %v", err)
		writeError(w, http.StatusInternalServerError, "couldn't load events")
		return
	}

	resp := eventsResponse{Events: events}
	if resp.Events == nil {
		resp.Events = []Event{} // [] in JSON, not null
	}
	if more {
		next := encodeCursor(events[len(events)-1])
		resp.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetEvent is public like the list. Drafts are hidden; cancelled and
// completed events stay visible so links to them keep working.
//
//	GET /api/events/{id}
func (a *API) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	event, err := a.events.GetEvent(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNoSuchEvent) || (err == nil && event.Status == EventDraft) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		log.Printf("get event: %v", err)
		writeError(w, http.StatusInternalServerError, "couldn't load the event")
		return
	}
	writeJSON(w, http.StatusOK, eventResponse{Event: event})
}
