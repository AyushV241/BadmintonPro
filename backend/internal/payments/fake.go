package payments

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// FakeSignature is the checkout signature Fake accepts for a payment: the
// frontend's fake checkout sends it in place of Razorpay's HMAC.
func FakeSignature(orderID, paymentID string) string {
	return "fake:" + orderID + "|" + paymentID
}

// Fake is the local-development provider, like OTP's console provider: no
// money moves and nothing leaves the process. Orders are order_fake_<hex>,
// the frontend's fake checkout makes up a pay_fake_<hex> payment and signs
// it with FakeSignature, every payment is captured for its order's amount,
// and every refund succeeds.
//
// Orders live in process memory, so it only suits a single instance, and a
// restart forgets them: an order created before one can't be paid after it.
// Its webhook is disabled unless WithWebhookSecret is called (tests do); it
// then speaks Razorpay's webhook format and signature.
type Fake struct {
	webhookSecret string

	mu     sync.Mutex
	orders map[string]Order
	// payments maps a payment ID to its order, recorded when its checkout
	// signature is verified.
	payments map[string]string
}

// NewFake returns a fake provider with no orders.
func NewFake() *Fake {
	return &Fake{orders: make(map[string]Order), payments: make(map[string]string)}
}

// WithWebhookSecret enables the webhook with Razorpay's signature scheme. It
// returns f for chaining.
func (f *Fake) WithWebhookSecret(secret string) *Fake {
	f.webhookSecret = secret
	return f
}

func (f *Fake) Name() string  { return "fake" }
func (f *Fake) KeyID() string { return "" }

func (f *Fake) CreateOrder(_ context.Context, req OrderRequest) (Order, error) {
	id, err := fakeID("order_fake_")
	if err != nil {
		return Order{}, err
	}
	o := Order{ID: id, AmountPaise: req.AmountPaise, Currency: req.Currency, Status: StatusCreated}
	f.mu.Lock()
	f.orders[id] = o
	f.mu.Unlock()
	return o, nil
}

func (f *Fake) VerifyCheckoutSignature(orderID, paymentID, signature string) error {
	want := FakeSignature(orderID, paymentID)
	if !strings.HasPrefix(paymentID, "pay_fake_") ||
		subtle.ConstantTimeCompare([]byte(signature), []byte(want)) != 1 {
		return ErrInvalidSignature
	}
	f.mu.Lock()
	f.payments[paymentID] = orderID
	f.mu.Unlock()
	return nil
}

// RecordPayment says a payment was made for an order, as a verified checkout
// signature does. Tests use it to fake a payment that only a webhook
// reports.
func (f *Fake) RecordPayment(orderID, paymentID string) {
	f.mu.Lock()
	f.payments[paymentID] = orderID
	f.mu.Unlock()
}

func (f *Fake) FetchPayment(_ context.Context, paymentID string) (Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	orderID, ok := f.payments[paymentID]
	if !ok {
		return Payment{}, fmt.Errorf("%w: %s", ErrNotFound, paymentID)
	}
	o, ok := f.orders[orderID]
	if !ok {
		return Payment{}, fmt.Errorf("%w: order %s (the fake provider forgets orders on restart)", ErrNotFound, orderID)
	}
	return Payment{ID: paymentID, OrderID: orderID, AmountPaise: o.AmountPaise, Currency: o.Currency, Status: StatusCaptured}, nil
}

func (f *Fake) Capture(ctx context.Context, paymentID string, _ int, _ string) (Payment, error) {
	return f.FetchPayment(ctx, paymentID)
}

func (f *Fake) Refund(_ context.Context, _ string, _ int) (Refund, error) {
	id, err := fakeID("rfnd_fake_")
	if err != nil {
		return Refund{}, err
	}
	return Refund{ID: id, Status: "processed"}, nil
}

func (f *Fake) VerifyWebhookSignature(body []byte, signature string) error {
	if f.webhookSecret == "" {
		return ErrWebhookDisabled
	}
	return verifyHMAC(f.webhookSecret, body, signature)
}

func (f *Fake) ParseWebhook(body []byte) (WebhookEvent, error) {
	return parseRazorpayWebhook(body)
}

func fakeID(prefix string) (string, error) {
	buf := make([]byte, 7) // 14 hex digits, like Razorpay's IDs
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf), nil
}
