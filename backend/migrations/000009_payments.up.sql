-- Payments for entries (design doc, sections 4.2, 7 and 11): online ones
-- through the payment provider (Razorpay, or the fake one in development)
-- and offline ones the walk-in desk records. Money is whole paise (rule 3).
--
-- One entry can have several payment rows: an abandoned order, a payment
-- refunded because its hold had expired, a second payment for an entry
-- already paid for. At most one of them is paid (rule 9).

CREATE TABLE payments (
    id       TEXT PRIMARY KEY,
    -- Entries and users are never deleted (rule 5), so no ON DELETE here.
    entry_id TEXT NOT NULL REFERENCES entries (id),
    user_id  TEXT NOT NULL REFERENCES users (id),
    provider TEXT NOT NULL CHECK (provider IN ('razorpay', 'fake', 'offline')),
    kind     TEXT NOT NULL CHECK (kind IN ('online', 'offline')),
    -- The provider's order and the payment that paid it. NULL for offline
    -- payments; provider_payment_id is NULL until a payment arrives.
    -- provider_order_id is also NULL on the rare row recording a second
    -- payment made against an order that was already paid.
    provider_order_id   TEXT UNIQUE,
    provider_payment_id TEXT UNIQUE,
    amount_paise INTEGER NOT NULL CHECK (amount_paise > 0),
    currency     TEXT NOT NULL,
    -- created: an order waiting for its payment. failed: an order nobody
    -- will pay (the entry was cancelled, or a newer order replaced it).
    -- refund_pending: paid but not usable, and the refund isn't accepted by
    -- the provider yet. refunded: the provider accepted the refund.
    status TEXT NOT NULL
        CHECK (status IN ('created', 'paid', 'failed', 'refund_pending', 'refunded')),
    -- The admin who took an offline payment at the desk.
    recorded_by TEXT REFERENCES users (id),
    -- The provider's refund, once accepted.
    refund_id   TEXT,
    paid_at     TIMESTAMPTZ,
    refunded_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The payer is the entry's player.
    FOREIGN KEY (entry_id, user_id) REFERENCES entries (id, user_id),
    CHECK ((provider = 'offline') = (kind = 'offline')),
    CHECK (kind = 'online' OR (recorded_by IS NOT NULL AND status IN ('paid', 'refund_pending', 'refunded'))),
    CHECK (status <> 'paid' OR paid_at IS NOT NULL),
    CHECK (status <> 'refunded' OR (refunded_at IS NOT NULL AND refund_id IS NOT NULL))
);
-- One paid payment per entry (rule 9): a second one is refunded instead.
CREATE UNIQUE INDEX payments_one_paid_per_entry ON payments (entry_id) WHERE status = 'paid';
-- One open order per entry, so paying again reuses it.
CREATE UNIQUE INDEX payments_one_open_order_per_entry ON payments (entry_id) WHERE status = 'created';
