package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// challengeSelect reads challenges as the viewer ($1) sees them, with
// ratings defaulting to $2 (startingRating). Only the two players' rows
// match. scanChallenge reads the columns in this order.
const challengeSelect = `
	SELECT ch.id, ch.status, ch.format, ch.preferred_start, ch.expires_at, ch.created_at, ch.responded_at,
		e.id, e.title, e.starts_at, e.ends_at, e.registration_closes_at, e.fee_paise, e.currency,
		` + venueColumns + `,
		cu.id, COALESCE(cu.username, ''), cu.name, COALESCE(cr.rating, $2),
		ou.id, COALESCE(ou.username, ''), ou.name, COALESCE(orr.rating, $2),
		me.id, me.status, me.hold_expires_at,
		EXISTS (SELECT 1 FROM entries t WHERE t.challenge_id = ch.id AND t.user_id <> $1
			AND t.status IN ('confirmed', 'scheduled')),
		m.id, c.id, c.label, m.starts_at, m.ends_at,
		j.id, COALESCE(j.name, ''), COALESCE(j.username, '')
	FROM challenges ch
	JOIN events e ON e.id = ch.event_id
	JOIN venues v ON v.id = e.venue_id
	JOIN users cu ON cu.id = ch.challenger_id
	JOIN users ou ON ou.id = ch.opponent_id
	LEFT JOIN player_ratings cr ON cr.user_id = ch.challenger_id AND cr.discipline = ch.format
	LEFT JOIN player_ratings orr ON orr.user_id = ch.opponent_id AND orr.discipline = ch.format
	LEFT JOIN entries me ON me.challenge_id = ch.id AND me.user_id = $1
	LEFT JOIN matches m ON m.id = ch.match_id
	LEFT JOIN courts c ON c.id = m.court_id
	LEFT JOIN users j ON j.id = m.judge_id
	WHERE (ch.challenger_id = $1 OR ch.opponent_id = $1)`

func scanChallenge(row pgx.Row, viewerID string) (Challenge, error) {
	var ch Challenge
	ev := &ch.Event
	var myEntryID, myEntryStatus, matchID, courtID, courtLabel, judgeID *string
	var myHold, matchStart, matchEnd *time.Time
	var judge Judge
	err := row.Scan(&ch.ID, &ch.Status, &ch.Format, &ch.PreferredStart, &ch.ExpiresAt, &ch.CreatedAt, &ch.RespondedAt,
		&ev.ID, &ev.Title, &ev.StartsAt, &ev.EndsAt, &ev.RegistrationClosesAt, &ev.FeePaise, &ev.Currency,
		&ev.Venue.ID, &ev.Venue.Name, &ev.Venue.Address, &ev.Venue.City, &ev.Venue.Lat, &ev.Venue.Lng, &ev.Venue.Timezone,
		&ch.Challenger.ID, &ch.Challenger.Username, &ch.Challenger.Name, &ch.Challenger.Rating,
		&ch.Opponent.ID, &ch.Opponent.Username, &ch.Opponent.Name, &ch.Opponent.Rating,
		&myEntryID, &myEntryStatus, &myHold,
		&ch.TheirEntryPaid,
		&matchID, &courtID, &courtLabel, &matchStart, &matchEnd,
		&judgeID, &judge.Name, &judge.Username)
	if err != nil {
		return Challenge{}, err
	}
	ch.Role = RoleOpponent
	if ch.Challenger.ID == viewerID {
		ch.Role = RoleChallenger
	}
	ch.PreferredStart, ch.ExpiresAt, ch.CreatedAt = ch.PreferredStart.UTC(), ch.ExpiresAt.UTC(), ch.CreatedAt.UTC()
	ev.StartsAt, ev.EndsAt, ev.RegistrationClosesAt = ev.StartsAt.UTC(), ev.EndsAt.UTC(), ev.RegistrationClosesAt.UTC()
	if ch.RespondedAt != nil {
		utc := ch.RespondedAt.UTC()
		ch.RespondedAt = &utc
	}
	if myEntryID != nil {
		ch.MyEntry = &ChallengeEntry{ID: *myEntryID, Status: *myEntryStatus}
		if myHold != nil {
			utc := myHold.UTC()
			ch.MyEntry.HoldExpiresAt = &utc
		}
	}
	if matchID != nil {
		ch.Match = &ChallengeMatch{
			ID: *matchID, Court: Court{ID: *courtID, Label: *courtLabel},
			StartsAt: matchStart.UTC(), EndsAt: matchEnd.UTC(),
		}
		if judgeID != nil {
			judge.UserID = *judgeID
			ch.Match.Judge = &judge
		}
	}
	return ch, nil
}

// loadChallenge returns the challenge as viewerID sees it, or
// ErrNoSuchChallenge if it isn't one of theirs.
func loadChallenge(ctx context.Context, q querier, viewerID, challengeID string) (Challenge, error) {
	ch, err := scanChallenge(q.QueryRow(ctx, challengeSelect+` AND ch.id = $3`,
		viewerID, startingRating, challengeID), viewerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Challenge{}, ErrNoSuchChallenge
	}
	if err != nil {
		return Challenge{}, fmt.Errorf("load challenge: %w", err)
	}
	return ch, nil
}

// challengeEvent is what sending and accepting check about the event.
type challengeEvent struct {
	status, timezone                       string
	formats                                []string
	startsAt, endsAt, registrationClosesAt time.Time
	singlesSlot                            time.Duration
	capacity, maxMatches                   int
	ratingMin, ratingMax                   *int
}

// lockChallengeEvent locks the event row, the first lock every challenge
// and payment transaction takes, and reads it.
func lockChallengeEvent(ctx context.Context, q querier, eventID string) (challengeEvent, error) {
	var ev challengeEvent
	var slot int
	err := q.QueryRow(ctx, `
		SELECT e.status, v.timezone, e.formats, e.starts_at, e.ends_at, e.registration_closes_at,
			e.singles_slot_minutes, e.capacity, e.max_matches, e.rating_min, e.rating_max
		FROM events e JOIN venues v ON v.id = e.venue_id
		WHERE e.id = $1 FOR UPDATE OF e`, eventID,
	).Scan(&ev.status, &ev.timezone, &ev.formats, &ev.startsAt, &ev.endsAt, &ev.registrationClosesAt,
		&slot, &ev.capacity, &ev.maxMatches, &ev.ratingMin, &ev.ratingMax)
	if errors.Is(err, pgx.ErrNoRows) {
		return ev, ErrNoSuchEvent
	}
	if err != nil {
		return ev, fmt.Errorf("lock event: %w", err)
	}
	ev.singlesSlot = time.Duration(slot) * time.Minute
	return ev, nil
}

// open checks the event takes challenges at now: published, before the
// cutoff, offering singles.
func (ev challengeEvent) open(now time.Time) error {
	switch {
	case ev.status == EventDraft: // invisible to players
		return ErrNoSuchEvent
	case ev.status != EventPublished:
		return ErrEventNotOpen
	case !now.Before(ev.registrationClosesAt):
		return ErrRegistrationClosed
	case !slices.Contains(ev.formats, FormatSingles):
		return validationError("this event doesn't offer singles")
	}
	return nil
}

// checkPreferredStart checks a singles slot starting at t fits inside the
// event's window.
func (ev challengeEvent) checkPreferredStart(t time.Time) error {
	latest := ev.endsAt.Add(-ev.singlesSlot)
	if t.Before(ev.startsAt) || t.After(latest) {
		loc, err := time.LoadLocation(ev.timezone)
		if err != nil {
			loc = time.UTC
		}
		return validationError("pick a start time between %s and %s",
			ev.startsAt.In(loc).Format("15:04"), latest.In(loc).Format("15:04"))
	}
	return nil
}

// checkChallengePlayers checks the rules both players must meet, from the
// point of view of me (the one acting): both inside the rating band, neither
// at the per-event entry limit with one more, and room in the event for two
// more entries and one more match. Call it with the event row locked.
func checkChallengePlayers(ctx context.Context, q rowsQuerier, ev challengeEvent, eventID, me, other string, now time.Time) error {
	ratings, err := loadRatings(ctx, q, []string{me, other})
	if err != nil {
		return err
	}
	if !inBand(ratings.of(me, FormatSingles), ev.ratingMin, ev.ratingMax) {
		return ErrOutsideBand
	}
	if !inBand(ratings.of(other, FormatSingles), ev.ratingMin, ev.ratingMax) {
		return ErrOtherOutsideBand
	}
	// Live entries: the same rule as CreateEntry and liveEntriesSQL.
	var live, mine, theirs int
	if err := q.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE user_id = $2), count(*) FILTER (WHERE user_id = $3)
		FROM entries
		WHERE event_id = $1
		  AND (status IN ('confirmed', 'standby', 'scheduled')
		       OR (status = 'pending_payment' AND hold_expires_at > $4))`,
		eventID, me, other, now,
	).Scan(&live, &mine, &theirs); err != nil {
		return fmt.Errorf("count entries: %w", err)
	}
	switch {
	case mine+1 > maxEntriesPerPlayer:
		return ErrEntryLimit
	case theirs+1 > maxEntriesPerPlayer:
		return ErrOtherEntryLimit
	case live+2 > ev.capacity:
		return ErrEventFull
	// Two locked entries share one match: two of the event's 2 × max_matches
	// half-matches.
	case live+2 > 2*ev.maxMatches:
		return ErrNoMatchesLeft
	}
	return nil
}

func (s *PostgresStore) CreateChallenge(ctx context.Context, in ChallengeInput) (Challenge, error) {
	id, err := newID("chl")
	if err != nil {
		return Challenge{}, err
	}
	var out Challenge
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// The event row lock makes the counts below agree with the joins and
		// accepts that run alongside.
		ev, err := lockChallengeEvent(ctx, tx, in.EventID)
		if err != nil {
			return err
		}
		if err := ev.open(in.Now); err != nil {
			return err
		}
		if err := ev.checkPreferredStart(in.PreferredStart); err != nil {
			return err
		}

		var opponentID, name string
		err = tx.QueryRow(ctx, `SELECT id, name FROM users WHERE lower(username) = $1`, in.OpponentUsername).
			Scan(&opponentID, &name)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNoSuchUser
		case err != nil:
			return fmt.Errorf("find opponent: %w", err)
		case opponentID == in.ChallengerID:
			return validationError("you can't challenge yourself")
		case name == "": // a username but no name: the profile step isn't done
			return ErrOtherProfileIncomplete
		}
		if err := checkChallengePlayers(ctx, tx, ev, in.EventID, in.ChallengerID, opponentID, in.Now); err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO challenges (id, event_id, format, challenger_id, opponent_id, preferred_start, expires_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, in.EventID, FormatSingles, in.ChallengerID, opponentID, in.PreferredStart.UTC(),
			challengeExpiry(in.Now, ev.registrationClosesAt).UTC(), in.Now.UTC())
		if isUniqueViolation(err) {
			return ErrChallengeExists
		}
		if err != nil {
			return fmt.Errorf("insert challenge: %w", err)
		}
		out, err = loadChallenge(ctx, tx, in.ChallengerID, id)
		return err
	})
	if err != nil {
		return Challenge{}, err
	}
	return out, nil
}

// lockedChallenge is a challenges row read with its lock held.
type lockedChallenge struct {
	id, eventID, status       string
	challengerID, opponentID  string
	preferredStart, expiresAt time.Time
}

// isOpen reports whether the challenge is still pending or accepted.
func (c lockedChallenge) isOpen() bool {
	return c.status == ChallengePending || c.status == ChallengeAccepted
}

// due reports whether the challenge is open and its deadline has passed.
func (c lockedChallenge) due(now time.Time) bool {
	return c.isOpen() && !now.Before(c.expiresAt)
}

// lockChallengeRow locks and reads a challenge. Lock its event row first.
func lockChallengeRow(ctx context.Context, q querier, challengeID string) (lockedChallenge, error) {
	c := lockedChallenge{id: challengeID}
	err := q.QueryRow(ctx, `
		SELECT event_id, status, challenger_id, opponent_id, preferred_start, expires_at
		FROM challenges WHERE id = $1 FOR UPDATE`, challengeID,
	).Scan(&c.eventID, &c.status, &c.challengerID, &c.opponentID, &c.preferredStart, &c.expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNoSuchChallenge
	}
	if err != nil {
		return c, fmt.Errorf("lock challenge: %w", err)
	}
	return c, nil
}

// lockUserChallenge locks a challenge one of whose players is userID, after
// its event row, and returns both. ErrNoSuchChallenge for anyone else.
func lockUserChallenge(ctx context.Context, tx pgx.Tx, userID, challengeID string) (lockedChallenge, challengeEvent, error) {
	var eventID string
	err := tx.QueryRow(ctx, `
		SELECT event_id FROM challenges WHERE id = $1 AND (challenger_id = $2 OR opponent_id = $2)`,
		challengeID, userID).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedChallenge{}, challengeEvent{}, ErrNoSuchChallenge
	}
	if err != nil {
		return lockedChallenge{}, challengeEvent{}, fmt.Errorf("find challenge: %w", err)
	}
	ev, err := lockChallengeEvent(ctx, tx, eventID)
	if err != nil {
		return lockedChallenge{}, challengeEvent{}, err
	}
	c, err := lockChallengeRow(ctx, tx, challengeID)
	return c, ev, err
}

// endChallenge moves an open challenge to a final status (declined,
// withdrawn, expired or failed) and releases its locked entries: unpaid ones
// are cancelled with their open orders, paid ones refunded. It returns the
// payments to refund once the transaction commits. Call it with the
// challenge locked.
func endChallenge(ctx context.Context, tx pgx.Tx, c lockedChallenge, status string, now time.Time) ([]PendingRefund, error) {
	responded := status == ChallengeDeclined
	if _, err := tx.Exec(ctx, `
		UPDATE challenges SET status = $2, responded_at = CASE WHEN $3 THEN $4 ELSE responded_at END,
			updated_at = now()
		WHERE id = $1`, c.id, status, responded, now.UTC()); err != nil {
		return nil, fmt.Errorf("end challenge: %w", err)
	}

	rows, err := tx.Query(ctx, `SELECT id, status FROM entries WHERE challenge_id = $1 ORDER BY id FOR UPDATE`, c.id)
	if err != nil {
		return nil, fmt.Errorf("lock challenge entries: %w", err)
	}
	type entryRow struct{ id, status string }
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (entryRow, error) {
		var e entryRow
		return e, row.Scan(&e.id, &e.status)
	})
	if err != nil {
		return nil, fmt.Errorf("lock challenge entries: %w", err)
	}

	var refunds []PendingRefund
	for _, e := range entries {
		next := EntryCancelled
		switch e.status {
		case EntryPendingPayment:
		case EntryConfirmed:
			// Paid: its payment goes back. A free event's entry has none and
			// is simply cancelled.
			var paymentID string
			var providerPaymentID *string
			var amount int
			err := tx.QueryRow(ctx, `
				UPDATE payments SET status = 'refund_pending', updated_at = now()
				WHERE entry_id = $1 AND status = 'paid'
				RETURNING id, provider_payment_id, amount_paise`, e.id).Scan(&paymentID, &providerPaymentID, &amount)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return nil, fmt.Errorf("refund payment: %w", err)
			default:
				next = EntryRefunded
				// Locked entries are only paid online (the desk can't mark
				// them paid), so there's always a provider payment to refund.
				if providerPaymentID != nil {
					refunds = append(refunds, PendingRefund{PaymentID: paymentID, ProviderPaymentID: *providerPaymentID, AmountPaise: amount})
				}
			}
		default: // already released, or scheduled (never ended)
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE entries SET status = $2, updated_at = now() WHERE id = $1`, e.id, next); err != nil {
			return nil, fmt.Errorf("release entry: %w", err)
		}
		// Nobody will pay its open order now; one paid anyway is refunded
		// by ConfirmPayment.
		if _, err := tx.Exec(ctx, `
			UPDATE payments SET status = 'failed', updated_at = now() WHERE entry_id = $1 AND status = 'created'`,
			e.id); err != nil {
			return nil, fmt.Errorf("close open order: %w", err)
		}
	}
	return refunds, nil
}

// expireIfDue expires the challenge if its deadline has passed, in its own
// transaction, and returns the refunds to make. Anything else (unknown, not
// due) is left alone without an error.
func (s *PostgresStore) expireIfDue(ctx context.Context, challengeID string, now time.Time) (bool, []PendingRefund, error) {
	// Most reads find nothing due, so look before taking any lock.
	var eventID string
	err := s.pool.QueryRow(ctx, `
		SELECT event_id FROM challenges
		WHERE id = $1 AND status IN ('pending', 'accepted') AND expires_at <= $2`, challengeID, now).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("find due challenge: %w", err)
	}
	var expired bool
	var refunds []PendingRefund
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := lockChallengeEvent(ctx, tx, eventID); err != nil {
			return err
		}
		c, err := lockChallengeRow(ctx, tx, challengeID)
		if err != nil {
			return err
		}
		if !c.due(now) { // both paid, or someone acted, meanwhile
			return nil
		}
		refunds, err = endChallenge(ctx, tx, c, ChallengeExpired, now)
		expired = err == nil
		return err
	})
	if err != nil {
		return false, nil, err
	}
	return expired, refunds, nil
}

func (s *PostgresStore) ExpireChallenges(ctx context.Context, now time.Time, userID string) (int, []PendingRefund, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM challenges
		WHERE status IN ('pending', 'accepted') AND expires_at <= $1
		  AND ($2 = '' OR challenger_id = $2 OR opponent_id = $2)
		ORDER BY expires_at, id`, now, userID)
	if err != nil {
		return 0, nil, fmt.Errorf("find due challenges: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, nil, fmt.Errorf("find due challenges: %w", err)
	}
	count := 0
	var refunds []PendingRefund
	for _, id := range ids {
		expired, more, err := s.expireIfDue(ctx, id, now)
		// Refunds already committed are returned even when a later one fails.
		refunds = append(refunds, more...)
		if err != nil {
			return count, refunds, err
		}
		if expired {
			count++
		}
	}
	return count, refunds, nil
}

func (s *PostgresStore) GetChallenge(ctx context.Context, userID, challengeID string, now time.Time) (ChallengeOutcome, error) {
	var out ChallengeOutcome
	var err error
	if _, out.Refunds, err = s.expireIfDue(ctx, challengeID, now); err != nil {
		return out, err
	}
	out.Challenge, err = loadChallenge(ctx, s.pool, userID, challengeID)
	return out, err
}

func (s *PostgresStore) ListUserChallenges(ctx context.Context, userID string) ([]Challenge, error) {
	rows, err := s.pool.Query(ctx, challengeSelect+`
		ORDER BY ch.status IN ('pending', 'accepted') DESC, ch.created_at DESC, ch.id DESC
		LIMIT $3`, userID, startingRating, userChallengesLimit)
	if err != nil {
		return nil, fmt.Errorf("list challenges: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Challenge, error) { return scanChallenge(row, userID) })
	if err != nil {
		return nil, fmt.Errorf("list challenges: %w", err)
	}
	if out == nil {
		out = []Challenge{}
	}
	return out, nil
}

// actOnChallenge runs an answer or a withdrawal: it expires the challenge
// first if it's due (committing that on its own), then runs act with the
// event and challenge locked, and returns the challenge as userID sees it.
func (s *PostgresStore) actOnChallenge(ctx context.Context, userID, challengeID string, now time.Time,
	act func(tx pgx.Tx, c lockedChallenge, ev challengeEvent) ([]PendingRefund, error)) (ChallengeOutcome, error) {
	var out ChallengeOutcome
	_, refunds, err := s.expireIfDue(ctx, challengeID, now)
	out.Refunds = refunds
	if err != nil {
		return out, err
	}
	var more []PendingRefund
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		c, ev, err := lockUserChallenge(ctx, tx, userID, challengeID)
		if err != nil {
			return err
		}
		if c.due(now) || c.status == ChallengeExpired {
			return ErrChallengeExpired
		}
		if more, err = act(tx, c, ev); err != nil {
			return err
		}
		out.Challenge, err = loadChallenge(ctx, tx, userID, challengeID)
		return err
	})
	if err != nil {
		return out, err
	}
	// Only refunds the transaction committed.
	out.Refunds = append(out.Refunds, more...)
	return out, nil
}

func (s *PostgresStore) AcceptChallenge(ctx context.Context, userID, challengeID string, now time.Time) (ChallengeOutcome, error) {
	return s.actOnChallenge(ctx, userID, challengeID, now, func(tx pgx.Tx, c lockedChallenge, ev challengeEvent) ([]PendingRefund, error) {
		switch {
		case c.opponentID != userID:
			return nil, ErrNotChallengeOpponent
		case c.status != ChallengePending:
			return nil, ErrChallengeNotPending
		}
		// Everything checked when it was sent, again: time has passed.
		if err := ev.open(now); err != nil {
			return nil, err
		}
		var challengerName string
		if err := tx.QueryRow(ctx, `SELECT name FROM users WHERE id = $1 AND username IS NOT NULL`, c.challengerID).
			Scan(&challengerName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("load challenger: %w", err)
		}
		if challengerName == "" {
			return nil, ErrOtherProfileIncomplete
		}
		if err := checkChallengePlayers(ctx, tx, ev, c.eventID, userID, c.challengerID, now); err != nil {
			return nil, err
		}

		// A locked entry each, held until the challenge's deadline: they
		// count towards the event from now on.
		for _, player := range []string{c.challengerID, c.opponentID} {
			id, err := newID("ent")
			if err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO entries (id, event_id, user_id, kind, formats, source, status, hold_expires_at, challenge_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				id, c.eventID, player, EntryLocked, []string{FormatSingles}, SourceOnline, EntryPendingPayment,
				c.expiresAt.UTC(), c.id); err != nil {
				return nil, fmt.Errorf("insert locked entry: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE challenges SET status = 'accepted', responded_at = $2, updated_at = now() WHERE id = $1`,
			c.id, now.UTC()); err != nil {
			return nil, fmt.Errorf("accept challenge: %w", err)
		}
		return nil, nil
	})
}

func (s *PostgresStore) DeclineChallenge(ctx context.Context, userID, challengeID string, now time.Time) (ChallengeOutcome, error) {
	return s.actOnChallenge(ctx, userID, challengeID, now, func(tx pgx.Tx, c lockedChallenge, _ challengeEvent) ([]PendingRefund, error) {
		switch {
		case c.opponentID != userID:
			return nil, ErrNotChallengeOpponent
		case c.status != ChallengePending:
			return nil, ErrChallengeNotPending
		}
		return endChallenge(ctx, tx, c, ChallengeDeclined, now)
	})
}

func (s *PostgresStore) WithdrawChallenge(ctx context.Context, userID, challengeID string, now time.Time) (ChallengeOutcome, error) {
	return s.actOnChallenge(ctx, userID, challengeID, now, func(tx pgx.Tx, c lockedChallenge, _ challengeEvent) ([]PendingRefund, error) {
		// Either player can pull out before both have paid (section 12);
		// before accepting, the opponent declines instead.
		canWithdraw := c.status == ChallengeAccepted ||
			(c.status == ChallengePending && c.challengerID == userID)
		if !canWithdraw {
			return nil, ErrChallengeNotWithdrawable
		}
		return endChallenge(ctx, tx, c, ChallengeWithdrawn, now)
	})
}

// --- fixing the match once both have paid ---

// loadFixedMatches returns the matches already taking time the event could
// use: the event's own that aren't cancelled, and other events' on its
// courts overlapping its window (OtherEvent).
func loadFixedMatches(ctx context.Context, q rowsQuerier, eventID string, startsAt, endsAt time.Time) ([]FixedMatch, error) {
	rows, err := q.Query(ctx, `
		SELECT m.court_id, m.starts_at, m.ends_at, COALESCE(m.judge_id, ''), m.event_id IS DISTINCT FROM $1,
			COALESCE(array_agg(ms.user_id ORDER BY ms.user_id) FILTER (WHERE ms.user_id IS NOT NULL), '{}')
		FROM matches m
		LEFT JOIN match_sides ms ON ms.match_id = m.id
		WHERE m.status <> 'cancelled'
		  AND (m.event_id = $1
		       OR (m.court_id IN (SELECT court_id FROM event_courts WHERE event_id = $1)
		           AND m.starts_at < $3 AND m.ends_at > $2))
		GROUP BY m.id
		ORDER BY m.starts_at, m.id`, eventID, startsAt, endsAt)
	if err != nil {
		return nil, fmt.Errorf("load fixed matches: %w", err)
	}
	fixed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FixedMatch, error) {
		var f FixedMatch
		err := row.Scan(&f.CourtID, &f.StartsAt, &f.EndsAt, &f.JudgeID, &f.OtherEvent, &f.UserIDs)
		f.StartsAt, f.EndsAt = f.StartsAt.UTC(), f.EndsAt.UTC()
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("load fixed matches: %w", err)
	}
	return fixed, nil
}

// fixChallengeMatch fixes the challenge's match if it's accepted and both
// locked entries are now paid (confirmed): the slot findChallengeSlot picks,
// saved as a match with both entries scheduled into it and the challenge
// scheduled. With no slot left the challenge fails and both entries are
// refunded; the payments to refund are returned. Otherwise it does nothing.
//
// ConfirmPayment calls it in the transaction that confirms an entry, with
// the event, the challenge and that entry locked. Payments for one event
// take turns on the event row, so when both players' payments land at once
// only the second sees both confirmed, and exactly one match is made.
func fixChallengeMatch(ctx context.Context, tx pgx.Tx, challengeID string, now time.Time) ([]PendingRefund, error) {
	c, err := lockChallengeRow(ctx, tx, challengeID)
	if err != nil {
		return nil, err
	}
	if c.status != ChallengeAccepted {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id, user_id, status FROM entries WHERE challenge_id = $1 ORDER BY id FOR UPDATE`, c.id)
	if err != nil {
		return nil, fmt.Errorf("lock challenge entries: %w", err)
	}
	entryOf := map[string]string{}
	paid := 0
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			var id, userID, status string
			if err := rows.Scan(&id, &userID, &status); err != nil {
				return err
			}
			entryOf[userID] = id
			if status == EntryConfirmed {
				paid++
			}
		}
		return rows.Err()
	}()
	if err != nil {
		return nil, fmt.Errorf("lock challenge entries: %w", err)
	}
	if paid < 2 {
		return nil, nil // the other player hasn't paid yet
	}

	req := SlotRequest{Preferred: c.preferredStart.UTC(), PlayerIDs: []string{c.challengerID, c.opponentID}}
	var slot int
	if err := tx.QueryRow(ctx, `SELECT starts_at, ends_at, singles_slot_minutes FROM events WHERE id = $1`,
		c.eventID).Scan(&req.StartsAt, &req.EndsAt, &slot); err != nil {
		return nil, fmt.Errorf("load event: %w", err)
	}
	req.StartsAt, req.EndsAt, req.Length = req.StartsAt.UTC(), req.EndsAt.UTC(), time.Duration(slot)*time.Minute
	rows, err = tx.Query(ctx, `
		SELECT c.id, c.label FROM event_courts ec JOIN courts c ON c.id = ec.court_id
		WHERE ec.event_id = $1 ORDER BY length(c.label), c.label`, c.eventID)
	if err != nil {
		return nil, fmt.Errorf("event courts: %w", err)
	}
	if req.Courts, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Court]); err != nil {
		return nil, fmt.Errorf("event courts: %w", err)
	}
	rows, err = tx.Query(ctx, `SELECT user_id FROM event_judges WHERE event_id = $1 ORDER BY user_id`, c.eventID)
	if err != nil {
		return nil, fmt.Errorf("event judges: %w", err)
	}
	if req.JudgeIDs, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return nil, fmt.Errorf("event judges: %w", err)
	}
	if req.Taken, err = loadFixedMatches(ctx, tx, c.eventID, req.StartsAt, req.EndsAt); err != nil {
		return nil, err
	}

	choice, ok := findChallengeSlot(req)
	if !ok {
		// No slot left: the challenge fails and both are refunded (section
		// 12). It's never turned into open entries.
		return endChallenge(ctx, tx, c, ChallengeFailed, now)
	}

	matchID, err := newID("mat")
	if err != nil {
		return nil, err
	}
	var judge any // nil: no judge free; the admin is asked to assign one
	if choice.JudgeID != "" {
		judge = choice.JudgeID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO matches (id, event_id, format, court_id, starts_at, ends_at, judge_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		matchID, c.eventID, FormatSingles, choice.CourtID, choice.StartsAt, choice.EndsAt, judge); err != nil {
		return nil, fmt.Errorf("insert match: %w", err)
	}
	// The challenger is side A.
	for i, player := range []string{c.challengerID, c.opponentID} {
		if _, err := tx.Exec(ctx,
			`INSERT INTO match_sides (match_id, side, user_id, entry_id) VALUES ($1, $2, $3, $4)`,
			matchID, sideLabel(i), player, entryOf[player]); err != nil {
			return nil, fmt.Errorf("insert match side: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE entries SET status = 'scheduled', match_id = $2, updated_at = now() WHERE id = $1`,
			entryOf[player], matchID); err != nil {
			return nil, fmt.Errorf("schedule entry: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE challenges SET status = 'scheduled', match_id = $2, updated_at = now() WHERE id = $1`,
		c.id, matchID); err != nil {
		return nil, fmt.Errorf("schedule challenge: %w", err)
	}
	// TODO(step 11, notifications): tell both players the venue, court,
	// time and judge (section 12). Until then they see it in the challenge
	// and in GET /api/me/matches.
	return nil, nil
}

// --- players to challenge ---

// playerColumns is a users row (u) as a Player, with its singles rating (r,
// left-joined), defaulting to $2 (startingRating).
const playerColumns = `u.id, COALESCE(u.username, ''), u.name, COALESCE(r.rating, $2)`

func collectPlayers(rows pgx.Rows) ([]Player, error) {
	players, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Player])
	if err != nil {
		return nil, err
	}
	if players == nil {
		players = []Player{}
	}
	return players, nil
}

func (s *PostgresStore) RecentPlayers(ctx context.Context, userID string) ([]Player, error) {
	// Matches count from their start, challenges from when they were sent.
	rows, err := s.pool.Query(ctx, `
		WITH met AS (
			SELECT other.user_id AS other_id, m.starts_at AS at
			FROM match_sides mine
			JOIN match_sides other ON other.match_id = mine.match_id AND other.user_id <> mine.user_id
			JOIN matches m ON m.id = mine.match_id
			WHERE mine.user_id = $1 AND m.status <> 'cancelled'
			UNION ALL
			SELECT CASE WHEN challenger_id = $1 THEN opponent_id ELSE challenger_id END, created_at
			FROM challenges WHERE challenger_id = $1 OR opponent_id = $1
		), latest AS (
			SELECT other_id, max(at) AS at FROM met GROUP BY other_id
		)
		SELECT `+playerColumns+`
		FROM latest
		JOIN users u ON u.id = latest.other_id
		LEFT JOIN player_ratings r ON r.user_id = u.id AND r.discipline = 'singles'
		WHERE u.username IS NOT NULL AND u.name <> ''
		ORDER BY latest.at DESC, u.id
		LIMIT $3`, userID, startingRating, playerSuggestionsLimit)
	if err != nil {
		return nil, fmt.Errorf("recent players: %w", err)
	}
	players, err := collectPlayers(rows)
	if err != nil {
		return nil, fmt.Errorf("recent players: %w", err)
	}
	return players, nil
}

func (s *PostgresStore) NearbyPlayers(ctx context.Context, userID string, near GeoPoint, radiusKm float64, now time.Time) ([]Player, error) {
	rows, err := s.pool.Query(ctx, `
		WITH me AS (
			SELECT COALESCE((SELECT rating FROM player_ratings WHERE user_id = $1 AND discipline = 'singles'), $2) AS rating
		), regulars AS (
			SELECT en.user_id, max(en.created_at) AS last_entry
			FROM entries en
			JOIN events e ON e.id = en.event_id
			JOIN venues v ON v.id = e.venue_id
			WHERE en.status <> 'cancelled' AND en.created_at > $4 AND en.user_id <> $1
			  AND v.lat IS NOT NULL AND `+distanceFrom("$5", "$6")+` <= $7
			GROUP BY en.user_id
		)
		SELECT `+playerColumns+`
		FROM regulars
		JOIN users u ON u.id = regulars.user_id
		LEFT JOIN player_ratings r ON r.user_id = u.id AND r.discipline = 'singles'
		CROSS JOIN me
		WHERE u.username IS NOT NULL AND u.name <> ''
		ORDER BY abs(COALESCE(r.rating, $2) - me.rating), regulars.last_entry DESC, u.id
		LIMIT $3`, userID, startingRating, playerSuggestionsLimit, now.Add(-nearbyPlayerWindow),
		near.Lat, near.Lng, radiusKm)
	if err != nil {
		return nil, fmt.Errorf("nearby players: %w", err)
	}
	players, err := collectPlayers(rows)
	if err != nil {
		return nil, fmt.Errorf("nearby players: %w", err)
	}
	return players, nil
}
