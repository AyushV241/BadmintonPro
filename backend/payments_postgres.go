package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const onlinePaymentColumns = `id, entry_id, user_id, provider, provider_order_id, amount_paise, currency, status`

func scanOnlinePayment(row pgx.Row) (OnlinePayment, error) {
	var p OnlinePayment
	err := row.Scan(&p.ID, &p.EntryID, &p.UserID, &p.Provider, &p.OrderID, &p.AmountPaise, &p.Currency, &p.Status)
	return p, err
}

// checkPayable says whether an entry can be paid for at now: waiting for
// payment, its hold live, its event published.
func checkPayable(en Entry, now time.Time) error {
	switch {
	case en.Status != EntryPendingPayment:
		return ErrEntryNotPending
	case en.Event.Status != EventPublished:
		return ErrEventNotOpen
	case en.HoldExpiresAt == nil || !en.HoldExpiresAt.After(now):
		return ErrHoldExpired
	}
	return nil
}

// openOrder returns the entry's created order with this provider, amount and
// currency, or nil.
func openOrder(ctx context.Context, q querier, entryID, provider string, amount int, currency string) (*OnlinePayment, error) {
	p, err := scanOnlinePayment(q.QueryRow(ctx, `
		SELECT `+onlinePaymentColumns+` FROM payments
		WHERE entry_id = $1 AND status = 'created' AND provider = $2 AND amount_paise = $3 AND currency = $4`,
		entryID, provider, amount, currency))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find open order: %w", err)
	}
	return &p, nil
}

func (s *PostgresStore) PayableEntry(ctx context.Context, userID, entryID, provider string, now time.Time) (PayableEntry, error) {
	en, err := loadEntry(ctx, s.pool, userID, entryID)
	if err != nil {
		return PayableEntry{}, err
	}
	if err := checkPayable(en, now); err != nil {
		return PayableEntry{}, err
	}
	out := PayableEntry{Entry: en}
	if provider != "" && en.Event.FeePaise > 0 {
		if out.Order, err = openOrder(ctx, s.pool, entryID, provider, en.Event.FeePaise, en.Event.Currency); err != nil {
			return PayableEntry{}, err
		}
	}
	return out, nil
}

func (s *PostgresStore) SaveOrder(ctx context.Context, o NewOrder) (OnlinePayment, error) {
	id, err := newID("pmt")
	if err != nil {
		return OnlinePayment{}, err
	}
	var saved OnlinePayment
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Locking the entry makes two pay requests for it take turns, and
		// keeps a cancel from landing between the check and the insert.
		var locked string
		err := tx.QueryRow(ctx, `SELECT id FROM entries WHERE id = $1 AND user_id = $2 FOR UPDATE`,
			o.EntryID, o.UserID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchEntry
		}
		if err != nil {
			return fmt.Errorf("lock entry: %w", err)
		}
		en, err := loadEntry(ctx, tx, o.UserID, o.EntryID)
		if err != nil {
			return err
		}
		if err := checkPayable(en, o.Now); err != nil {
			return err
		}
		// Another request saved an order first: use that one.
		existing, err := openOrder(ctx, tx, o.EntryID, o.Provider, o.AmountPaise, o.Currency)
		if err != nil {
			return err
		}
		if existing != nil {
			saved = *existing
			return nil
		}
		// An order for an old fee or another provider won't be offered
		// again. If it is paid anyway (an old tab), ConfirmPayment still
		// applies or refunds the payment.
		if _, err := tx.Exec(ctx, `
			UPDATE payments SET status = 'failed', updated_at = now()
			WHERE entry_id = $1 AND status = 'created'`, o.EntryID); err != nil {
			return fmt.Errorf("retire old orders: %w", err)
		}
		saved, err = scanOnlinePayment(tx.QueryRow(ctx, `
			INSERT INTO payments (id, entry_id, user_id, provider, kind, provider_order_id, amount_paise, currency, status)
			VALUES ($1, $2, $3, $4, 'online', $5, $6, $7, 'created')
			RETURNING `+onlinePaymentColumns,
			id, o.EntryID, o.UserID, o.Provider, o.OrderID, o.AmountPaise, o.Currency))
		if err != nil {
			return fmt.Errorf("save order: %w", err)
		}
		return nil
	})
	if err != nil {
		return OnlinePayment{}, err
	}
	return saved, nil
}

func (s *PostgresStore) OnlinePayment(ctx context.Context, provider, orderID string) (OnlinePayment, error) {
	p, err := scanOnlinePayment(s.pool.QueryRow(ctx, `
		SELECT `+onlinePaymentColumns+` FROM payments WHERE provider = $1 AND provider_order_id = $2`,
		provider, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return OnlinePayment{}, ErrNoSuchPayment
	}
	if err != nil {
		return OnlinePayment{}, fmt.Errorf("load payment: %w", err)
	}
	return p, nil
}

func (s *PostgresStore) ConfirmPayment(ctx context.Context, c PaymentConfirmation) (PaymentOutcome, error) {
	free := c.OrderID == ""
	var out PaymentOutcome
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Find the entry and its event, without locks yet.
		entryID, userID := c.EntryID, c.UserID
		var eventID string
		if free {
			err := tx.QueryRow(ctx, `SELECT event_id FROM entries WHERE id = $1 AND user_id = $2`,
				entryID, userID).Scan(&eventID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoSuchEntry
			}
			if err != nil {
				return fmt.Errorf("find entry: %w", err)
			}
		} else {
			err := tx.QueryRow(ctx, `
				SELECT p.entry_id, p.user_id, en.event_id
				FROM payments p JOIN entries en ON en.id = p.entry_id
				WHERE p.provider = $1 AND p.provider_order_id = $2`, c.Provider, c.OrderID,
			).Scan(&entryID, &userID, &eventID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoSuchPayment
			}
			if err != nil {
				return fmt.Errorf("find payment: %w", err)
			}
		}

		// The event row, then the entry, then the payment: the order
		// CreateEntry, MarkEntryPaid and PublishSchedule lock in, so the room
		// check can't go stale, a verify and a webhook for one payment take
		// turns, and nothing deadlocks.
		var evStatus, evCurrency string
		var endsAt time.Time
		var capacity, maxMatches, fee int
		if err := tx.QueryRow(ctx, `
			SELECT status, ends_at, capacity, max_matches, fee_paise, currency FROM events WHERE id = $1 FOR UPDATE`,
			eventID).Scan(&evStatus, &endsAt, &capacity, &maxMatches, &fee, &evCurrency); err != nil {
			return fmt.Errorf("lock event: %w", err)
		}
		var entryStatus string
		var holdExpiresAt *time.Time
		if err := tx.QueryRow(ctx, `SELECT status, hold_expires_at FROM entries WHERE id = $1 FOR UPDATE`,
			entryID).Scan(&entryStatus, &holdExpiresAt); err != nil {
			return fmt.Errorf("lock entry: %w", err)
		}
		holdLive := entryStatus == EntryPendingPayment && holdExpiresAt != nil && holdExpiresAt.After(c.Now)

		confirmEntry := func() error {
			if _, err := tx.Exec(ctx, `
				UPDATE entries SET status = 'confirmed', confirmed_at = $2, updated_at = now() WHERE id = $1`,
				entryID, c.Now.UTC()); err != nil {
				return fmt.Errorf("confirm entry: %w", err)
			}
			return nil
		}
		finish := func() error {
			var err error
			out.Entry, err = loadEntry(ctx, tx, userID, entryID)
			return err
		}

		if free {
			switch {
			case entryStatus != EntryPendingPayment:
				return ErrEntryNotPending
			case evStatus != EventPublished:
				return ErrEventNotOpen
			case !holdLive:
				return ErrHoldExpired
			case fee != 0: // the fee changed since the caller looked
				return ErrPaymentMismatch
			}
			if err := confirmEntry(); err != nil {
				return err
			}
			return finish()
		}

		var paymentID, paymentStatus, currency string
		var providerPaymentID *string
		var amount int
		if err := tx.QueryRow(ctx, `
			SELECT id, status, provider_payment_id, amount_paise, currency
			FROM payments WHERE provider = $1 AND provider_order_id = $2 FOR UPDATE`, c.Provider, c.OrderID,
		).Scan(&paymentID, &paymentStatus, &providerPaymentID, &amount, &currency); err != nil {
			return fmt.Errorf("lock payment: %w", err)
		}
		if c.AmountPaise != amount || c.Currency != currency {
			return ErrPaymentMismatch
		}

		// Already applied, on this order's row or as a second payment's own
		// row: a repeated webhook, or verify and the webhook both arriving.
		var seen bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM payments WHERE provider = $1 AND provider_payment_id = $2)`,
			c.Provider, c.PaymentID).Scan(&seen); err != nil {
			return fmt.Errorf("check payment: %w", err)
		}
		if seen {
			out.Repeat = true
			return finish()
		}

		refund := func(rowID string) {
			out.Refund = &PendingRefund{PaymentID: rowID, ProviderPaymentID: c.PaymentID, AmountPaise: c.AmountPaise}
		}
		setPayment := func(status string) error {
			if _, err := tx.Exec(ctx, `
				UPDATE payments SET status = $2, provider_payment_id = $3, paid_at = $4, updated_at = now()
				WHERE id = $1`, paymentID, status, c.PaymentID, c.Now.UTC()); err != nil {
				return fmt.Errorf("record payment: %w", err)
			}
			return nil
		}

		// A different payment already paid this order. Providers normally
		// refuse that, but if one gets through it gets its own row, without
		// the order (provider_order_id is unique), and is refunded.
		if providerPaymentID != nil {
			id, err := newID("pmt")
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO payments (id, entry_id, user_id, provider, kind, provider_payment_id,
					amount_paise, currency, status, paid_at)
				VALUES ($1, $2, $3, $4, 'online', $5, $6, $7, 'refund_pending', $8)`,
				id, entryID, userID, c.Provider, c.PaymentID, c.AmountPaise, c.Currency, c.Now.UTC()); err != nil {
				return fmt.Errorf("record second payment: %w", err)
			}
			refund(id)
			return finish()
		}

		// Is the entry already paid for, online or at the desk? A confirmed
		// entry with no payment (demo data) counts too: only an unpaid entry,
		// or one cancelled while its player paid, can take this payment.
		var paidElsewhere bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM payments WHERE entry_id = $1 AND status = 'paid')`,
			entryID).Scan(&paidElsewhere); err != nil {
			return fmt.Errorf("check entry payments: %w", err)
		}
		if paidElsewhere || (entryStatus != EntryPendingPayment && entryStatus != EntryCancelled) {
			if err := setPayment(PaymentRefundPending); err != nil {
				return err
			}
			refund(paymentID)
			return finish()
		}

		// Can the entry still be confirmed? A live hold already counts
		// towards the event; an expired one has to find room again, as at
		// the desk.
		usable := entryStatus == EntryPendingPayment && evStatus == EventPublished && c.Now.Before(endsAt)
		if usable && !holdLive {
			err := checkRoom(ctx, tx, eventID, userID, entryID, capacity, maxMatches, c.Now)
			switch {
			case isNoRoom(err):
				usable = false
			case err != nil:
				return err
			}
		}
		if usable {
			if err := setPayment(PaymentPaid); err != nil {
				return err
			}
			if err := confirmEntry(); err != nil {
				return err
			}
			return finish()
		}

		// No room, the event called off or over, or the player cancelled
		// meanwhile: the entry is refunded, and so is the payment.
		if err := setPayment(PaymentRefundPending); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE entries SET status = 'refunded', updated_at = now() WHERE id = $1`,
			entryID); err != nil {
			return fmt.Errorf("refund entry: %w", err)
		}
		refund(paymentID)
		return finish()
	})
	if err != nil {
		return PaymentOutcome{}, err
	}
	return out, nil
}

func (s *PostgresStore) FinishRefund(ctx context.Context, paymentID, refundID string, now time.Time) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE payments SET status = 'refunded', refund_id = $2, refunded_at = $3, updated_at = now()
		WHERE id = $1 AND status = 'refund_pending'`, paymentID, refundID, now.UTC()); err != nil {
		return fmt.Errorf("record refund: %w", err)
	}
	return nil
}

// checkRoom returns nil if the event has room for the entry by the same
// rules as CreateEntry, counting the live entries other than this one. With
// no room it returns ErrEntryLimit, ErrEventFull or ErrNoMatchesLeft. Call
// it with the event row locked.
func checkRoom(ctx context.Context, q querier, eventID, userID, entryID string, capacity, maxMatches int, now time.Time) error {
	var live, mine int
	if err := q.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE user_id = $2)
		FROM entries
		WHERE event_id = $1 AND id <> $4
		  AND (status IN ('confirmed', 'standby', 'scheduled')
		       OR (status = 'pending_payment' AND hold_expires_at > $3))`,
		eventID, userID, now, entryID,
	).Scan(&live, &mine); err != nil {
		return fmt.Errorf("count entries: %w", err)
	}
	switch {
	case mine >= maxEntriesPerPlayer:
		return ErrEntryLimit
	case live >= capacity:
		return ErrEventFull
	case live+1 > 2*maxMatches:
		return ErrNoMatchesLeft
	}
	return nil
}

// isNoRoom reports whether err is one of checkRoom's "no room" answers.
func isNoRoom(err error) bool {
	return errors.Is(err, ErrEntryLimit) || errors.Is(err, ErrEventFull) || errors.Is(err, ErrNoMatchesLeft)
}
