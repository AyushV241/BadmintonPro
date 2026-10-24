package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

// --- validation (no database) ---

var testVenue = AdminVenue{
	Venue: Venue{ID: "ven_t", Timezone: demoTimezone},
	Courts: []AdminCourt{
		{ID: "crt_1", Label: "Court 1", Active: true},
		{ID: "crt_2", Label: "Court 2", Active: true},
		{ID: "crt_off", Label: "Court 3", Active: false},
	},
}

func intp(v int) *int { return &v }

// validEventRequest is a singles evening on testVenue's two active courts,
// a week after testNow.
func validEventRequest() eventRequest {
	return eventRequest{
		VenueID:    "ven_t",
		Title:      "  Friday Smash  ",
		Formats:    []string{FormatSingles},
		Date:       "2026-10-16",
		StartTime:  "19:00",
		EndTime:    "21:00",
		FeePaise:   5000,
		Capacity:   16,
		MaxMatches: 8,
		CourtIDs:   []string{"crt_1", "crt_2"},
	}
}

func TestValidateEvent(t *testing.T) {
	in, err := validateEvent(validEventRequest(), testVenue, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "Friday Smash" || in.VenueID != "ven_t" {
		t.Errorf("title/venue = %q %q", in.Title, in.VenueID)
	}
	// 19:00 in Bangalore is 13:30 UTC; joining closes 2 hours before.
	if want := time.Date(2026, 10, 16, 13, 30, 0, 0, time.UTC); !in.StartsAt.Equal(want) || in.StartsAt.Location() != time.UTC {
		t.Errorf("startsAt = %v, want %v", in.StartsAt, want)
	}
	if want := in.StartsAt.Add(-2 * time.Hour); !in.RegistrationClosesAt.Equal(want) {
		t.Errorf("registrationClosesAt = %v", in.RegistrationClosesAt)
	}
	if in.SlotMinutes[FormatSingles] != 30 || len(in.SlotMinutes) != 1 {
		t.Errorf("slots = %v", in.SlotMinutes)
	}

	// Formats come out in a fixed order without repeats; slot defaults apply.
	req := validEventRequest()
	req.Formats = []string{FormatMixed, FormatSingles, FormatMixed}
	req.SlotMinutes = map[string]int{FormatMixed: 45}
	in, err = validateEvent(req, testVenue, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(in.Formats, ",") != "singles,mixed" || in.SlotMinutes[FormatSingles] != 30 || in.SlotMinutes[FormatMixed] != 45 {
		t.Errorf("formats %v, slots %v", in.Formats, in.SlotMinutes)
	}

	// Repeated courts count once.
	req = validEventRequest()
	req.CourtIDs = []string{"crt_1", "crt_1"}
	req.MaxMatches = 4 // 1 court × 120 min ÷ 30
	if in, err = validateEvent(req, testVenue, testNow); err != nil || len(in.CourtIDs) != 1 {
		t.Errorf("courts = %v, %v", in.CourtIDs, err)
	}
}

func TestValidateEventRejects(t *testing.T) {
	for name, mutate := range map[string]func(*eventRequest){
		"blank title":             func(r *eventRequest) { r.Title = "   " },
		"no formats":              func(r *eventRequest) { r.Formats = nil },
		"unknown format":          func(r *eventRequest) { r.Formats = []string{"tennis"} },
		"slot for unoffered":      func(r *eventRequest) { r.SlotMinutes = map[string]int{FormatDoubles: 40} },
		"slot too short":          func(r *eventRequest) { r.SlotMinutes = map[string]int{FormatSingles: 5} },
		"bad date":                func(r *eventRequest) { r.Date = "16/10/2026" },
		"bad time":                func(r *eventRequest) { r.StartTime = "7pm" },
		"ends before start":       func(r *eventRequest) { r.EndTime = "18:00" },
		"in the past":             func(r *eventRequest) { r.Date = "2026-10-01" },
		"close too early":         func(r *eventRequest) { r.RegistrationClosesMinutesBefore = intp(maxCloseMinutes + 1) },
		"close negative":          func(r *eventRequest) { r.RegistrationClosesMinutesBefore = intp(-1) },
		"no courts":               func(r *eventRequest) { r.CourtIDs = nil },
		"inactive court":          func(r *eventRequest) { r.CourtIDs = []string{"crt_off"} },
		"court elsewhere":         func(r *eventRequest) { r.CourtIDs = []string{"crt_other"} },
		"more matches than fit":   func(r *eventRequest) { r.MaxMatches = 9 }, // 2 courts × 120 ÷ 30 = 8
		"no matches":              func(r *eventRequest) { r.MaxMatches = 0 },
		"negative fee":            func(r *eventRequest) { r.FeePaise = -1 },
		"tiny capacity":           func(r *eventRequest) { r.Capacity = 1 },
		"rating out of range":     func(r *eventRequest) { r.RatingMin = intp(50) },
		"rating band upside down": func(r *eventRequest) { r.RatingMin, r.RatingMax = intp(1600), intp(1400) },
	} {
		req := validEventRequest()
		mutate(&req)
		if _, err := validateEvent(req, testVenue, testNow); !errors.Is(err, errValidation) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
}

func TestValidateVenue(t *testing.T) {
	lat, lng := 12.97, 77.59
	in, err := validateVenue(venueRequest{Name: " Arena ", City: "Bangalore", Lat: &lat, Lng: &lng, Courts: 3})
	if err != nil {
		t.Fatal(err)
	}
	if in.Name != "Arena" || in.City != "bangalore" || in.Timezone != defaultVenueTimezone || in.Courts != 3 {
		t.Errorf("venue = %+v", in)
	}

	bad := 200.0
	for name, req := range map[string]venueRequest{
		"no name":         {City: "bangalore"},
		"bad city":        {Name: "A", City: "new york"},
		"lat only":        {Name: "A", City: "x", Lat: &lat},
		"lat range":       {Name: "A", City: "x", Lat: &bad, Lng: &lng},
		"bad timezone":    {Name: "A", City: "x", Timezone: "Mars/Olympus"},
		"too many courts": {Name: "A", City: "x", Courts: maxCourtsPerVenue + 1},
	} {
		if _, err := validateVenue(req); !errors.Is(err, errValidation) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestMaxMatchesFor(t *testing.T) {
	start := time.Date(2026, 10, 16, 13, 30, 0, 0, time.UTC)
	// The shortest slot decides: 3 courts × 120 min ÷ 30 min.
	if n := maxMatchesFor(3, start, start.Add(2*time.Hour), map[string]int{FormatSingles: 30, FormatDoubles: 40}); n != 12 {
		t.Errorf("got %d, want 12", n)
	}
	if n := maxMatchesFor(2, start, start.Add(100*time.Minute), map[string]int{FormatDoubles: 40}); n != 4 {
		t.Errorf("got %d, want 4 (2 courts × 2 whole slots)", n)
	}
}

// --- admin endpoints against Postgres ---

type adminClient struct {
	t      *testing.T
	srv    *httptest.Server
	cookie *http.Cookie
}

// do sends a JSON request and decodes a 2xx response into out.
func (c adminClient) do(method, path string, body any, out any) int {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if s, ok := body.(string); ok {
			buf.WriteString(s)
		} else if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, c.srv.URL+path, &buf)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if out != nil && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			c.t.Fatal(err)
		}
	}
	return res.StatusCode
}

func TestAdminFlow(t *testing.T) {
	store := eventsTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `TRUNCATE users, sessions, user_identities, event_judges CASCADE`); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewAPI(store, oauth.NewRegistry(), nil, false).WithEvents(store).WithAdmin(store).Routes())
	t.Cleanup(srv.Close)

	adminCookie, admin := signIn(t, store, "+919800000001")
	_, judgeUser := signIn(t, store, "+919800000002")
	if _, err := store.UpdateProfile(ctx, judgeUser.ID, ProfileUpdate{Name: "Priya Nair", Username: "priya"}); err != nil {
		t.Fatal(err)
	}
	api := adminClient{t: t, srv: srv, cookie: adminCookie}
	anon := adminClient{t: t, srv: srv}

	// Every admin route needs a session.
	for _, route := range [][2]string{
		{"GET", "/api/admin/venues"}, {"POST", "/api/admin/venues"}, {"GET", "/api/admin/events"},
		{"POST", "/api/admin/events"}, {"POST", "/api/admin/events/x/publish"}, {"DELETE", "/api/admin/events/x/judges/y"},
	} {
		if code := anon.do(route[0], route[1], nil, nil); code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want 401", route[0], route[1], code)
		}
	}

	// Venue with three courts, then a fourth.
	var venue venueResponse
	if code := api.do("POST", "/api/admin/venues", map[string]any{
		"name": "Whitefield Arena", "city": "Bangalore", "lat": 12.9698, "lng": 77.7500, "courts": 3,
	}, &venue); code != http.StatusCreated {
		t.Fatalf("create venue: %d", code)
	}
	v := venue.Venue
	if v.City != "bangalore" || len(v.Courts) != 3 || v.Courts[0].Label != "Court 1" {
		t.Fatalf("venue = %+v", v)
	}
	if code := api.do("POST", "/api/admin/venues/"+v.ID+"/courts", map[string]string{"label": "Court 4"}, nil); code != http.StatusCreated {
		t.Errorf("add court: %d", code)
	}
	if code := api.do("POST", "/api/admin/venues/"+v.ID+"/courts", map[string]string{"label": "Court 4"}, nil); code != http.StatusConflict {
		t.Errorf("duplicate court: %d", code)
	}
	if code := api.do("POST", "/api/admin/venues/ven_nope/courts", map[string]string{"label": "Court 1"}, nil); code != http.StatusNotFound {
		t.Errorf("court at unknown venue: %d", code)
	}
	// A request can't carry fields we don't know, such as a role.
	if code := api.do("POST", "/api/admin/venues", `{"name":"X","city":"x","role":"admin"}`, nil); code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", code)
	}

	// Draft event a week from now on two courts.
	date := time.Now().In(mustLoad(demoTimezone)).AddDate(0, 0, 7).Format("2006-01-02")
	eventBody := map[string]any{
		"venueId": v.ID, "title": "Whitefield Wednesday", "formats": []string{"singles", "doubles"},
		"date": date, "startTime": "19:00", "endTime": "21:00",
		"feePaise": 5000, "capacity": 16, "maxMatches": 6, "ratingMin": 1200,
		"courtIds": []string{v.Courts[0].ID, v.Courts[1].ID},
	}
	var created adminEventResponse
	if code := api.do("POST", "/api/admin/events", eventBody, &created); code != http.StatusCreated {
		t.Fatalf("create event: %d", code)
	}
	ev := created.Event
	if ev.Status != EventDraft || len(ev.Courts) != 2 || ev.SlotMinutes[FormatDoubles] != 40 || len(created.Judges) != 0 {
		t.Fatalf("created = %+v", created)
	}
	var createdBy string
	if err := store.pool.QueryRow(ctx, `SELECT created_by FROM events WHERE id = $1`, ev.ID).Scan(&createdBy); err != nil || createdBy != admin.ID {
		t.Errorf("created_by = %q, %v", createdBy, err)
	}

	// Drafts are invisible to players.
	if code := anon.do("GET", "/api/events/"+ev.ID, nil, nil); code != http.StatusNotFound {
		t.Errorf("public draft: %d", code)
	}

	// Validation errors come back as 400 with a message.
	bad := map[string]any{}
	for k, val := range eventBody {
		bad[k] = val
	}
	bad["maxMatches"] = 99
	if code := api.do("POST", "/api/admin/events", bad, nil); code != http.StatusBadRequest {
		t.Errorf("too many matches: %d", code)
	}
	bad["maxMatches"], bad["venueId"] = 6, "ven_nope"
	if code := api.do("POST", "/api/admin/events", bad, nil); code != http.StatusBadRequest {
		t.Errorf("unknown venue: %d", code)
	}

	// Edit the draft: new title, three courts.
	eventBody["title"] = "Whitefield Wednesday Night"
	eventBody["courtIds"] = []string{v.Courts[0].ID, v.Courts[1].ID, v.Courts[2].ID}
	var updated adminEventResponse
	if code := api.do("PUT", "/api/admin/events/"+ev.ID, eventBody, &updated); code != http.StatusOK {
		t.Fatalf("update: %d", code)
	}
	if updated.Event.Title != "Whitefield Wednesday Night" || len(updated.Event.Courts) != 3 {
		t.Errorf("updated = %+v", updated.Event)
	}

	// Judges come from the registered judge list, by account ID. Someone
	// who isn't registered is refused, even with an account.
	addJudge := func(userID string) (judgeResponse, int) {
		var judge judgeResponse
		return judge, api.do("POST", "/api/admin/events/"+ev.ID+"/judges", map[string]string{"userId": userID}, &judge)
	}
	if _, code := addJudge(judgeUser.ID); code != http.StatusBadRequest {
		t.Errorf("unregistered judge: %d, want 400", code)
	}
	if _, err := store.AddJudge(ctx, judgeUser.ID, "Priya Nair", "+919800000002", admin.ID); err != nil {
		t.Fatal(err)
	}
	judge, code := addJudge(judgeUser.ID)
	if code != http.StatusCreated {
		t.Fatalf("add judge: %d", code)
	}
	if judge.Judge.UserID != judgeUser.ID || judge.Judge.Name != "Priya Nair" {
		t.Errorf("judge = %+v", judge.Judge)
	}
	if _, code := addJudge(judgeUser.ID); code != http.StatusCreated {
		t.Errorf("adding again: %d", code)
	}
	if _, code := addJudge("usr_nobody"); code != http.StatusBadRequest {
		t.Errorf("unknown judge: %d", code)
	}
	var got adminEventResponse
	api.do("GET", "/api/admin/events/"+ev.ID, nil, &got)
	if len(got.Judges) != 1 {
		t.Errorf("judges = %+v", got.Judges)
	}

	// Publish: players see it; it can't be edited or published again.
	var published adminEventResponse
	if code := api.do("POST", "/api/admin/events/"+ev.ID+"/publish", nil, &published); code != http.StatusOK || published.Event.Status != EventPublished {
		t.Fatalf("publish: %d %s", code, published.Event.Status)
	}
	if code := anon.do("GET", "/api/events/"+ev.ID, nil, nil); code != http.StatusOK {
		t.Errorf("public after publish: %d", code)
	}
	if code := api.do("PUT", "/api/admin/events/"+ev.ID, eventBody, nil); code != http.StatusConflict {
		t.Errorf("edit published: %d", code)
	}
	if code := api.do("POST", "/api/admin/events/"+ev.ID+"/publish", nil, nil); code != http.StatusConflict {
		t.Errorf("publish twice: %d", code)
	}

	// Remove the judge; then cancel, once.
	if code := api.do("DELETE", "/api/admin/events/"+ev.ID+"/judges/"+judgeUser.ID, nil, nil); code != http.StatusNoContent {
		t.Errorf("remove judge: %d", code)
	}
	var cancelled adminEventResponse
	if code := api.do("POST", "/api/admin/events/"+ev.ID+"/cancel", nil, &cancelled); code != http.StatusOK || cancelled.Event.Status != EventCancelled || len(cancelled.Judges) != 0 {
		t.Errorf("cancel: %d %+v", code, cancelled)
	}
	if code := api.do("POST", "/api/admin/events/"+ev.ID+"/cancel", nil, nil); code != http.StatusConflict {
		t.Errorf("cancel twice: %d", code)
	}
	if code := api.do("POST", "/api/admin/events/evt_nope/cancel", nil, nil); code != http.StatusNotFound {
		t.Errorf("cancel unknown: %d", code)
	}

	// The admin list has every status and filters by one.
	var all, drafts eventsResponse
	api.do("GET", "/api/admin/events", nil, &all)
	api.do("GET", "/api/admin/events?status=draft", nil, &drafts)
	if len(all.Events) != 1 || len(drafts.Events) != 0 {
		t.Errorf("admin list: %d all, %d drafts", len(all.Events), len(drafts.Events))
	}
	if code := api.do("GET", "/api/admin/events?status=bogus", nil, nil); code != http.StatusBadRequest {
		t.Errorf("bad status filter: %d", code)
	}

	var venues venuesResponse
	api.do("GET", "/api/admin/venues", nil, &venues)
	if len(venues.Venues) != 1 || len(venues.Venues[0].Courts) != 4 {
		t.Errorf("venues = %+v", venues.Venues)
	}
}

// Two events that aren't cancelled can't use one court at overlapping times
// (design doc, section 10).
func TestCourtDoubleBooking(t *testing.T) {
	store := eventsTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `TRUNCATE users, sessions, user_identities, event_judges CASCADE`); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewAPI(store, oauth.NewRegistry(), nil, false).WithEvents(store).WithAdmin(store).Routes())
	t.Cleanup(srv.Close)
	cookie, _ := signIn(t, store, "+919800000001")
	api := adminClient{t: t, srv: srv, cookie: cookie}

	venue, err := store.CreateVenue(ctx, VenueInput{Name: "Arena", City: "bangalore", Timezone: demoTimezone, Courts: 3})
	if err != nil {
		t.Fatal(err)
	}
	c1, c2, c3 := venue.Courts[0].ID, venue.Courts[1].ID, venue.Courts[2].ID
	date := time.Now().In(mustLoad(demoTimezone)).AddDate(0, 0, 7).Format("2006-01-02")
	body := func(title, start, end string, courts ...string) map[string]any {
		return map[string]any{
			"venueId": venue.ID, "title": title, "formats": []string{"singles"},
			"date": date, "startTime": start, "endTime": end,
			"feePaise": 5000, "capacity": 8, "maxMatches": 1, "courtIds": courts,
		}
	}
	// save creates (id "") or updates an event and returns the status code,
	// the error message on a 409, and the event's ID.
	save := func(id string, b map[string]any) (int, string, string) {
		t.Helper()
		method, path := "POST", "/api/admin/events"
		if id != "" {
			method, path = "PUT", "/api/admin/events/"+id
		}
		var buf bytes.Buffer
		_ = json.NewEncoder(&buf).Encode(b)
		req, _ := http.NewRequest(method, srv.URL+path, &buf)
		req.AddCookie(cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		var out struct {
			adminEventResponse
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out.Error, out.Event.ID
	}

	code, _, a := save("", body("Evening A", "19:00", "21:00", c1, c2))
	if code != http.StatusCreated {
		t.Fatalf("A: %d", code)
	}

	// Overlapping on court 2: refused, naming the court and the other event.
	code, msg, _ := save("", body("Clash", "20:00", "22:00", c2, c3))
	if code != http.StatusConflict || !strings.Contains(msg, "Court 2") || !strings.Contains(msg, `"Evening A"`) {
		t.Errorf("overlap on create: %d %q", code, msg)
	}

	// Touching end to end is fine, before and after; so is another court.
	if code, msg, _ := save("", body("Late", "21:00", "23:00", c2)); code != http.StatusCreated {
		t.Errorf("starting as A ends: %d %q", code, msg)
	}
	if code, msg, _ := save("", body("Early", "18:00", "19:00", c1)); code != http.StatusCreated {
		t.Errorf("ending as A starts: %d %q", code, msg)
	}
	code, _, e := save("", body("Side court", "19:30", "20:30", c3))
	if code != http.StatusCreated {
		t.Fatalf("other court: %d", code)
	}

	// Updating onto a busy court is refused; an event never clashes with
	// itself.
	if code, msg, _ := save(e, body("Side court", "19:30", "20:30", c1)); code != http.StatusConflict || !strings.Contains(msg, "Court 1") {
		t.Errorf("overlap on update: %d %q", code, msg)
	}
	if code, msg, _ := save(e, body("Side court, renamed", "19:30", "20:30", c3)); code != http.StatusOK {
		t.Errorf("updating in place: %d %q", code, msg)
	}

	// A cancelled event frees its courts.
	if code := api.do("POST", "/api/admin/events/"+a+"/cancel", nil, nil); code != http.StatusOK {
		t.Fatalf("cancel A: %d", code)
	}
	if code, msg, _ := save("", body("Evening B", "19:00", "21:00", c1, c2)); code != http.StatusCreated {
		t.Errorf("after A was cancelled: %d %q", code, msg)
	}

	// Publishing checks again: a clash written around the API (here, plain
	// SQL) can't go live.
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO events (id, venue_id, title, formats, starts_at, ends_at, registration_closes_at, fee_paise, capacity, max_matches)
		SELECT 'evt_t_sneaky', venue_id, 'Sneaky', formats, starts_at + interval '30 minutes', ends_at, registration_closes_at, 5000, 8, 1
		FROM events WHERE id = $1`, e); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO event_courts (event_id, court_id, venue_id) VALUES ('evt_t_sneaky', $1, $2)`, c3, venue.ID); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/admin/events/evt_t_sneaky/publish", nil)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out errorResponse
	_ = json.NewDecoder(res.Body).Decode(&out)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusConflict || !strings.Contains(out.Error, "Court 3") || !strings.Contains(out.Error, "Side court, renamed") {
		t.Errorf("publish over a clash: %d %q", res.StatusCode, out.Error)
	}
	var status string
	if err := store.pool.QueryRow(ctx, `SELECT status FROM events WHERE id = 'evt_t_sneaky'`).Scan(&status); err != nil || status != EventDraft {
		t.Errorf("status after refused publish = %q, %v", status, err)
	}
}
