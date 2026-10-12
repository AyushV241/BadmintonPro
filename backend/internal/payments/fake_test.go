package payments

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFake(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	if f.Name() != "fake" || f.KeyID() != "" {
		t.Errorf("name %q, key ID %q", f.Name(), f.KeyID())
	}

	order, err := f.CreateOrder(ctx, OrderRequest{AmountPaise: 5000, Currency: "INR", Receipt: "ent_1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(order.ID, "order_fake_") || order.AmountPaise != 5000 || order.Currency != "INR" || order.Status != StatusCreated {
		t.Errorf("order = %+v", order)
	}
	other, _ := f.CreateOrder(ctx, OrderRequest{AmountPaise: 5000, Currency: "INR", Receipt: "ent_1"})
	if other.ID == order.ID {
		t.Error("two orders share an ID")
	}

	// A payment the provider hasn't seen a checkout for doesn't exist.
	if _, err := f.FetchPayment(ctx, "pay_fake_1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unverified payment: err = %v", err)
	}
	for name, tc := range map[string][3]string{
		"another order":   {other.ID, "pay_fake_1", FakeSignature(order.ID, "pay_fake_1")},
		"another payment": {order.ID, "pay_fake_2", FakeSignature(order.ID, "pay_fake_1")},
		"not a fake ID":   {order.ID, "pay_real", FakeSignature(order.ID, "pay_real")},
		"empty":           {order.ID, "pay_fake_1", ""},
	} {
		if err := f.VerifyCheckoutSignature(tc[0], tc[1], tc[2]); !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := f.VerifyCheckoutSignature(order.ID, "pay_fake_1", "fake:"+order.ID+"|pay_fake_1"); err != nil {
		t.Fatal(err)
	}
	want := Payment{ID: "pay_fake_1", OrderID: order.ID, AmountPaise: 5000, Currency: "INR", Status: StatusCaptured}
	if p, err := f.FetchPayment(ctx, "pay_fake_1"); err != nil || p != want {
		t.Errorf("payment = %+v, %v", p, err)
	}
	if p, err := f.Capture(ctx, "pay_fake_1", 5000, "INR"); err != nil || p != want {
		t.Errorf("capture = %+v, %v", p, err)
	}
	if r, err := f.Refund(ctx, "pay_fake_1", 5000); err != nil || !strings.HasPrefix(r.ID, "rfnd_fake_") || r.Status != "processed" {
		t.Errorf("refund = %+v, %v", r, err)
	}

	// The webhook is off until a secret is set, then Razorpay-shaped.
	body := []byte(capturedWebhook)
	if err := f.VerifyWebhookSignature(body, sign("", capturedWebhook)); !errors.Is(err, ErrWebhookDisabled) {
		t.Errorf("default webhook: err = %v", err)
	}
	f.WithWebhookSecret("s")
	if err := f.VerifyWebhookSignature(body, sign("s", capturedWebhook)); err != nil {
		t.Errorf("signed webhook: %v", err)
	}
	if err := f.VerifyWebhookSignature(body, sign("t", capturedWebhook)); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("wrong secret: err = %v", err)
	}
	if ev, err := f.ParseWebhook(body); err != nil || ev.Payment == nil || ev.Payment.ID != "pay_W" {
		t.Errorf("parse = %+v, %v", ev, err)
	}

	// RecordPayment stands in for a checkout only a webhook reports.
	f.RecordPayment(other.ID, "pay_fake_2")
	if p, err := f.FetchPayment(ctx, "pay_fake_2"); err != nil || p.OrderID != other.ID {
		t.Errorf("recorded payment = %+v, %v", p, err)
	}
}
