package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fixtureEvent is an event as fixture editing sees it.
type fixtureEvent struct {
	id, status string
	loc        *time.Location
	formats    []string
	// slot is each offered format's slot length.
	slot             map[string]time.Duration
	startsAt, endsAt time.Time
	maxMatches       int
	courts           []Court
	judgeIDs         []string
}

// loadFixtureEvent reads the event with its courts and judges. With lock it
// locks the event row, the first lock every fixture action takes, as
// CreateEntry, MarkEntryPaid and PublishSchedule do.
func loadFixtureEvent(ctx context.Context, q rowsQuerier, eventID string, lock bool) (fixtureEvent, error) {
	ev := fixtureEvent{id: eventID, slot: map[string]time.Duration{}}
	forUpdate := ""
	if lock {
		forUpdate = " FOR UPDATE OF e"
	}
	var timezone string
	var singles, doubles, mixed int
	err := q.QueryRow(ctx, `
		SELECT e.status, v.timezone, e.formats, e.singles_slot_minutes, e.doubles_slot_minutes,
			e.mixed_slot_minutes, e.starts_at, e.ends_at, e.max_matches
		FROM events e JOIN venues v ON v.id = e.venue_id
		WHERE e.id = $1`+forUpdate, eventID,
	).Scan(&ev.status, &timezone, &ev.formats, &singles, &doubles, &mixed, &ev.startsAt, &ev.endsAt, &ev.maxMatches)
	if errors.Is(err, pgx.ErrNoRows) {
		return ev, ErrNoSuchEvent
	}
	if err != nil {
		return ev, fmt.Errorf("load event: %w", err)
	}
	ev.startsAt, ev.endsAt = ev.startsAt.UTC(), ev.endsAt.UTC()
	if ev.loc, err = time.LoadLocation(timezone); err != nil {
		ev.loc = time.UTC
	}
	minutes := map[string]int{FormatSingles: singles, FormatDoubles: doubles, FormatMixed: mixed}
	for _, f := range ev.formats {
		if minutes[f] > 0 {
			ev.slot[f] = time.Duration(minutes[f]) * time.Minute
		}
	}

	rows, err := q.Query(ctx, `
		SELECT c.id, c.label FROM event_courts ec JOIN courts c ON c.id = ec.court_id
		WHERE ec.event_id = $1 ORDER BY length(c.label), c.label`, eventID)
	if err != nil {
		return ev, fmt.Errorf("event courts: %w", err)
	}
	if ev.courts, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Court]); err != nil {
		return ev, fmt.Errorf("event courts: %w", err)
	}
	rows, err = q.Query(ctx, `SELECT user_id FROM event_judges WHERE event_id = $1 ORDER BY user_id`, eventID)
	if err != nil {
		return ev, fmt.Errorf("event judges: %w", err)
	}
	if ev.judgeIDs, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return ev, fmt.Errorf("event judges: %w", err)
	}
	return ev, nil
}

// clock is t as the admin reads it, in the venue's time zone: "8:30 PM".
func (ev fixtureEvent) clock(t time.Time) string {
	return t.In(ev.loc).Format("3:04 PM")
}

func (ev fixtureEvent) court(id string) (Court, bool) {
	i := slices.IndexFunc(ev.courts, func(c Court) bool { return c.ID == id })
	if i < 0 {
		return Court{}, false
	}
	return ev.courts[i], true
}

// checkFormat checks the event offers the format, returning its slot length.
func (ev fixtureEvent) checkFormat(format string) (time.Duration, error) {
	if !slices.Contains(formatOrder, format) {
		return 0, validationError("format must be singles, doubles or mixed")
	}
	length, ok := ev.slot[format]
	if !ok {
		return 0, validationError("this event doesn't offer %s", format)
	}
	return length, nil
}

// checkStart checks a slot of length starting at t lies inside the event's
// window.
func (ev fixtureEvent) checkStart(t time.Time, length time.Duration) error {
	latest := ev.endsAt.Add(-length)
	if t.Before(ev.startsAt) || t.After(latest) {
		return validationError("pick a start between %s and %s", ev.clock(ev.startsAt), ev.clock(latest))
	}
	return nil
}

// slotRequest is the event's side of a SlotRequest: its window, courts and
// judges, and the matches already taking time (taken), for these players.
func (ev fixtureEvent) slotRequest(length time.Duration, players []string, taken []FixedMatch) SlotRequest {
	return SlotRequest{
		StartsAt: ev.startsAt, EndsAt: ev.endsAt, Length: length, Preferred: ev.startsAt,
		Courts: ev.courts, JudgeIDs: ev.judgeIDs, PlayerIDs: players, Taken: taken,
	}
}

// loadTaken returns the matches a slot for these players must work around:
// the event's own that aren't cancelled and other events' on its courts
// (loadFixedMatches, as the matcher sees them), plus the players' matches in
// other events near the event's time, which they must keep the gap from too.
// The match exceptID (moved or changed) is left out.
func loadTaken(ctx context.Context, q rowsQuerier, ev fixtureEvent, players []string, exceptID string) ([]FixedMatch, error) {
	fixed, err := loadFixedMatches(ctx, q, ev.id, ev.startsAt, ev.endsAt)
	if err != nil {
		return nil, err
	}
	// The longest slot is two hours, so anything ending more than that
	// before the event (or starting that long after) can't be in the way.
	rows, err := q.Query(ctx, `
		SELECT m.id, m.court_id, m.starts_at, m.ends_at, COALESCE(m.judge_id, ''), true,
			COALESCE(array_agg(ms.user_id ORDER BY ms.user_id) FILTER (WHERE ms.user_id IS NOT NULL), '{}')
		FROM matches m
		LEFT JOIN match_sides ms ON ms.match_id = m.id
		WHERE m.status <> 'cancelled' AND m.event_id IS DISTINCT FROM $1
		  AND m.starts_at < $3 AND m.ends_at > $2
		  AND (m.judge_id = ANY($4)
		       OR EXISTS (SELECT 1 FROM match_sides p WHERE p.match_id = m.id AND p.user_id = ANY($4)))
		GROUP BY m.id
		ORDER BY m.starts_at, m.id`,
		ev.id, ev.startsAt.Add(-maxSlotMinutes*time.Minute), ev.endsAt.Add(maxSlotMinutes*time.Minute), players)
	if err != nil {
		return nil, fmt.Errorf("load players' matches: %w", err)
	}
	more, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FixedMatch, error) {
		var f FixedMatch
		err := row.Scan(&f.ID, &f.CourtID, &f.StartsAt, &f.EndsAt, &f.JudgeID, &f.OtherEvent, &f.UserIDs)
		f.StartsAt, f.EndsAt = f.StartsAt.UTC(), f.EndsAt.UTC()
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("load players' matches: %w", err)
	}
	seen := map[string]bool{}
	var taken []FixedMatch
	for _, f := range append(fixed, more...) {
		if f.ID == exceptID || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		taken = append(taken, f)
	}
	return taken, nil
}

// fixtureEntry is an entry with its player, read with its row locked.
type fixtureEntry struct {
	id, eventID, userID, kind, status string
	matchID                           *string
	formats                           []string
	// name is how messages name the player.
	name string
}

// lockEntries locks the entries in ID order and returns those found.
func lockEntries(ctx context.Context, q rowsQuerier, ids []string) (map[string]fixtureEntry, error) {
	rows, err := q.Query(ctx, `
		SELECT en.id, en.event_id, en.user_id, en.kind, en.status, en.match_id, en.formats,
			COALESCE(NULLIF(u.name, ''), u.username, 'A player')
		FROM entries en JOIN users u ON u.id = en.user_id
		WHERE en.id = ANY($1)
		ORDER BY en.id
		FOR UPDATE OF en`, ids)
	if err != nil {
		return nil, fmt.Errorf("lock entries: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (fixtureEntry, error) {
		var e fixtureEntry
		err := row.Scan(&e.id, &e.eventID, &e.userID, &e.kind, &e.status, &e.matchID, &e.formats, &e.name)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("lock entries: %w", err)
	}
	out := make(map[string]fixtureEntry, len(list))
	for _, e := range list {
		out[e.id] = e
	}
	return out, nil
}

// checkWaiting checks the entry can go into a match of the format: an open
// entry that's paid and waiting (confirmed or standby, in no match) and
// allows the format.
func checkWaiting(e fixtureEntry, format string) error {
	switch {
	case e.kind == EntryLocked:
		return validationError("%s's entry belongs to a challenge, so it can't go in another match", e.name)
	case (e.status != EntryConfirmed && e.status != EntryStandby) || e.matchID != nil:
		return validationError("%s's entry isn't waiting for a match (it's %s)", e.name, strings.ReplaceAll(e.status, "_", " "))
	case !slices.Contains(e.formats, format):
		return validationError("%s's entry doesn't allow %s", e.name, format)
	}
	return nil
}

// userNames returns how messages name these users.
func userNames(ctx context.Context, q rowsQuerier, ids []string) (map[string]string, error) {
	rows, err := q.Query(ctx, `
		SELECT id, COALESCE(NULLIF(name, ''), username, 'A player') FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("load names: %w", err)
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("load names: %w", err)
		}
		names[id] = name
	}
	return names, rows.Err()
}

// playerError is the message for a player who can't play a slot.
func (ev fixtureEvent) playerError(b *playerBusy, names map[string]string) error {
	if b.judging {
		return conflictError("%s is judging a match at %s", names[b.userID], ev.clock(b.start))
	}
	return conflictError("%s already plays at %s", names[b.userID], ev.clock(b.start))
}

// checkSlot checks a given court and start against the court's other
// matches and the players' (r's Taken and PlayerIDs).
func (ev fixtureEvent) checkSlot(r SlotRequest, court Court, start time.Time, names map[string]string) error {
	if !courtFree(r, court.ID, start) {
		return conflictError("%s is taken at %s", court.Label, ev.clock(start))
	}
	if b := playerConflict(r, start); b != nil {
		return ev.playerError(b, names)
	}
	return nil
}

// chooseSlot checks or picks the slot for a new match: the court and start
// given, the first free court at the start given, the earliest start on the
// court given, or the earliest slot of all (first court on a tie). The
// judge is pickJudge's.
func (ev fixtureEvent) chooseSlot(r SlotRequest, courtID string, start *time.Time, names map[string]string) (SlotChoice, error) {
	var court Court
	if courtID != "" {
		var ok bool
		if court, ok = ev.court(courtID); !ok {
			return SlotChoice{}, validationError("that court isn't one of this event's courts")
		}
	}
	if start != nil {
		if err := ev.checkStart(*start, r.Length); err != nil {
			return SlotChoice{}, err
		}
	}
	if len(ev.courts) == 0 {
		return SlotChoice{}, conflictError("this event has no courts")
	}

	var choice SlotChoice
	switch {
	case start != nil && courtID != "":
		if err := ev.checkSlot(r, court, *start, names); err != nil {
			return SlotChoice{}, err
		}
		choice = SlotChoice{CourtID: court.ID, StartsAt: *start, EndsAt: start.Add(r.Length)}
	case start != nil:
		if b := playerConflict(r, *start); b != nil {
			return SlotChoice{}, ev.playerError(b, names)
		}
		for _, c := range courtOrder(ev.courts) {
			if courtFree(r, c.ID, *start) {
				choice = SlotChoice{CourtID: c.ID, StartsAt: *start, EndsAt: start.Add(r.Length)}
				break
			}
		}
		if choice.CourtID == "" {
			return SlotChoice{}, conflictError("every court is taken at %s", ev.clock(*start))
		}
	default:
		if courtID != "" {
			r.Courts = []Court{court}
		}
		var ok bool
		if choice, ok = findSlot(r); !ok {
			if courtID != "" {
				return SlotChoice{}, conflictError("%s has no free slot these players can take in the event's time", court.Label)
			}
			return SlotChoice{}, conflictError("no court has a free slot these players can take in the event's time")
		}
	}
	choice.JudgeID = pickJudge(r.JudgeIDs, r.Taken, r.PlayerIDs, choice.StartsAt, choice.EndsAt)
	return choice, nil
}

// manualPlan is a new match checked and placed, not yet saved.
type manualPlan struct {
	ev     fixtureEvent
	sides  [][]fixtureEntry
	choice SlotChoice
}

// planManualMatch checks a match made by hand (CreateMatch) or a suggestion
// (SuggestSlot), with the event row and the entries locked, and places it.
func planManualMatch(ctx context.Context, tx pgx.Tx, in ManualMatchInput) (manualPlan, error) {
	ev, err := loadFixtureEvent(ctx, tx, in.EventID, true)
	if err != nil {
		return manualPlan{}, err
	}
	if ev.status != EventPublished {
		return manualPlan{}, ErrEventNotOpen
	}
	length, err := ev.checkFormat(in.Format)
	if err != nil {
		return manualPlan{}, err
	}
	perSide := playersPerSide(in.Format)
	var ids []string
	for _, side := range in.Sides {
		ids = append(ids, side...)
	}
	if len(in.Sides) == 2 {
		if len(in.Sides[0]) != perSide || len(in.Sides[1]) != perSide {
			return manualPlan{}, validationError("%s needs %s on each side", in.Format, plural(perSide, "one entry", "two entries"))
		}
	} else if len(ids) != 2*perSide {
		return manualPlan{}, validationError("%s needs %d entries", in.Format, 2*perSide)
	}
	if len(slices.Compact(slices.Sorted(slices.Values(ids)))) != len(ids) {
		return manualPlan{}, validationError("each entry can only be picked once")
	}

	entries, err := lockEntries(ctx, tx, ids)
	if err != nil {
		return manualPlan{}, err
	}
	plan := manualPlan{ev: ev}
	sideOf := map[string]int{}
	var players []string
	names := map[string]string{}
	for k, side := range in.Sides {
		var group []fixtureEntry
		for _, id := range side {
			e, ok := entries[id]
			if !ok || e.eventID != ev.id {
				return manualPlan{}, ErrNoSuchEntry
			}
			if err := checkWaiting(e, in.Format); err != nil {
				return manualPlan{}, err
			}
			if prev, seen := sideOf[e.userID]; seen {
				if prev == k {
					return manualPlan{}, validationError("%s is picked twice", e.name)
				}
				return manualPlan{}, validationError("%s can't play on both sides", e.name)
			}
			sideOf[e.userID] = k
			players = append(players, e.userID)
			names[e.userID] = e.name
			group = append(group, e)
		}
		plan.sides = append(plan.sides, group)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM matches WHERE event_id = $1 AND status <> 'cancelled'`,
		ev.id).Scan(&count); err != nil {
		return manualPlan{}, fmt.Errorf("count matches: %w", err)
	}
	if count >= ev.maxMatches {
		return manualPlan{}, conflictError("this event already has %d %s, its limit", count, plural(count, "match", "matches"))
	}

	taken, err := loadTaken(ctx, tx, ev, players, "")
	if err != nil {
		return manualPlan{}, err
	}
	var start *time.Time
	if in.StartsAt != nil {
		t := in.StartsAt.Truncate(time.Minute).UTC() // slots start on whole minutes
		start = &t
	}
	plan.choice, err = ev.chooseSlot(ev.slotRequest(length, players, taken), in.CourtID, start, names)
	return plan, err
}

// courtClash maps the court exclusion constraint, which only fires when
// another event's match took the court between our read and our write, to
// a conflict.
func courtClash(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == exclusionViolation {
		return conflictError("that court was just taken at that time; pick another slot")
	}
	return err
}

// recordChange writes an audit row.
func recordChange(ctx context.Context, tx pgx.Tx, matchID, eventID, adminID, action string, details any) error {
	id, err := newID("mch")
	if err != nil {
		return err
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("audit details: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO match_changes (id, match_id, event_id, admin_id, action, details) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, matchID, eventID, adminID, action, raw); err != nil {
		return fmt.Errorf("record change: %w", err)
	}
	return nil
}

// loadOneMatch returns the match as the schedule shows it.
func loadOneMatch(ctx context.Context, q rowsQuerier, eventID, matchID string) (ScheduleMatch, error) {
	matches, err := loadScheduleMatches(ctx, q, eventID, matchID)
	if err != nil {
		return ScheduleMatch{}, err
	}
	if len(matches) != 1 {
		return ScheduleMatch{}, ErrNoSuchMatch
	}
	return matches[0], nil
}

func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// nullable is id for a nullable column: nil for "".
func nullable(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func (s *PostgresStore) CreateMatch(ctx context.Context, in ManualMatchInput) (ScheduleMatch, error) {
	var out ScheduleMatch
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		plan, err := planManualMatch(ctx, tx, in)
		if err != nil {
			return err
		}
		c := plan.choice
		matchID, err := newID("mat")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO matches (id, event_id, format, court_id, starts_at, ends_at, judge_id, source)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			matchID, plan.ev.id, in.Format, c.CourtID, c.StartsAt, c.EndsAt, nullable(c.JudgeID), SourceAdmin); err != nil {
			return courtClash(fmt.Errorf("insert match: %w", err))
		}
		sides := map[string][]string{}
		for k, side := range plan.sides {
			for _, e := range side {
				if _, err := tx.Exec(ctx,
					`INSERT INTO match_sides (match_id, side, user_id, entry_id) VALUES ($1, $2, $3, $4)`,
					matchID, sideLabel(k), e.userID, e.id); err != nil {
					return fmt.Errorf("insert match side: %w", err)
				}
				if _, err := tx.Exec(ctx, `
					UPDATE entries SET status = 'scheduled', match_id = $2, standby_reason = NULL, updated_at = now()
					WHERE id = $1`, e.id, matchID); err != nil {
					return fmt.Errorf("schedule entry: %w", err)
				}
				sides[sideLabel(k)] = append(sides[sideLabel(k)], e.id)
			}
		}
		court, _ := plan.ev.court(c.CourtID)
		if err := recordChange(ctx, tx, matchID, plan.ev.id, in.AdminID, ChangeCreate, map[string]any{
			"format": in.Format,
			"sides":  sides,
			"after":  changeSlot{CourtID: court.ID, CourtLabel: court.Label, StartsAt: c.StartsAt, EndsAt: c.EndsAt, JudgeID: optionalID(c.JudgeID)},
		}); err != nil {
			return err
		}
		// TODO(step 11, notifications): tell the players their match.
		out, err = loadOneMatch(ctx, tx, plan.ev.id, matchID)
		return err
	})
	if err != nil {
		return ScheduleMatch{}, err
	}
	return out, nil
}

func (s *PostgresStore) SuggestSlot(ctx context.Context, in ManualMatchInput) (SlotSuggestion, error) {
	var out SlotSuggestion
	// The same checks as CreateMatch, with the same locks so the answer
	// agrees with a create that follows; nothing is written.
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		plan, err := planManualMatch(ctx, tx, in)
		if err != nil {
			return err
		}
		c := plan.choice
		court, _ := plan.ev.court(c.CourtID)
		out = SlotSuggestion{Court: court, StartsAt: c.StartsAt, EndsAt: c.EndsAt}
		var users []string
		for _, side := range plan.sides {
			for _, e := range side {
				users = append(users, e.userID)
			}
		}
		if c.JudgeID != "" {
			var j Judge
			if err := tx.QueryRow(ctx, `SELECT id, name, COALESCE(username, '') FROM users WHERE id = $1`,
				c.JudgeID).Scan(&j.UserID, &j.Name, &j.Username); err != nil {
				return fmt.Errorf("load judge: %w", err)
			}
			out.Judge = &j
		}
		// A rating gap needs the sides: known in singles (one player each),
		// not yet in doubles or mixed.
		var sideRatings []int
		if in.Format == FormatSingles {
			ratings, err := loadRatings(ctx, tx, users)
			if err != nil {
				return err
			}
			for _, u := range users {
				sideRatings = append(sideRatings, ratings.of(u, FormatSingles))
			}
		}
		out.Flags = matchFlags(in.Format, sideRatings, out.Judge != nil)
		return nil
	})
	if err != nil {
		return SlotSuggestion{}, err
	}
	return out, nil
}

func (s *PostgresStore) FreeSlots(ctx context.Context, eventID, format, matchID string) ([]FreeSlot, error) {
	ev, err := loadFixtureEvent(ctx, s.pool, eventID, false)
	if err != nil {
		return nil, err
	}
	length, err := ev.checkFormat(format)
	if err != nil {
		return nil, err
	}
	if matchID != "" {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM matches WHERE id = $1 AND event_id = $2`,
			matchID, eventID).Scan(&n); err != nil {
			return nil, fmt.Errorf("find match: %w", err)
		}
		if n == 0 {
			return nil, ErrNoSuchMatch
		}
	}
	taken, err := loadFixedMatches(ctx, s.pool, eventID, ev.startsAt, ev.endsAt)
	if err != nil {
		return nil, err
	}
	taken = slices.DeleteFunc(taken, func(f FixedMatch) bool { return f.ID == matchID })
	out := []FreeSlot{}
	for _, c := range freeSlots(ev.slotRequest(length, nil, taken)) {
		court, _ := ev.court(c.CourtID)
		out = append(out, FreeSlot{Court: court, StartsAt: c.StartsAt, EndsAt: c.EndsAt})
	}
	return out, nil
}

// fixtureMatch is a match read with its row locked.
type fixtureMatch struct {
	id, eventID, format, status, courtID, judgeID string
	startsAt, endsAt                              time.Time
	challengeID                                   *string
	sides                                         []fixtureSide
}

type fixtureSide struct {
	side, userID, entryID string
}

func (m fixtureMatch) players() []string {
	out := make([]string, len(m.sides))
	for i, s := range m.sides {
		out[i] = s.userID
	}
	return out
}

func (m fixtureMatch) slot(ev fixtureEvent) changeSlot {
	court, _ := ev.court(m.courtID)
	return changeSlot{CourtID: m.courtID, CourtLabel: court.Label, StartsAt: m.startsAt, EndsAt: m.endsAt, JudgeID: optionalID(m.judgeID)}
}

// lockFixtureMatch locks the match's event row, then the match, and reads
// both. A match without an event is ErrNoSuchMatch: only events' matches are
// edited here.
func lockFixtureMatch(ctx context.Context, tx pgx.Tx, matchID string) (fixtureEvent, fixtureMatch, error) {
	var eventID *string
	err := tx.QueryRow(ctx, `SELECT event_id FROM matches WHERE id = $1`, matchID).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && eventID == nil) {
		return fixtureEvent{}, fixtureMatch{}, ErrNoSuchMatch
	}
	if err != nil {
		return fixtureEvent{}, fixtureMatch{}, fmt.Errorf("find match: %w", err)
	}
	ev, err := loadFixtureEvent(ctx, tx, *eventID, true)
	if err != nil {
		return fixtureEvent{}, fixtureMatch{}, err
	}
	m := fixtureMatch{id: matchID, eventID: ev.id}
	if err := tx.QueryRow(ctx, `
		SELECT m.format, m.status, m.court_id, COALESCE(m.judge_id, ''), m.starts_at, m.ends_at,
			(SELECT ch.id FROM challenges ch WHERE ch.match_id = m.id)
		FROM matches m WHERE m.id = $1 FOR UPDATE`, matchID,
	).Scan(&m.format, &m.status, &m.courtID, &m.judgeID, &m.startsAt, &m.endsAt, &m.challengeID); err != nil {
		return fixtureEvent{}, fixtureMatch{}, fmt.Errorf("lock match: %w", err)
	}
	m.startsAt, m.endsAt = m.startsAt.UTC(), m.endsAt.UTC()
	rows, err := tx.Query(ctx, `
		SELECT side, user_id, COALESCE(entry_id, '') FROM match_sides WHERE match_id = $1 ORDER BY side, user_id`, matchID)
	if err != nil {
		return fixtureEvent{}, fixtureMatch{}, fmt.Errorf("match sides: %w", err)
	}
	m.sides, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (fixtureSide, error) {
		var s fixtureSide
		return s, row.Scan(&s.side, &s.userID, &s.entryID)
	})
	if err != nil {
		return fixtureEvent{}, fixtureMatch{}, fmt.Errorf("match sides: %w", err)
	}
	return ev, m, nil
}

// checkEditable checks the match hasn't started or ended: only scheduled
// and ready matches change by hand.
func (m fixtureMatch) checkEditable() error {
	if m.status != MatchScheduled && m.status != MatchReady {
		return conflictError("only a scheduled match can be changed; this one is %s", m.status)
	}
	return nil
}

func (s *PostgresStore) ReplacePlayer(ctx context.Context, in ReplaceInput) (ScheduleMatch, error) {
	var out ScheduleMatch
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		ev, m, err := lockFixtureMatch(ctx, tx, in.MatchID)
		if err != nil {
			return err
		}
		if ev.status != EventPublished {
			return ErrEventNotOpen
		}
		if err := m.checkEditable(); err != nil {
			return err
		}
		if m.challengeID != nil {
			// Both players chose each other and paid for that pairing.
			return conflictError("this is a challenge match: both players chose each other, so neither can be replaced")
		}
		if in.OutEntryID == in.InEntryID {
			return validationError("pick a different entry to bring in")
		}
		entries, err := lockEntries(ctx, tx, []string{in.OutEntryID, in.InEntryID})
		if err != nil {
			return err
		}
		outEntry, okOut := entries[in.OutEntryID]
		inEntry, okIn := entries[in.InEntryID]
		if !okOut || !okIn || outEntry.eventID != ev.id || inEntry.eventID != ev.id {
			return ErrNoSuchEntry
		}
		outSide := slices.IndexFunc(m.sides, func(s fixtureSide) bool { return s.entryID != "" && s.entryID == outEntry.id })
		if outSide < 0 {
			return validationError("%s's entry isn't in this match", outEntry.name)
		}
		if err := checkWaiting(inEntry, m.format); err != nil {
			return err
		}
		for i, s := range m.sides {
			if i != outSide && s.userID == inEntry.userID {
				return validationError("%s already plays in this match", inEntry.name)
			}
		}

		// The incoming player keeps their gap from their other matches.
		var lineup []string
		for i, s := range m.sides {
			if i == outSide {
				lineup = append(lineup, inEntry.userID)
			} else {
				lineup = append(lineup, s.userID)
			}
		}
		taken, err := loadTaken(ctx, tx, ev, lineup, m.id)
		if err != nil {
			return err
		}
		r := ev.slotRequest(m.endsAt.Sub(m.startsAt), []string{inEntry.userID}, taken)
		if b := playerConflict(r, m.startsAt); b != nil {
			return ev.playerError(b, map[string]string{inEntry.userID: inEntry.name})
		}

		if _, err := tx.Exec(ctx, `
			UPDATE match_sides SET user_id = $3, entry_id = $4 WHERE match_id = $1 AND entry_id = $2`,
			m.id, outEntry.id, inEntry.userID, inEntry.id); err != nil {
			return fmt.Errorf("replace player: %w", err)
		}
		if in.Outcome == OutcomeNoShow {
			// They keep the match they missed, and no refund (section 4.4).
			_, err = tx.Exec(ctx, `UPDATE entries SET status = 'no_show', updated_at = now() WHERE id = $1`, outEntry.id)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE entries SET status = 'confirmed', match_id = NULL, standby_reason = NULL, updated_at = now()
				WHERE id = $1`, outEntry.id)
		}
		if err != nil {
			return fmt.Errorf("release entry: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE entries SET status = 'scheduled', match_id = $2, standby_reason = NULL, updated_at = now()
			WHERE id = $1`, inEntry.id, m.id); err != nil {
			return fmt.Errorf("schedule entry: %w", err)
		}

		// A judge who now plays in the match can't judge it.
		judge := m.judgeID
		if judge == inEntry.userID {
			judge = pickJudge(ev.judgeIDs, taken, lineup, m.startsAt, m.endsAt)
			if _, err := tx.Exec(ctx, `UPDATE matches SET judge_id = $2, updated_at = now() WHERE id = $1`,
				m.id, nullable(judge)); err != nil {
				return fmt.Errorf("reassign judge: %w", err)
			}
		}
		if err := recordChange(ctx, tx, m.id, ev.id, in.AdminID, ChangeReplace, map[string]any{
			"out":         changeEntry{EntryID: outEntry.id, UserID: outEntry.userID},
			"in":          changeEntry{EntryID: inEntry.id, UserID: inEntry.userID},
			"outcome":     in.Outcome,
			"side":        m.sides[outSide].side,
			"judgeBefore": optionalID(m.judgeID),
			"judgeAfter":  optionalID(judge),
		}); err != nil {
			return err
		}
		out, err = loadOneMatch(ctx, tx, ev.id, m.id)
		return err
	})
	if err != nil {
		return ScheduleMatch{}, err
	}
	return out, nil
}

func (s *PostgresStore) MoveMatch(ctx context.Context, in MoveInput) (ScheduleMatch, error) {
	var out ScheduleMatch
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		ev, m, err := lockFixtureMatch(ctx, tx, in.MatchID)
		if err != nil {
			return err
		}
		if ev.status != EventPublished {
			return ErrEventNotOpen
		}
		// A challenge's match moves like any other: its players chose each
		// other, not the slot.
		if err := m.checkEditable(); err != nil {
			return err
		}
		court, ok := ev.court(in.CourtID)
		if !ok {
			return validationError("that court isn't one of this event's courts")
		}
		length, ok := ev.slot[m.format]
		if !ok {
			length = m.endsAt.Sub(m.startsAt)
		}
		start := in.StartsAt.Truncate(time.Minute).UTC()
		if err := ev.checkStart(start, length); err != nil {
			return err
		}
		players := m.players()
		taken, err := loadTaken(ctx, tx, ev, players, m.id)
		if err != nil {
			return err
		}
		names, err := userNames(ctx, tx, players)
		if err != nil {
			return err
		}
		r := ev.slotRequest(length, players, taken)
		if err := ev.checkSlot(r, court, start, names); err != nil {
			return err
		}
		end := start.Add(length)
		judge := m.judgeID
		if judge == "" || !slices.Contains(ev.judgeIDs, judge) || !judgeFree(judge, taken, players, start, end) {
			judge = pickJudge(ev.judgeIDs, taken, players, start, end)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE matches SET court_id = $2, starts_at = $3, ends_at = $4, judge_id = $5, updated_at = now()
			WHERE id = $1`, m.id, court.ID, start, end, nullable(judge)); err != nil {
			return courtClash(fmt.Errorf("move match: %w", err))
		}
		if err := recordChange(ctx, tx, m.id, ev.id, in.AdminID, ChangeMove, map[string]any{
			"before": m.slot(ev),
			"after":  changeSlot{CourtID: court.ID, CourtLabel: court.Label, StartsAt: start, EndsAt: end, JudgeID: optionalID(judge)},
		}); err != nil {
			return err
		}
		// TODO(step 11, notifications): tell the players and the judge (rule 7).
		out, err = loadOneMatch(ctx, tx, ev.id, m.id)
		return err
	})
	if err != nil {
		return ScheduleMatch{}, err
	}
	return out, nil
}

func (s *PostgresStore) CancelMatch(ctx context.Context, matchID, adminID string) (ScheduleMatch, error) {
	var out ScheduleMatch
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		ev, m, err := lockFixtureMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if err := m.checkEditable(); err != nil {
			return err
		}
		if m.challengeID != nil {
			// Its locked entries were paid for that pairing.
			return conflictError("challenges are cancelled by withdrawing or refunding, not here")
		}
		if _, err := tx.Exec(ctx, `UPDATE matches SET status = 'cancelled', updated_at = now() WHERE id = $1`, m.id); err != nil {
			return fmt.Errorf("cancel match: %w", err)
		}
		// Its open entries go back to the pool. Their match_sides rows keep
		// who was in the match but let go of the entry, which may now be
		// used for another match (an entry is in at most one).
		rows, err := tx.Query(ctx, `
			SELECT id FROM entries WHERE match_id = $1 AND kind = 'open' AND status = 'scheduled'
			ORDER BY id FOR UPDATE`, m.id)
		if err != nil {
			return fmt.Errorf("lock entries: %w", err)
		}
		released, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("lock entries: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE entries SET status = 'confirmed', match_id = NULL, standby_reason = NULL, updated_at = now()
			WHERE id = ANY($1)`, released); err != nil {
			return fmt.Errorf("release entries: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE match_sides SET entry_id = NULL WHERE match_id = $1 AND entry_id = ANY($2)`,
			m.id, released); err != nil {
			return fmt.Errorf("release match sides: %w", err)
		}
		if released == nil {
			released = []string{}
		}
		before := m.slot(ev)
		if err := recordChange(ctx, tx, m.id, ev.id, adminID, ChangeCancel, map[string]any{
			"before":           map[string]any{"status": m.status, "slot": before},
			"releasedEntryIds": released,
		}); err != nil {
			return err
		}
		// TODO(step 11, notifications): tell the players and the judge.
		out, err = loadOneMatch(ctx, tx, ev.id, m.id)
		return err
	})
	if err != nil {
		return ScheduleMatch{}, err
	}
	return out, nil
}

func (s *PostgresStore) ListMatchChanges(ctx context.Context, eventID string) ([]MatchChange, error) {
	if _, err := statusOf(ctx, s.pool, eventID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT mc.id, mc.match_id, mc.action, mc.details, mc.created_at, u.id, COALESCE(u.username, ''), u.name
		FROM match_changes mc JOIN users u ON u.id = mc.admin_id
		WHERE mc.event_id = $1
		ORDER BY mc.created_at DESC, mc.id DESC
		LIMIT $2`, eventID, matchChangesLimit)
	if err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}
	changes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (MatchChange, error) {
		var c MatchChange
		err := row.Scan(&c.ID, &c.MatchID, &c.Action, &c.Details, &c.CreatedAt, &c.Admin.ID, &c.Admin.Username, &c.Admin.Name)
		c.CreatedAt = c.CreatedAt.UTC()
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}
	if changes == nil {
		changes = []MatchChange{}
	}
	return changes, nil
}
