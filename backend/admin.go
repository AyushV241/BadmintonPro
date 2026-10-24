package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Admin mode: creating venues, courts and events, and managing an event's
// judges. See docs/design/events-and-matches.md, section 10.

var (
	ErrNoSuchVenue      = errors.New("no such venue")
	ErrCourtLabelTaken  = errors.New("court label taken")
	ErrEventNotEditable = errors.New("only draft events can be edited")
	ErrStatusChange     = errors.New("event can't move to that status")
	// ErrCourtBusy is wrapped by CourtBusyError, which names the clash.
	ErrCourtBusy  = errors.New("court already booked")
	errValidation = errors.New("invalid")
)

// CourtBusyError says which court another event already uses at an
// overlapping time (section 10: no double-booking our own courts).
type CourtBusyError struct {
	Court, Event string
}

func (e *CourtBusyError) Error() string {
	return fmt.Sprintf("%s is already booked for %q at an overlapping time", e.Court, e.Event)
}

func (e *CourtBusyError) Is(target error) bool { return target == ErrCourtBusy }

const (
	defaultCloseMinutes  = 120
	maxCloseMinutes      = 7 * 24 * 60
	maxEventHours        = 16
	maxFeePaise          = 1_000_000 // ₹10,000
	minCapacity          = 2
	maxCapacity          = 1000
	minSlotMinutes       = 10
	maxSlotMinutes       = 120
	minElo, maxElo       = 100, 4000
	maxCourtsPerVenue    = 50
	maxTitleLength       = 120
	maxVenueFieldLength  = 200
	defaultVenueTimezone = "Asia/Kolkata"
)

var (
	// formatOrder is the order formats are stored and shown in.
	formatOrder        = []string{FormatSingles, FormatDoubles, FormatMixed}
	defaultSlotMinutes = map[string]int{FormatSingles: 30, FormatDoubles: 40, FormatMixed: 40}
)

// validationError is a 400 with a message for the admin.
func validationError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errValidation, fmt.Sprintf(format, args...))
}

// normaliseFormats puts formats in formatOrder without repeats. It is shared
// by events (what they offer) and entries (what a player will play).
func normaliseFormats(formats []string) ([]string, error) {
	for _, f := range formats {
		if !slices.Contains(formatOrder, f) {
			return nil, validationError("unknown format %q (want singles, doubles or mixed)", f)
		}
	}
	var out []string
	for _, f := range formatOrder {
		if slices.Contains(formats, f) {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil, validationError("pick at least one format")
	}
	return out, nil
}

// AdminCourt is a court as admins see it, including inactive ones.
type AdminCourt struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Active bool   `json:"active"`
}

// AdminVenue is a venue with all its courts.
type AdminVenue struct {
	Venue
	Courts []AdminCourt `json:"courts"`
}

// Judge is a user assigned to judge an event's matches.
type Judge struct {
	UserID   string `json:"userId"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

// VenueInput is a validated new venue.
type VenueInput struct {
	Name, Address, City string
	Lat, Lng            *float64
	Timezone            string
	// Courts creates "Court 1" … "Court n".
	Courts int
}

// EventInput is a validated event, with times already resolved in the
// venue's time zone.
type EventInput struct {
	VenueID              string
	Title                string
	Formats              []string
	SlotMinutes          map[string]int
	StartsAt, EndsAt     time.Time
	RegistrationClosesAt time.Time
	FeePaise             int
	Capacity, MaxMatches int
	RatingMin, RatingMax *int
	CourtIDs             []string
}

// AdminStore is what admin mode needs. Only PostgresStore implements it.
type AdminStore interface {
	ListVenues(ctx context.Context) ([]AdminVenue, error)
	// GetVenue returns ErrNoSuchVenue for an unknown ID.
	GetVenue(ctx context.Context, id string) (AdminVenue, error)
	CreateVenue(ctx context.Context, in VenueInput) (AdminVenue, error)
	// AddCourt returns ErrNoSuchVenue or ErrCourtLabelTaken.
	AddCourt(ctx context.Context, venueID, label string) (AdminCourt, error)

	// ListAllEvents returns events in every status, latest start first.
	// status filters when non-empty.
	ListAllEvents(ctx context.Context, status string) ([]Event, error)
	// CreateEvent saves a draft and returns its ID.
	CreateEvent(ctx context.Context, in EventInput, createdBy string) (string, error)
	// UpdateEvent replaces a draft's details. It returns ErrNoSuchEvent or
	// ErrEventNotEditable.
	UpdateEvent(ctx context.Context, id string, in EventInput) error
	// SetEventStatus moves an event to `to` if its status is one of `from`.
	// It returns ErrNoSuchEvent or ErrStatusChange.
	//
	// CreateEvent, UpdateEvent and publishing through SetEventStatus also
	// return a *CourtBusyError (errors.Is ErrCourtBusy) when another event
	// that isn't cancelled uses one of the courts at an overlapping time.
	SetEventStatus(ctx context.Context, id string, from []string, to string) error

	ListEventJudges(ctx context.Context, eventID string) ([]Judge, error)
	// AddEventJudge adds a registered judge (the admin's judge list) to the
	// event. It returns ErrNoSuchEvent or ErrNotAJudge; adding someone twice
	// is fine.
	AddEventJudge(ctx context.Context, eventID, userID string) (Judge, error)
	RemoveEventJudge(ctx context.Context, eventID, userID string) error
}

// venueRequest is the body of POST /api/admin/venues.
type venueRequest struct {
	Name     string   `json:"name"`
	Address  string   `json:"address"`
	City     string   `json:"city"`
	Lat      *float64 `json:"lat"`
	Lng      *float64 `json:"lng"`
	Timezone string   `json:"timezone"`
	Courts   int      `json:"courts"`
}

func validateVenue(req venueRequest) (VenueInput, error) {
	in := VenueInput{
		Name:     strings.TrimSpace(req.Name),
		Address:  strings.TrimSpace(req.Address),
		City:     strings.ToLower(strings.TrimSpace(req.City)),
		Lat:      req.Lat,
		Lng:      req.Lng,
		Timezone: strings.TrimSpace(req.Timezone),
		Courts:   req.Courts,
	}
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > maxVenueFieldLength:
		return in, validationError("name is required (at most %d characters)", maxVenueFieldLength)
	case utf8.RuneCountInString(in.Address) > maxVenueFieldLength:
		return in, validationError("address is too long")
	case !citySlug.MatchString(in.City):
		return in, validationError("city must be a slug like \"bangalore\"")
	case (in.Lat == nil) != (in.Lng == nil):
		return in, validationError("give both lat and lng, or neither")
	case in.Lat != nil && (*in.Lat < -90 || *in.Lat > 90 || *in.Lng < -180 || *in.Lng > 180):
		return in, validationError("lat or lng is out of range")
	case in.Courts < 0 || in.Courts > maxCourtsPerVenue:
		return in, validationError("courts must be 0–%d", maxCourtsPerVenue)
	}
	if in.Timezone == "" {
		in.Timezone = defaultVenueTimezone
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil || in.Timezone == "Local" {
		return in, validationError("timezone must be an IANA zone like \"Asia/Kolkata\"")
	}
	return in, nil
}

// eventRequest is the body of POST and PUT /api/admin/events. Times are the
// venue's local wall-clock times on one day: the server applies the venue's
// time zone, so the browser never converts zones.
type eventRequest struct {
	VenueID     string         `json:"venueId"`
	Title       string         `json:"title"`
	Formats     []string       `json:"formats"`
	SlotMinutes map[string]int `json:"slotMinutes"`
	// Date is "2026-10-17"; StartTime and EndTime are "19:30".
	Date      string `json:"date"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	// RegistrationClosesMinutesBefore defaults to 120.
	RegistrationClosesMinutesBefore *int     `json:"registrationClosesMinutesBefore"`
	FeePaise                        int      `json:"feePaise"`
	Capacity                        int      `json:"capacity"`
	MaxMatches                      int      `json:"maxMatches"`
	RatingMin                       *int     `json:"ratingMin"`
	RatingMax                       *int     `json:"ratingMax"`
	CourtIDs                        []string `json:"courtIds"`
}

// maxMatchesFor is how many matches fit: every court, for the whole event,
// in slots of the shortest format offered.
func maxMatchesFor(courts int, start, end time.Time, slotMinutes map[string]int) int {
	shortest := 0
	for _, m := range slotMinutes {
		if shortest == 0 || m < shortest {
			shortest = m
		}
	}
	if shortest == 0 {
		return 0
	}
	// Whole slots per court: a match can't be split across courts.
	return courts * (int(end.Sub(start).Minutes()) / shortest)
}

// validateEvent checks an event request against its venue and turns it into
// an EventInput. now is when the request arrived.
func validateEvent(req eventRequest, venue AdminVenue, now time.Time) (EventInput, error) {
	in := EventInput{VenueID: venue.ID, Title: strings.TrimSpace(req.Title)}
	if in.Title == "" || utf8.RuneCountInString(in.Title) > maxTitleLength {
		return in, validationError("title is required (at most %d characters)", maxTitleLength)
	}

	var err error
	if in.Formats, err = normaliseFormats(req.Formats); err != nil {
		return in, err
	}
	for f := range req.SlotMinutes {
		if !slices.Contains(in.Formats, f) {
			return in, validationError("slotMinutes has %q, which the event doesn't offer", f)
		}
	}
	in.SlotMinutes = make(map[string]int, len(in.Formats))
	for _, f := range in.Formats {
		m, ok := req.SlotMinutes[f]
		if !ok {
			m = defaultSlotMinutes[f]
		}
		if m < minSlotMinutes || m > maxSlotMinutes {
			return in, validationError("%s slots must be %d–%d minutes", f, minSlotMinutes, maxSlotMinutes)
		}
		in.SlotMinutes[f] = m
	}

	// Times on one day, in the venue's zone.
	loc, err := time.LoadLocation(venue.Timezone)
	if err != nil {
		return in, fmt.Errorf("venue time zone %q: %w", venue.Timezone, err)
	}
	in.StartsAt, err = time.ParseInLocation("2006-01-02 15:04", req.Date+" "+req.StartTime, loc)
	if err != nil {
		return in, validationError("date must be YYYY-MM-DD and startTime HH:MM")
	}
	in.EndsAt, err = time.ParseInLocation("2006-01-02 15:04", req.Date+" "+req.EndTime, loc)
	if err != nil {
		return in, validationError("endTime must be HH:MM")
	}
	switch {
	case !in.EndsAt.After(in.StartsAt):
		return in, validationError("the event must end after it starts, on the same day")
	case in.EndsAt.Sub(in.StartsAt) > time.Duration(maxEventHours)*time.Hour:
		return in, validationError("an event can last at most %d hours", maxEventHours)
	case !in.StartsAt.After(now):
		return in, validationError("the event must start in the future")
	}
	closeMinutes := defaultCloseMinutes
	if req.RegistrationClosesMinutesBefore != nil {
		closeMinutes = *req.RegistrationClosesMinutesBefore
	}
	if closeMinutes < 0 || closeMinutes > maxCloseMinutes {
		return in, validationError("registration must close 0–%d minutes before the start", maxCloseMinutes)
	}
	in.RegistrationClosesAt = in.StartsAt.Add(-time.Duration(closeMinutes) * time.Minute)
	in.StartsAt, in.EndsAt, in.RegistrationClosesAt = in.StartsAt.UTC(), in.EndsAt.UTC(), in.RegistrationClosesAt.UTC()

	// Courts: at least one, all active courts at this venue.
	active := map[string]bool{}
	for _, c := range venue.Courts {
		active[c.ID] = c.Active
	}
	for _, id := range req.CourtIDs {
		if slices.Contains(in.CourtIDs, id) {
			continue
		}
		if !active[id] {
			return in, validationError("court %q isn't an active court at this venue", id)
		}
		in.CourtIDs = append(in.CourtIDs, id)
	}
	if len(in.CourtIDs) == 0 {
		return in, validationError("pick at least one court")
	}

	in.FeePaise, in.Capacity, in.MaxMatches = req.FeePaise, req.Capacity, req.MaxMatches
	limit := maxMatchesFor(len(in.CourtIDs), in.StartsAt, in.EndsAt, in.SlotMinutes)
	switch {
	case in.FeePaise < 0 || in.FeePaise > maxFeePaise:
		return in, validationError("feePaise must be 0–%d", maxFeePaise)
	case in.Capacity < minCapacity || in.Capacity > maxCapacity:
		return in, validationError("capacity must be %d–%d players", minCapacity, maxCapacity)
	case in.MaxMatches < 1 || in.MaxMatches > limit:
		return in, validationError("maxMatches must be 1–%d: that's all the slots on these courts", limit)
	}

	in.RatingMin, in.RatingMax = req.RatingMin, req.RatingMax
	for _, r := range []*int{in.RatingMin, in.RatingMax} {
		if r != nil && (*r < minElo || *r > maxElo) {
			return in, validationError("ratings must be %d–%d", minElo, maxElo)
		}
	}
	if in.RatingMin != nil && in.RatingMax != nil && *in.RatingMin > *in.RatingMax {
		return in, validationError("ratingMin can't be above ratingMax")
	}
	return in, nil
}
