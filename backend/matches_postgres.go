package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// exclusionViolation is the SQLSTATE code for an exclusion constraint, here
// a court holding two matches at overlapping times.
const exclusionViolation = "23P01"

// loadScheduleData reads what the matcher needs for one event. With lock it
// locks the event row and its confirmed open entries, for publishing.
func loadScheduleData(ctx context.Context, q rowsQuerier, eventID string, lock bool) (scheduleData, error) {
	d := scheduleData{eventID: eventID, users: map[string]PublicUser{}, courts: map[string]Court{}}
	in := &d.input
	forUpdate := ""
	if lock {
		forUpdate = " FOR UPDATE"
	}

	var singles, doubles, mixed int
	err := q.QueryRow(ctx, `
		SELECT status, formats, singles_slot_minutes, doubles_slot_minutes, mixed_slot_minutes,
			starts_at, ends_at, max_matches, registration_closes_at, schedule_published_at
		FROM events WHERE id = $1`+forUpdate, eventID,
	).Scan(&d.status, &in.Formats, &singles, &doubles, &mixed,
		&in.StartsAt, &in.EndsAt, &in.MaxMatches, &d.registrationClosesAt, &d.publishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNoSuchEvent
	}
	if err != nil {
		return d, fmt.Errorf("load event: %w", err)
	}
	in.StartsAt, in.EndsAt, d.registrationClosesAt = in.StartsAt.UTC(), in.EndsAt.UTC(), d.registrationClosesAt.UTC()
	if d.publishedAt != nil {
		utc := d.publishedAt.UTC()
		d.publishedAt = &utc
	}
	slot := map[string]int{FormatSingles: singles, FormatDoubles: doubles, FormatMixed: mixed}
	in.SlotMinutes = map[string]int{}
	for _, f := range in.Formats {
		in.SlotMinutes[f] = slot[f]
	}

	rows, err := q.Query(ctx, `
		SELECT c.id, c.label FROM event_courts ec JOIN courts c ON c.id = ec.court_id
		WHERE ec.event_id = $1 ORDER BY length(c.label), c.label`, eventID)
	if err != nil {
		return d, fmt.Errorf("event courts: %w", err)
	}
	if in.Courts, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Court]); err != nil {
		return d, fmt.Errorf("event courts: %w", err)
	}
	for _, c := range in.Courts {
		d.courts[c.ID] = c
	}

	rows, err = q.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), u.name
		FROM event_judges j JOIN users u ON u.id = j.user_id
		WHERE j.event_id = $1 ORDER BY u.id`, eventID)
	if err != nil {
		return d, fmt.Errorf("event judges: %w", err)
	}
	judges, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PublicUser])
	if err != nil {
		return d, fmt.Errorf("event judges: %w", err)
	}
	for _, j := range judges {
		in.JudgeIDs = append(in.JudgeIDs, j.ID)
		d.users[j.ID] = j
	}

	// Only open entries are matched. Locked ones (challenges) get their slot
	// when both players have paid; their matches come in as Fixed below.
	forUpdateEntries := ""
	if lock {
		forUpdateEntries = " FOR UPDATE OF en"
	}
	rows, err = q.Query(ctx, `
		SELECT en.id, en.user_id, en.formats, COALESCE(en.partner_user_id, ''), en.created_at,
			COALESCE(u.username, ''), u.name
		FROM entries en JOIN users u ON u.id = en.user_id
		WHERE en.event_id = $1 AND en.kind = 'open' AND en.status = 'confirmed'
		ORDER BY en.id`+forUpdateEntries, eventID)
	if err != nil {
		return d, fmt.Errorf("event entries: %w", err)
	}
	defer rows.Close()
	var userIDs []string
	for rows.Next() {
		var e MatcherEntry
		var u PublicUser
		if err := rows.Scan(&e.ID, &e.UserID, &e.Formats, &e.PartnerUserID, &e.CreatedAt, &u.Username, &u.Name); err != nil {
			return d, fmt.Errorf("event entries: %w", err)
		}
		u.ID = e.UserID
		d.users[e.UserID] = u
		userIDs = append(userIDs, e.UserID)
		in.Entries = append(in.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return d, fmt.Errorf("event entries: %w", err)
	}

	ratings, err := loadRatings(ctx, q, userIDs)
	if err != nil {
		return d, err
	}
	for i := range in.Entries {
		e := &in.Entries[i]
		e.Ratings = map[string]int{}
		for _, f := range formatOrder {
			e.Ratings[f] = ratings.of(e.UserID, f)
		}
	}

	// Matches already scheduled (challenges, rule 7) and other events' on
	// these courts: taken court time, busy players and judges.
	if in.Fixed, err = loadFixedMatches(ctx, q, eventID, in.StartsAt, in.EndsAt); err != nil {
		return d, err
	}
	return d, nil
}

func (s *PostgresStore) PreviewSchedule(ctx context.Context, eventID string, now time.Time) (ScheduleView, error) {
	d, err := loadScheduleData(ctx, s.pool, eventID, false)
	if err != nil {
		return ScheduleView{}, err
	}
	return renderSchedule(d, runMatcher(d.input), nil, now), nil
}

func (s *PostgresStore) PublishSchedule(ctx context.Context, eventID, adminID string, now time.Time) (ScheduleView, error) {
	var view ScheduleView
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Locking the event row first, as CreateEntry and MarkEntryPaid do,
		// means no entry joins or is confirmed while the matcher runs, and a
		// second publish waits and then sees the first.
		d, err := loadScheduleData(ctx, tx, eventID, true)
		if err != nil {
			return err
		}
		switch {
		case d.status != EventPublished:
			return ErrEventNotPublished
		case d.publishedAt != nil:
			// TODO(walk-ins, step 10): later runs that only fill gaps, never
			// moving a match players were told about (rule 7).
			return ErrSchedulePublished
		}
		sched := runMatcher(d.input)
		if len(sched.Matches) == 0 {
			// Publishing would lock the event out of a schedule for good.
			return ErrNothingToSchedule
		}

		ids := make([]string, len(sched.Matches))
		for i, m := range sched.Matches {
			if ids[i], err = newID("mat"); err != nil {
				return err
			}
			var judge any // nil: no judge free
			if m.JudgeID != "" {
				judge = m.JudgeID
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO matches (id, event_id, format, court_id, starts_at, ends_at, judge_id, source)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				ids[i], eventID, m.Format, m.CourtID, m.StartsAt, m.EndsAt, judge, SourceMatcher); err != nil {
				return fmt.Errorf("insert match: %w", err)
			}
			for k, side := range m.Sides {
				for _, p := range side.Players {
					if _, err := tx.Exec(ctx,
						`INSERT INTO match_sides (match_id, side, user_id, entry_id) VALUES ($1, $2, $3, $4)`,
						ids[i], sideLabel(k), p.UserID, p.EntryID); err != nil {
						return fmt.Errorf("insert match side: %w", err)
					}
					if _, err := tx.Exec(ctx, `
						UPDATE entries SET status = 'scheduled', match_id = $2, standby_reason = NULL, updated_at = now()
						WHERE id = $1`, p.EntryID, ids[i]); err != nil {
						return fmt.Errorf("schedule entry: %w", err)
					}
				}
			}
		}
		for _, e := range sched.Standby {
			if _, err := tx.Exec(ctx, `
				UPDATE entries SET status = 'standby', standby_reason = $2, updated_at = now()
				WHERE id = $1`, e.EntryID, e.Reason); err != nil {
				return fmt.Errorf("standby entry: %w", err)
			}
		}
		var publishedAt time.Time
		if err := tx.QueryRow(ctx, `
			UPDATE events SET schedule_published_at = $2, schedule_published_by = $3, updated_at = now()
			WHERE id = $1 RETURNING schedule_published_at`, eventID, now.UTC(), adminID,
		).Scan(&publishedAt); err != nil {
			return fmt.Errorf("record publish: %w", err)
		}
		publishedAt = publishedAt.UTC()
		d.publishedAt = &publishedAt
		view = renderSchedule(d, sched, ids, now)
		return nil
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == exclusionViolation {
		// Another event's match on one of these courts: courts are checked
		// per event when it's saved, so only data written some other way
		// gets here.
		return ScheduleView{}, ErrScheduleCourtClashes
	}
	if err != nil {
		return ScheduleView{}, err
	}
	return view, nil
}

func (s *PostgresStore) PublishedSchedule(ctx context.Context, eventID string) (ScheduleView, error) {
	view := ScheduleView{EventID: eventID, Matches: []ScheduleMatch{}, Standby: []ScheduleStandby{}, Warnings: []string{}}
	err := s.pool.QueryRow(ctx, `SELECT schedule_published_at FROM events WHERE id = $1`, eventID).Scan(&view.PublishedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ScheduleView{}, ErrNoSuchEvent
	case err != nil:
		return ScheduleView{}, fmt.Errorf("load event: %w", err)
	}
	if view.PublishedAt != nil {
		*view.PublishedAt = view.PublishedAt.UTC()
	}
	if view.Matches, err = loadScheduleMatches(ctx, s.pool, eventID, ""); err != nil {
		return ScheduleView{}, err
	}
	// Matches an admin made by hand show before a publish too.
	if view.PublishedAt == nil && len(view.Matches) == 0 {
		return ScheduleView{}, ErrScheduleNotPublished
	}

	rows, err := s.pool.Query(ctx, `
		SELECT en.id, COALESCE(en.standby_reason, ''), u.id, COALESCE(u.username, ''), u.name
		FROM entries en JOIN users u ON u.id = en.user_id
		WHERE en.event_id = $1 AND en.status = 'standby'
		ORDER BY en.created_at, en.id`, eventID)
	if err != nil {
		return ScheduleView{}, fmt.Errorf("load standby: %w", err)
	}
	view.Standby, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ScheduleStandby, error) {
		var st ScheduleStandby
		err := row.Scan(&st.EntryID, &st.Reason, &st.User.ID, &st.User.Username, &st.User.Name)
		return st, err
	})
	if err != nil {
		return ScheduleView{}, fmt.Errorf("load standby: %w", err)
	}
	if view.Standby == nil {
		view.Standby = []ScheduleStandby{}
	}
	return view, nil
}

// loadScheduleMatches returns the event's matches as the schedule shows
// them, with both sides, each player's current rating, and the flags worked
// out again from what was saved (ratings may have moved since). With matchID
// "" that's every match that isn't cancelled, by start and then court; with
// a matchID just that one, whatever its status (none if it isn't the
// event's).
func loadScheduleMatches(ctx context.Context, q rowsQuerier, eventID, matchID string) ([]ScheduleMatch, error) {
	rows, err := q.Query(ctx, `
		SELECT m.id, m.status, m.source, ch.id, m.format, m.starts_at, m.ends_at, c.id, c.label,
			j.id, COALESCE(j.name, ''), COALESCE(j.username, '')
		FROM matches m
		JOIN courts c ON c.id = m.court_id
		LEFT JOIN users j ON j.id = m.judge_id
		LEFT JOIN challenges ch ON ch.match_id = m.id
		WHERE m.event_id = $1 AND (($2 = '' AND m.status <> 'cancelled') OR m.id = $2)
		ORDER BY m.starts_at, length(c.label), c.label, m.id`, eventID, matchID)
	if err != nil {
		return nil, fmt.Errorf("load matches: %w", err)
	}
	matches, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ScheduleMatch, error) {
		var m ScheduleMatch
		var id string
		var judgeID *string
		var judge Judge
		err := row.Scan(&id, &m.Status, &m.Source, &m.ChallengeID, &m.Format, &m.StartsAt, &m.EndsAt, &m.Court.ID, &m.Court.Label,
			&judgeID, &judge.Name, &judge.Username)
		m.ID = &id
		m.StartsAt, m.EndsAt = m.StartsAt.UTC(), m.EndsAt.UTC()
		if judgeID != nil {
			judge.UserID = *judgeID
			m.Judge = &judge
		}
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("load matches: %w", err)
	}
	if len(matches) == 0 {
		return []ScheduleMatch{}, nil
	}

	ids := make([]string, len(matches))
	byID := make(map[string]*ScheduleMatch, len(matches))
	for i := range matches {
		ids[i] = *matches[i].ID
		byID[ids[i]] = &matches[i]
	}
	rows, err = q.Query(ctx, `
		SELECT ms.match_id, ms.side, u.id, u.name, COALESCE(u.username, ''), COALESCE(r.rating, $2),
			COALESCE(ms.entry_id, '')
		FROM match_sides ms
		JOIN matches m ON m.id = ms.match_id
		JOIN users u ON u.id = ms.user_id
		LEFT JOIN player_ratings r ON r.user_id = ms.user_id AND r.discipline = m.format
		WHERE ms.match_id = ANY($1)
		ORDER BY ms.match_id, ms.side, COALESCE(r.rating, $2) DESC, u.id`, ids, startingRating)
	if err != nil {
		return nil, fmt.Errorf("load match sides: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var matchID, side string
		var p MatchPlayer
		if err := rows.Scan(&matchID, &side, &p.UserID, &p.Name, &p.Username, &p.Rating, &p.EntryID); err != nil {
			return nil, fmt.Errorf("load match sides: %w", err)
		}
		m := byID[matchID]
		if n := len(m.Sides); n == 0 || m.Sides[n-1].Side != side {
			m.Sides = append(m.Sides, MatchSide{Side: side})
		}
		last := &m.Sides[len(m.Sides)-1]
		last.Players = append(last.Players, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load match sides: %w", err)
	}
	// The same flags the preview showed, worked out again from what was
	// saved: ratings may have moved since.
	for i := range matches {
		m := &matches[i]
		var ratings []int
		for k := range m.Sides {
			m.Sides[k].Rating = sideAverage(m.Sides[k].Players)
			ratings = append(ratings, m.Sides[k].Rating)
		}
		m.Flags = matchFlags(m.Format, ratings, m.Judge != nil)
	}
	return matches, nil
}

// adminEntryColumns is an entries row (en) with its player (u), partner (p)
// and confirming admin (cb), as adminEntryFrom joins them. scanAdminEntry
// reads them in this order.
const adminEntryColumns = `en.id, en.kind, en.formats, en.source, en.status, en.hold_expires_at,
	(en.status = 'pending_payment' AND en.hold_expires_at <= now()), en.match_id, en.standby_reason,
	en.confirmed_at, en.created_at,
	u.id, COALESCE(u.username, ''), u.name,
	p.id, COALESCE(p.username, ''), COALESCE(p.name, ''),
	cb.id, COALESCE(cb.username, ''), COALESCE(cb.name, '')`

const adminEntryFrom = `FROM entries en
	JOIN users u ON u.id = en.user_id
	LEFT JOIN users p ON p.id = en.partner_user_id
	LEFT JOIN users cb ON cb.id = en.confirmed_by`

// eventEntriesLimit caps an event's entry list; capacity is at most 1000.
const eventEntriesLimit = 1000

func scanAdminEntry(row pgx.Row) (AdminEntry, error) {
	var en AdminEntry
	var partnerID, confirmerID *string
	var partner, confirmer PublicUser
	err := row.Scan(&en.ID, &en.Kind, &en.Formats, &en.Source, &en.Status, &en.HoldExpiresAt,
		&en.HoldExpired, &en.MatchID, &en.StandbyReason, &en.ConfirmedAt, &en.CreatedAt,
		&en.User.ID, &en.User.Username, &en.User.Name,
		&partnerID, &partner.Username, &partner.Name,
		&confirmerID, &confirmer.Username, &confirmer.Name)
	if err != nil {
		return AdminEntry{}, err
	}
	if partnerID != nil {
		partner.ID = *partnerID
		en.Partner = &partner
	}
	if confirmerID != nil {
		confirmer.ID = *confirmerID
		en.ConfirmedBy = &confirmer
	}
	for _, t := range []**time.Time{&en.HoldExpiresAt, &en.ConfirmedAt} {
		if *t != nil {
			utc := (*t).UTC()
			*t = &utc
		}
	}
	en.CreatedAt = en.CreatedAt.UTC()
	return en, nil
}

// withRatings fills in each entry's Ratings for its formats.
func withRatings(ctx context.Context, q rowsQuerier, entries []AdminEntry) error {
	userIDs := make([]string, len(entries))
	for i, en := range entries {
		userIDs[i] = en.User.ID
	}
	ratings, err := loadRatings(ctx, q, userIDs)
	if err != nil {
		return err
	}
	for i := range entries {
		en := &entries[i]
		en.Ratings = make(map[string]int, len(en.Formats))
		for _, f := range en.Formats {
			en.Ratings[f] = ratings.of(en.User.ID, f)
		}
	}
	return nil
}

func loadAdminEntry(ctx context.Context, q rowsQuerier, entryID string) (AdminEntry, error) {
	en, err := scanAdminEntry(q.QueryRow(ctx, `SELECT `+adminEntryColumns+` `+adminEntryFrom+` WHERE en.id = $1`, entryID))
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminEntry{}, ErrNoSuchEntry
	}
	if err != nil {
		return AdminEntry{}, fmt.Errorf("load entry: %w", err)
	}
	entries := []AdminEntry{en}
	if err := withRatings(ctx, q, entries); err != nil {
		return AdminEntry{}, err
	}
	return entries[0], nil
}

func (s *PostgresStore) ListEventEntries(ctx context.Context, eventID string) ([]AdminEntry, error) {
	if _, err := statusOf(ctx, s.pool, eventID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+adminEntryColumns+` `+adminEntryFrom+`
		WHERE en.event_id = $1
		ORDER BY en.created_at, en.id
		LIMIT $2`, eventID, eventEntriesLimit)
	if err != nil {
		return nil, fmt.Errorf("list event entries: %w", err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminEntry, error) { return scanAdminEntry(row) })
	if err != nil {
		return nil, fmt.Errorf("list event entries: %w", err)
	}
	if entries == nil {
		entries = []AdminEntry{}
	}
	return entries, withRatings(ctx, s.pool, entries)
}

func (s *PostgresStore) MarkEntryPaid(ctx context.Context, entryID, adminID string, now time.Time) (AdminEntry, error) {
	var entry AdminEntry
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var eventID string
		err := tx.QueryRow(ctx, `SELECT event_id FROM entries WHERE id = $1`, entryID).Scan(&eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchEntry
		}
		if err != nil {
			return fmt.Errorf("find entry: %w", err)
		}

		// The event row first, then the entry: the order CreateEntry and
		// PublishSchedule lock in, so the counts below can't go stale and
		// nothing deadlocks.
		var status, currency string
		var endsAt time.Time
		var capacity, maxMatches, fee int
		if err := tx.QueryRow(ctx, `
			SELECT status, ends_at, capacity, max_matches, fee_paise, currency FROM events WHERE id = $1 FOR UPDATE`,
			eventID).Scan(&status, &endsAt, &capacity, &maxMatches, &fee, &currency); err != nil {
			return fmt.Errorf("lock event: %w", err)
		}
		var entryStatus, userID, kind string
		if err := tx.QueryRow(ctx, `SELECT status, user_id, kind FROM entries WHERE id = $1 FOR UPDATE`,
			entryID).Scan(&entryStatus, &userID, &kind); err != nil {
			return fmt.Errorf("lock entry: %w", err)
		}
		switch {
		// A challenge's entries are paid in the app: confirming one fixes
		// the match, and a failed challenge refunds through the provider.
		case kind == EntryLocked:
			return ErrEntryInChallenge
		case entryStatus != EntryPendingPayment:
			return ErrEntryNotPending
		case status != EventPublished:
			return ErrEventNotOpen
		case !now.Before(endsAt):
			return ErrEventOver
		}
		// The registration cutoff doesn't apply: the desk serves walk-ins.

		// Live entries other than this one, by the same rule as CreateEntry.
		// A held entry was already counted, so it always fits; an expired
		// hold has to find room again.
		if err := checkRoom(ctx, tx, eventID, userID, entryID, capacity, maxMatches, now); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE entries SET status = 'confirmed', confirmed_by = $2, confirmed_at = $3, updated_at = now()
			WHERE id = $1`, entryID, adminID, now.UTC()); err != nil {
			return fmt.Errorf("confirm entry: %w", err)
		}
		// The money taken at the desk, in cash or by UPI, for the event's
		// fee. A free event takes none, so it has no payment row. An online
		// order the player left open stays open: if they pay it after all,
		// ConfirmPayment refunds it as a second payment.
		if fee > 0 {
			paymentID, err := newID("pmt")
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO payments (id, entry_id, user_id, provider, kind, amount_paise, currency, status,
					recorded_by, paid_at)
				VALUES ($1, $2, $3, 'offline', 'offline', $4, $5, 'paid', $6, $7)`,
				paymentID, entryID, userID, fee, currency, adminID, now.UTC()); err != nil {
				return fmt.Errorf("record offline payment: %w", err)
			}
		}
		entry, err = loadAdminEntry(ctx, tx, entryID)
		return err
	})
	if err != nil {
		return AdminEntry{}, err
	}
	return entry, nil
}

// userMatchesLimit caps a player's match list until it needs paging.
const userMatchesLimit = 100

func (s *PostgresStore) ListUserMatches(ctx context.Context, userID string) ([]PlayerMatch, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.status, m.source, m.format, m.starts_at, m.ends_at, m.winner_side, me.side,
			e.id, e.title,
			c.id, c.label,
			`+venueColumns+`,
			j.id, COALESCE(j.name, ''), COALESCE(j.username, ''),
			ch.id
		FROM match_sides me
		JOIN matches m ON m.id = me.match_id
		LEFT JOIN events e ON e.id = m.event_id
		JOIN courts c ON c.id = m.court_id
		JOIN venues v ON v.id = c.venue_id
		LEFT JOIN users j ON j.id = m.judge_id
		LEFT JOIN challenges ch ON ch.match_id = m.id
		WHERE me.user_id = $1
		ORDER BY m.starts_at, m.id
		LIMIT $2`, userID, userMatchesLimit)
	if err != nil {
		return nil, fmt.Errorf("list matches: %w", err)
	}
	matches, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PlayerMatch, error) {
		var m PlayerMatch
		var eventID, eventTitle, judgeID *string
		var judge Judge
		v := &m.Venue
		err := row.Scan(&m.ID, &m.Status, &m.Source, &m.Format, &m.StartsAt, &m.EndsAt, &m.WinnerSide, &m.MySide,
			&eventID, &eventTitle,
			&m.Court.ID, &m.Court.Label,
			&v.ID, &v.Name, &v.Address, &v.City, &v.Lat, &v.Lng, &v.Timezone,
			&judgeID, &judge.Name, &judge.Username,
			&m.ChallengeID)
		if eventID != nil {
			m.Event = &MatchEvent{ID: *eventID, Title: *eventTitle}
		}
		if judgeID != nil {
			judge.UserID = *judgeID
			m.Judge = &judge
		}
		m.StartsAt, m.EndsAt = m.StartsAt.UTC(), m.EndsAt.UTC()
		m.Sides = []MatchSide{}
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("list matches: %w", err)
	}
	if len(matches) == 0 {
		return []PlayerMatch{}, nil
	}

	// Both sides of every match, with each player's current rating in the
	// match's format.
	ids := make([]string, len(matches))
	byID := make(map[string]*PlayerMatch, len(matches))
	for i := range matches {
		ids[i] = matches[i].ID
		byID[matches[i].ID] = &matches[i]
	}
	rows, err = s.pool.Query(ctx, `
		SELECT ms.match_id, ms.side, u.id, u.name, COALESCE(u.username, ''), COALESCE(r.rating, $2)
		FROM match_sides ms
		JOIN matches m ON m.id = ms.match_id
		JOIN users u ON u.id = ms.user_id
		LEFT JOIN player_ratings r ON r.user_id = ms.user_id AND r.discipline = m.format
		WHERE ms.match_id = ANY($1)
		ORDER BY ms.match_id, ms.side, COALESCE(r.rating, $2) DESC, u.id`, ids, startingRating)
	if err != nil {
		return nil, fmt.Errorf("match sides: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var matchID, side string
		var p MatchPlayer
		if err := rows.Scan(&matchID, &side, &p.UserID, &p.Name, &p.Username, &p.Rating); err != nil {
			return nil, fmt.Errorf("match sides: %w", err)
		}
		m := byID[matchID]
		if n := len(m.Sides); n == 0 || m.Sides[n-1].Side != side {
			m.Sides = append(m.Sides, MatchSide{Side: side})
		}
		last := &m.Sides[len(m.Sides)-1]
		last.Players = append(last.Players, p)
		last.Rating = sideAverage(last.Players)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("match sides: %w", err)
	}
	return matches, nil
}

func (s *PostgresStore) PlayerStats(ctx context.Context, userID string) (PlayerStats, error) {
	stats := PlayerStats{Ratings: map[string]int{FormatSingles: startingRating, FormatDoubles: startingRating, FormatMixed: startingRating}}
	rows, err := s.pool.Query(ctx, `SELECT discipline, rating FROM player_ratings WHERE user_id = $1`, userID)
	if err != nil {
		return PlayerStats{}, fmt.Errorf("load ratings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var discipline string
		var rating int
		if err := rows.Scan(&discipline, &rating); err != nil {
			return PlayerStats{}, fmt.Errorf("load ratings: %w", err)
		}
		stats.Ratings[discipline] = rating
	}
	if err := rows.Err(); err != nil {
		return PlayerStats{}, fmt.Errorf("load ratings: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE m.winner_side = ms.side)
		FROM match_sides ms JOIN matches m ON m.id = ms.match_id
		WHERE ms.user_id = $1 AND m.status IN ('completed', 'retired')`, userID,
	).Scan(&stats.Played, &stats.Won); err != nil {
		return PlayerStats{}, fmt.Errorf("count matches: %w", err)
	}
	return stats, nil
}
