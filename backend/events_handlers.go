package main

import (
	"encoding/base64"
	"errors"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEventsLimit = 20
	maxEventsLimit     = 50
	defaultRadiusKm    = 10
	maxRadiusKm        = 100
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

// parseNear reads ?lat=&lng=&radiusKm=. Both coordinates or neither; the
// radius only applies with them. It returns nil when there is no location.
func parseNear(params url.Values) (*GeoPoint, float64, error) {
	latS, lngS, radiusS := params.Get("lat"), params.Get("lng"), params.Get("radiusKm")
	if latS == "" && lngS == "" {
		if radiusS != "" {
			return nil, 0, errors.New("radiusKm needs lat and lng")
		}
		return nil, 0, nil
	}
	lat, errLat := strconv.ParseFloat(latS, 64)
	lng, errLng := strconv.ParseFloat(lngS, 64)
	// NaN slips past range comparisons, so rule it out by name.
	if errLat != nil || errLng != nil || math.IsNaN(lat) || math.IsNaN(lng) ||
		lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return nil, 0, errors.New("lat and lng must both be given, in degrees")
	}
	radius := float64(defaultRadiusKm)
	if radiusS != "" {
		r, err := strconv.ParseFloat(radiusS, 64)
		if err != nil || math.IsNaN(r) || r <= 0 || r > maxRadiusKm {
			return nil, 0, errors.New("radiusKm must be more than 0 and at most " + strconv.Itoa(maxRadiusKm))
		}
		radius = r
	}
	return &GeoPoint{Lat: lat, Lng: lng}, radius, nil
}

// handleListEvents is public: the list carries no information about who has
// joined, so it can back a signed-out page too.
//
//	GET /api/events?city=bangalore&limit=20&cursor=…
//	GET /api/events?lat=12.93&lng=77.62&radiusKm=5   (each event gets distanceKm)
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
	near, radius, err := parseNear(params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	q.Near, q.RadiusKm = near, radius

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
