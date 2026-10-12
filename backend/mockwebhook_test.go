package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AyushV241/BadmintonPro/backend/internal/payments"
)

func TestMockWebhookRefusesUnsafeSetups(t *testing.T) {
	for name, tc := range map[string]struct{ keyID, secret, order, want string }{
		"live keys":         {"rzp_live_abc", "s", "order_x", "test keys"},
		"no webhook secret": {"rzp_test_abc", "", "order_x", "RAZORPAY_WEBHOOK_SECRET"},
		"not an order ID":   {"rzp_test_abc", "s", "ent_x", "isn't a Razorpay order ID"},
	} {
		t.Setenv("RAZORPAY_KEY_ID", tc.keyID)
		t.Setenv("RAZORPAY_KEY_SECRET", "key-secret")
		t.Setenv("RAZORPAY_WEBHOOK_SECRET", tc.secret)
		err := sendMockWebhook(context.Background(), tc.order, "http://127.0.0.1:1/never")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
}

// The mock reads the order's amount from Razorpay and sends an event the
// real webhook check accepts.
func TestMockWebhookSendsASignedCapture(t *testing.T) {
	const secret = "webhook-secret"
	rzpAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, _, ok := r.BasicAuth(); !ok || user != "rzp_test_abc" || r.Method != http.MethodGet || r.URL.Path != "/orders/order_123" {
			http.Error(w, `{"error":{"code":"BAD_REQUEST_ERROR","description":"no"}}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "order_123", "amount": 5000, "currency": "INR", "status": "attempted"})
	}))
	defer rzpAPI.Close()
	rzp, err := payments.NewRazorpay("rzp_test_abc", "key-secret", secret)
	if err != nil {
		t.Fatal(err)
	}
	rzp = rzp.WithBaseURL(rzpAPI.URL + "/")

	var got payments.WebhookEvent
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := rzp.VerifyWebhookSignature(body, r.Header.Get("X-Razorpay-Signature")); err != nil {
			http.Error(w, "bad signature", http.StatusBadRequest)
			return
		}
		got, _ = rzp.ParseWebhook(body)
		_, _ = w.Write([]byte(`{"status":"applied"}`))
	}))
	defer receiver.Close()

	if err := postMockWebhook(context.Background(), rzp, secret, "order_123", receiver.URL); err != nil {
		t.Fatal(err)
	}
	p := got.Payment
	if got.Event != payments.EventPaymentCaptured || p == nil || p.OrderID != "order_123" || p.AmountPaise != 5000 ||
		p.Currency != "INR" || p.Status != payments.StatusCaptured || !strings.HasPrefix(p.ID, "pay_mock") {
		t.Errorf("event = %+v, payment = %+v", got, p)
	}

	// Sending it again is the same payment, as a Razorpay retry would be.
	first := p.ID
	if err := postMockWebhook(context.Background(), rzp, secret, "order_123", receiver.URL); err != nil || got.Payment.ID != first {
		t.Errorf("resend: payment %q, want %q (err %v)", got.Payment.ID, first, err)
	}

	// An order Razorpay doesn't know is reported, not sent.
	if err := postMockWebhook(context.Background(), rzp, secret, "order_nope", receiver.URL); err == nil {
		t.Error("unknown order: no error")
	}
}
