package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// entryColumns is an entries row (en) with its partner (p, left-joined), its
// event (e) and the event's venue (v), as entryFrom joins them. scanEntry
// reads them in this order.
const entryColumns = `en.id, en.kind, en.challenge_id, en.formats, en.source, en.status, en.hold_expires_at,
	(en.status = 'pending_payment' AND en.hold_expires_at <= now()), en.created_at,
	p.id, COALESCE(p.username, ''), COALESCE(p.name, ''),
	e.id, e.title, e.status, e.starts_at, e.ends_at, e.registration_closes_at, e.fee_paise, e.currency,
	` + venueColumns

const entryFrom = `FROM entries en
	JOIN events e ON e.id = en.event_id
	JOIN venues v ON v.id = e.venue_id
	LEFT JOIN users p ON p.id = en.partner_user_id`

// userEntriesLimit caps a player's entry list until it needs paging.
const userEntriesLimit = 100

func scanEntry(row pgx.Row) (Entry, error) {
	var en Entry
	var partnerID *string
	var partner PublicUser
	ev := &en.Event
	err := row.Scan(&en.ID, &en.Kind, &en.ChallengeID, &en.Formats, &en.Source, &en.Status, &en.HoldExpiresAt,
		&en.HoldExpired, &en.CreatedAt,
		&partnerID, &partner.Username, &partner.Name,
		&ev.ID, &ev.Title, &ev.Status, &ev.StartsAt, &ev.EndsAt, &ev.RegistrationClosesAt, &ev.FeePaise, &ev.Currency,
		&ev.Venue.ID, &ev.Venue.Name, &ev.Venue.Address, &ev.Venue.City, &ev.Venue.Lat, &ev.Venue.Lng, &ev.Venue.Timezone)
	if err != nil {
		return Entry{}, err
	}
	if partnerID != nil {
		partner.ID = *partnerID
		en.Partner = &partner
	}
	if en.HoldExpiresAt != nil {
		utc := en.HoldExpiresAt.UTC()
		en.HoldExpiresAt = &utc
	}
	en.CreatedAt = en.CreatedAt.UTC()
	ev.StartsAt, ev.EndsAt, ev.RegistrationClosesAt = ev.StartsAt.UTC(), ev.EndsAt.UTC(), ev.RegistrationClosesAt.UTC()
	return en, nil
}

// loadEntry returns the user's entry, or ErrNoSuchEntry.
func loadEntry(ctx context.Context, q querier, userID, entryID string) (Entry, error) {
	en, err := scanEntry(q.QueryRow(ctx,
		`SELECT `+entryColumns+` `+entryFrom+` WHERE en.id = $1 AND en.user_id = $2`, entryID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrNoSuchEntry
	}
	if err != nil {
		return Entry{}, fmt.Errorf("load entry: %w", err)
	}
	return en, nil
}

func (s *PostgresStore) CreateEntry(ctx context.Context, in EntryInput) (Entry, error) {
	id, err := newID("ent")
	if err != nil {
		return Entry{}, err
	}
	var entry Entry
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Locking the event row makes joins to one event take turns: each sees
		// the entries committed before it, so the counts below can't go stale
		// before the insert. Joins to other events don't wait.
		var ev Event
		err := tx.QueryRow(ctx, `
			SELECT status, formats, registration_closes_at, capacity, max_matches, rating_min, rating_max
			FROM events WHERE id = $1 FOR UPDATE`, in.EventID,
		).Scan(&ev.Status, &ev.Formats, &ev.RegistrationClosesAt, &ev.Capacity, &ev.MaxMatches, &ev.RatingMin, &ev.RatingMax)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchEvent
		}
		if err != nil {
			return fmt.Errorf("lock event: %w", err)
		}
		switch {
		case ev.Status == EventDraft: // invisible to players
			return ErrNoSuchEvent
		case ev.Status != EventPublished:
			return ErrEventNotOpen
		case !in.Now.Before(ev.RegistrationClosesAt):
			return ErrRegistrationClosed
		}
		for _, f := range in.Formats {
			if !slices.Contains(ev.Formats, f) {
				return validationError("this event doesn't offer %s", f)
			}
		}

		var partnerID any // nil: no partner
		if in.PartnerUsername != "" {
			var pid, name string
			err := tx.QueryRow(ctx, `SELECT id, name FROM users WHERE lower(username) = $1`, in.PartnerUsername).Scan(&pid, &name)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return ErrNoSuchUser
			case err != nil:
				return fmt.Errorf("find partner: %w", err)
			case pid == in.UserID:
				return validationError("you can't be your own partner")
			case name == "": // a username but no name: the profile step isn't done
				return validationError("@%s hasn't finished setting up their profile", in.PartnerUsername)
			}
			// Every player pays for every match, so a partner must hold their
			// own live entry here, for one of the team formats chosen.
			var teamFormats []string
			for _, f := range in.Formats {
				if f == FormatDoubles || f == FormatMixed {
					teamFormats = append(teamFormats, f)
				}
			}
			var joined bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM entries
					WHERE event_id = $1 AND user_id = $2 AND formats && $3
					  AND (status IN ('confirmed', 'standby', 'scheduled')
					       OR (status = 'pending_payment' AND hold_expires_at > $4)))`,
				in.EventID, pid, teamFormats, in.Now,
			).Scan(&joined); err != nil {
				return fmt.Errorf("check partner entry: %w", err)
			}
			if !joined {
				return validationError("@%s needs to join this event for %s first", in.PartnerUsername, strings.Join(teamFormats, " or "))
			}
			partnerID = pid
		}

		ratings, err := bandRatings(ctx, tx, in.UserID, in.Formats)
		if err != nil {
			return err
		}
		for _, rating := range ratings {
			if !inBand(rating, ev.RatingMin, ev.RatingMax) {
				return ErrOutsideBand
			}
		}

		// Live entries: the same rule as liveEntriesSQL, against in.Now. An
		// expired hold simply stops counting.
		var live, mine int
		if err := tx.QueryRow(ctx, `
			SELECT count(*), count(*) FILTER (WHERE user_id = $2)
			FROM entries
			WHERE event_id = $1
			  AND (status IN ('confirmed', 'standby', 'scheduled')
			       OR (status = 'pending_payment' AND hold_expires_at > $3))`,
			in.EventID, in.UserID, in.Now,
		).Scan(&live, &mine); err != nil {
			return fmt.Errorf("count entries: %w", err)
		}
		switch {
		case mine >= maxEntriesPerPlayer:
			return ErrEntryLimit
		case live >= ev.Capacity:
			return ErrEventFull
		// Each entry needs at most half a match: an open one is half a
		// singles match (less in doubles), and a challenge's two locked
		// entries share one. So live entries, this one included, can't
		// exceed twice max_matches.
		case live+1 > 2*ev.MaxMatches:
			return ErrNoMatchesLeft
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO entries (id, event_id, user_id, kind, formats, partner_user_id, source, status, hold_expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			id, in.EventID, in.UserID, EntryOpen, in.Formats, partnerID, SourceOnline, EntryPendingPayment,
			in.Now.Add(entryHold).UTC(),
		); err != nil {
			return fmt.Errorf("insert entry: %w", err)
		}
		entry, err = loadEntry(ctx, tx, in.UserID, id)
		return err
	})
	if err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *PostgresStore) ListUserEntries(ctx context.Context, userID string) ([]Entry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+entryColumns+` `+entryFrom+`
		WHERE en.user_id = $1
		ORDER BY en.created_at DESC, en.id DESC
		LIMIT $2`, userID, userEntriesLimit)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Entry, error) { return scanEntry(row) })
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	if entries == nil {
		entries = []Entry{}
	}
	return entries, nil
}

func (s *PostgresStore) CancelEntry(ctx context.Context, userID, entryID string) (Entry, error) {
	var entry Entry
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Locks the entry so a payment confirming it (ConfirmPayment) can't
		// land between the check and the update; a payment that lands after
		// it is refunded.
		var status, kind string
		err := tx.QueryRow(ctx, `SELECT status, kind FROM entries WHERE id = $1 AND user_id = $2 FOR UPDATE`,
			entryID, userID).Scan(&status, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchEntry
		}
		if err != nil {
			return fmt.Errorf("lock entry: %w", err)
		}
		// A challenge's entries come and go with the challenge: withdrawing
		// or declining it releases both.
		if kind == EntryLocked {
			return ErrEntryInChallenge
		}
		// Only unpaid entries for now: cancelling a paid one means a refund,
		// which waits for payments and the refund policy (open question 2).
		if status != EntryPendingPayment {
			return ErrEntryNotCancellable
		}
		if _, err := tx.Exec(ctx, `UPDATE entries SET status = $2, updated_at = now() WHERE id = $1`,
			entryID, EntryCancelled); err != nil {
			return fmt.Errorf("cancel entry: %w", err)
		}
		// Nobody will pay its open order now.
		if _, err := tx.Exec(ctx, `
			UPDATE payments SET status = 'failed', updated_at = now() WHERE entry_id = $1 AND status = 'created'`,
			entryID); err != nil {
			return fmt.Errorf("close open order: %w", err)
		}
		entry, err = loadEntry(ctx, tx, userID, entryID)
		return err
	})
	if err != nil {
		return Entry{}, err
	}
	return entry, nil
}
