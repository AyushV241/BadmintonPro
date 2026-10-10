package main

import (
	"context"
	"errors"
	"time"
)

var ErrNoSuchEvent = errors.New("no such event")

// Event formats and statuses, as stored.
const (
	FormatSingles = "singles"
	FormatDoubles = "doubles"
	FormatMixed   = "mixed"

	EventDraft     = "draft"
	EventPublished = "published"
	EventCancelled = "cancelled"
	EventCompleted = "completed"
)

// Venue is where events happen.
type Venue struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	City    string `json:"city"`
	// Lat and Lng are nil until the venue has been placed on a map.
	Lat *float64 `json:"lat"`
	Lng *float64 `json:"lng"`
	// Timezone is an IANA zone; clients show event times in it.
	Timezone string `json:"timezone"`
}

// Court is one of a venue's courts.
type Court struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Event is a session an admin runs on pre-booked courts. Players pay its
// slot fee to join the pool, and the matcher turns the pool into matches.
type Event struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Formats are what players can sign up for: singles, doubles, mixed.
	Formats []string `json:"formats"`
	// SlotMinutes is how long one match occupies a court, for each format
	// the event offers.
	SlotMinutes map[string]int `json:"slotMinutes"`
	Status      string         `json:"status"`
	StartsAt    time.Time      `json:"startsAt"`
	EndsAt      time.Time      `json:"endsAt"`
	// RegistrationClosesAt is when online joining stops so the matcher can
	// build the schedule.
	RegistrationClosesAt time.Time `json:"registrationClosesAt"`
	// FeePaise is the slot fee in the currency's minor unit.
	FeePaise int    `json:"feePaise"`
	Currency string `json:"currency"`
	// Capacity is the most players the event takes; MaxMatches the most
	// matches its courts can host.
	Capacity   int `json:"capacity"`
	MaxMatches int `json:"maxMatches"`
	// SpotsLeft is the capacity until joining exists.
	SpotsLeft int `json:"spotsLeft"`
	// RatingMin and RatingMax are the Elo band; nil when there is no limit.
	RatingMin *int `json:"ratingMin"`
	RatingMax *int `json:"ratingMax"`
	// DistanceKm is from the searched location; only set on a location search.
	DistanceKm *float64 `json:"distanceKm,omitempty"`
	Venue      Venue    `json:"venue"`
	// Courts is filled in by GetEvent only; lists leave it out.
	Courts []Court `json:"courts,omitempty"`
}

// GeoPoint is a location in degrees.
type GeoPoint struct {
	Lat, Lng float64
}

// EventQuery selects the public list of events: published, not yet over,
// optionally in one city or within a distance of a point, in start order.
type EventQuery struct {
	City string
	// Near, when set, keeps venues within RadiusKm of it (venues without
	// coordinates drop out) and fills in each event's DistanceKm.
	Near     *GeoPoint
	RadiusKm float64
	// Now is "the current time" for the not-yet-over check; a parameter so
	// tests control it.
	Now   time.Time
	Limit int
	// After continues a previous page: events strictly after this one in
	// (starts_at, id) order. Nil for the first page.
	After *EventCursor
}

// EventCursor is the position of the last event on a page.
type EventCursor struct {
	StartsAt time.Time
	ID       string
}

// EventStore reads events. Only PostgresStore implements it: events are
// queried in SQL and tested against a real database, with no in-memory copy.
type EventStore interface {
	// ListEvents returns up to q.Limit events and whether more follow.
	ListEvents(ctx context.Context, q EventQuery) (events []Event, more bool, err error)

	// GetEvent returns an event in any status, with its courts. It returns
	// ErrNoSuchEvent for an unknown ID.
	GetEvent(ctx context.Context, id string) (Event, error)
}
