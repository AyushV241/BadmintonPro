package main

import (
	"context"
	"errors"
	"time"
)

// Payments: taking an entry's fee through the payment provider, and
// confirming the entry once the provider says it's paid. See
// docs/design/events-and-matches.md, sections 4.2 and 7.

var (
	ErrHoldExpired     = errors.New("entry's hold has expired")
	ErrNoSuchPayment   = errors.New("no such payment")
	ErrPaymentMismatch = errors.New("payment doesn't match its order")
)

// Payment providers, kinds and statuses, as stored. The provider column also
// holds the online provider's Name(): "razorpay" or "fake".
const (
	ProviderOffline = "offline"

	PaymentOnline  = "online"
	PaymentOffline = "offline"

	PaymentCreated       = "created"
	PaymentPaid          = "paid"
	PaymentFailed        = "failed"
	PaymentRefundPending = "refund_pending"
	PaymentRefunded      = "refunded"
)

// checkoutName is the merchant name the checkout shows.
const checkoutName = "BadmintonPro"

// Checkout is what the page needs to open the provider's checkout for an
// entry's order.
type Checkout struct {
	// Provider is "razorpay" or "fake"; the page opens Razorpay Checkout or
	// its fake stand-in.
	Provider string `json:"provider"`
	// KeyID is Razorpay's public key ID; "" for the fake provider.
	KeyID       string          `json:"keyId"`
	OrderID     string          `json:"orderId"`
	AmountPaise int             `json:"amountPaise"`
	Currency    string          `json:"currency"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Prefill     CheckoutPrefill `json:"prefill"`
}

// CheckoutPrefill fills in the checkout's contact fields from the profile.
type CheckoutPrefill struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Contact string `json:"contact"`
}

// OnlinePayment is a payments row for a provider's order.
type OnlinePayment struct {
	ID          string
	EntryID     string
	UserID      string
	Provider    string
	OrderID     string
	AmountPaise int
	Currency    string
	Status      string
}

// PayableEntry is an entry that can be paid for now, with any open order
// that paying can reuse.
type PayableEntry struct {
	Entry Entry
	// Order is the entry's created order with this provider and the event's
	// current fee, or nil.
	Order *OnlinePayment
}

// NewOrder is an order just created with the provider, to record.
type NewOrder struct {
	EntryID, UserID string
	Provider        string
	OrderID         string
	AmountPaise     int
	Currency        string
	Now             time.Time
}

// PaymentConfirmation says an entry is paid: either a captured online
// payment (Provider, OrderID, PaymentID, AmountPaise, Currency), or, for a
// free event, the entry itself (EntryID, UserID) with no payment.
type PaymentConfirmation struct {
	Provider    string
	OrderID     string
	PaymentID   string
	AmountPaise int
	Currency    string

	EntryID, UserID string

	// Now decides whether the hold is live and the event over.
	Now time.Time
}

// PendingRefund is a payment the provider must now refund. Its row is
// refund_pending.
type PendingRefund struct {
	PaymentID         string // payments.id
	ProviderPaymentID string
	AmountPaise       int
}

// PaymentOutcome is what a confirmation did.
type PaymentOutcome struct {
	// Entry is the paid-for entry afterwards, as its player sees it.
	Entry Entry
	// Repeat is true when this payment had already been applied: a repeated
	// webhook, or the webhook and the page's verify both arriving.
	Repeat bool
	// Refund is set when the payment can't be used and has to be refunded.
	Refund *PendingRefund
}

// PaymentStore is what payments need. Only PostgresStore implements it.
type PaymentStore interface {
	// PayableEntry returns the user's entry if it can be paid for now:
	// pending_payment with its hold still live, in a published event. With
	// it comes the entry's open order with provider at the current fee, if
	// any (provider "" never has one). It returns ErrNoSuchEntry (also for
	// someone else's entry), ErrEntryNotPending, ErrHoldExpired or
	// ErrEventNotOpen.
	PayableEntry(ctx context.Context, userID, entryID, provider string, now time.Time) (PayableEntry, error)

	// SaveOrder records a new order as the entry's created payment, after
	// checking again, with the entry locked, that it can be paid for. Any
	// other created order for the entry is marked failed, unless one has the
	// same provider and amount: that one is returned instead, and the new
	// order is left unused. It returns PayableEntry's errors.
	SaveOrder(ctx context.Context, o NewOrder) (OnlinePayment, error)

	// OnlinePayment returns the payment row for a provider's order, or
	// ErrNoSuchPayment.
	OnlinePayment(ctx context.Context, provider, orderID string) (OnlinePayment, error)

	// ConfirmPayment applies a payment, in one transaction that locks the
	// event, then the entry, then the payment row, so it is safe to call
	// more than once and concurrently (verify, webhooks):
	//
	//   - a payment already applied changes nothing (Repeat);
	//   - a pending_payment entry is confirmed if its hold is live, or if
	//     the event still has room, and its event is published and not over;
	//   - otherwise the entry is refunded and the payment must be (Refund);
	//   - a second payment for an entry already paid for is refunded and
	//     leaves the entry alone.
	//
	// A free event's confirmation (no payment) confirms a payable entry,
	// with PayableEntry's errors otherwise. It returns ErrNoSuchPayment for
	// an unknown order and ErrPaymentMismatch for an amount or currency
	// that isn't the order's.
	ConfirmPayment(ctx context.Context, c PaymentConfirmation) (PaymentOutcome, error)

	// FinishRefund records that the provider accepted a refund_pending
	// payment's refund.
	FinishRefund(ctx context.Context, paymentID, refundID string, now time.Time) error
}

// verifyRequest is the body of POST /api/payments/verify: what the
// provider's checkout hands the page on success.
type verifyRequest struct {
	OrderID   string `json:"orderId"`
	PaymentID string `json:"paymentId"`
	Signature string `json:"signature"`
}
