// Package payments takes entry fees through a payment provider.
//
// Provider is the only thing the rest of the app depends on, the same way
// internal/otp's Provider is: each vendor (Razorpay today) has an adapter
// that translates its API, signatures and error codes into this interface,
// and Fake stands in for it in local development and tests.
//
// The flow is the hosted-checkout one most Indian gateways share: the server
// creates an order for the amount, the browser's checkout takes the payment
// and hands back a signed (order, payment) pair, and the provider's webhook
// confirms the same payment server to server. The server re-fetches the
// payment before trusting either.
//
// Amounts are always whole paise (the currency's minor unit) in ints, never
// floating-point rupees.
package payments

import (
	"context"
	"errors"
)

// Payment statuses as providers report them. Only Captured means the money
// is ours; Authorized still needs a capture.
const (
	StatusCreated    = "created"
	StatusAuthorized = "authorized"
	StatusCaptured   = "captured"
	StatusRefunded   = "refunded"
	StatusFailed     = "failed"
)

// Webhook events the app acts on. Both carry the captured payment; a
// provider may send either or both, more than once and in any order.
const (
	EventPaymentCaptured = "payment.captured"
	EventOrderPaid       = "order.paid"
)

// OrderRequest is an order to create: the amount the checkout must collect.
type OrderRequest struct {
	AmountPaise int
	Currency    string
	// Receipt is our reference for the order, at most 40 characters.
	Receipt string
	// Notes are stored with the order and shown in the provider's dashboard.
	Notes map[string]string
}

// Order is a created order.
type Order struct {
	ID          string
	AmountPaise int
	Currency    string
	Status      string
}

// Payment is a payment as the provider reports it.
type Payment struct {
	ID          string
	OrderID     string
	AmountPaise int
	Currency    string
	// Status is one of the Status constants (or another provider status,
	// which the app treats as unusable).
	Status string
}

// Refund is an accepted refund request.
type Refund struct {
	ID string
	// Status is the provider's, e.g. "processed" or "pending". Either way
	// the provider has accepted it and will return the money.
	Status string
}

// WebhookEvent is a parsed webhook body.
type WebhookEvent struct {
	// Event is the event's name, e.g. EventPaymentCaptured.
	Event string
	// Payment is the payment the event is about, or nil when it carries none.
	Payment *Payment
}

// Provider takes payments for orders. Implementations must be safe for
// concurrent use.
type Provider interface {
	// Name identifies the adapter in logs and in stored payments, e.g.
	// "razorpay".
	Name() string
	// KeyID is the public key the browser's checkout opens with; "" when the
	// provider has none (Fake).
	KeyID() string

	// CreateOrder creates an order the checkout can collect.
	CreateOrder(ctx context.Context, req OrderRequest) (Order, error)
	// VerifyCheckoutSignature returns nil if signature is the one the
	// provider's checkout gives for this order and payment, and
	// ErrInvalidSignature otherwise. It compares in constant time.
	VerifyCheckoutSignature(orderID, paymentID, signature string) error
	// FetchPayment returns the payment, or ErrNotFound.
	FetchPayment(ctx context.Context, paymentID string) (Payment, error)
	// Capture captures an authorized payment for its full amount.
	Capture(ctx context.Context, paymentID string, amountPaise int, currency string) (Payment, error)
	// Refund refunds amountPaise of a captured payment.
	Refund(ctx context.Context, paymentID string, amountPaise int) (Refund, error)

	// VerifyWebhookSignature returns nil if signature signs body,
	// ErrInvalidSignature if it doesn't, and ErrWebhookDisabled when no
	// webhook secret is configured.
	VerifyWebhookSignature(body []byte, signature string) error
	// ParseWebhook reads a verified webhook body.
	ParseWebhook(body []byte) (WebhookEvent, error)
}

// Adapters return these (possibly wrapped) so handlers never see vendor error
// codes. Any other error means the provider itself failed.
var (
	// ErrInvalidSignature covers a checkout or webhook signature that doesn't
	// match, including a malformed one.
	ErrInvalidSignature = errors.New("payments: invalid signature")
	// ErrWebhookDisabled means webhooks can't be verified because no secret
	// is configured.
	ErrWebhookDisabled = errors.New("payments: webhook secret not configured")
	// ErrNotFound means the provider has no such payment.
	ErrNotFound = errors.New("payments: no such payment")
)
