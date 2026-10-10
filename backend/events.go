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

// Event is a session an admin runs on pre-booked courts.
type Event struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Format   string    `json:"format"`
	Status   string    `json:"status"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	// FeePaise is the entry fee in the currency's minor unit.
	FeePaise int    `json:"feePaise"`
	Currency string `json:"currency"`
	Capacity int    `json:"capacity"`
	// SpotsLeft is the capacity until joining exists.
	SpotsLeft int `json:"spotsLeft"`
	// RatingMin and RatingMax are nil when the event takes any rating.
	RatingMin *float64 `json:"ratingMin"`
	RatingMax *float64 `json:"ratingMax"`
	Venue     Venue    `json:"venue"`
	// Courts is filled in by GetEvent only; lists leave it out.
	Courts []Court `json:"courts,omitempty"`
}

// EventQuery selects the public list of events: published, not yet over,
// optionally in one city, in start order.
type EventQuery struct {
	City string
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
