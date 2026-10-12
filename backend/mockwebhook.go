package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/payments"
)

// sendMockWebhook does what Razorpay's webhook would after a payment, for
// local development, where Razorpay can't reach this machine without a
// tunnel: `go run . -mock-webhook order_…`.
//
// It reads the test order back from Razorpay for its amount, makes up a
// captured payment for it, signs the event with RAZORPAY_WEBHOOK_SECRET
// exactly as Razorpay does, and posts it to the running backend, which
// checks it like any other webhook. Running it again for the same order
// resends the same payment, as Razorpay retries do. Test keys only: with
// live keys a made-up payment would confirm an entry nobody paid for.
//
// Refunds can't follow a made-up payment: if the backend decides to refund
// one (the hold ran out and the event filled), Razorpay answers 404 and the
// payment stays refund_pending.
func sendMockWebhook(ctx context.Context, orderID, webhookURL string) error {
	keyID, keySecret := os.Getenv("RAZORPAY_KEY_ID"), os.Getenv("RAZORPAY_KEY_SECRET")
	secret := os.Getenv("RAZORPAY_WEBHOOK_SECRET")
	switch {
	case !strings.HasPrefix(keyID, "rzp_test_"):
		return errors.New("-mock-webhook needs Razorpay test keys (RAZORPAY_KEY_ID starting rzp_test_); it never runs in live mode")
	case secret == "":
		return errors.New("-mock-webhook needs RAZORPAY_WEBHOOK_SECRET: set any value in .env and restart the backend so both use it")
	case !strings.HasPrefix(orderID, "order_"):
		return fmt.Errorf("%q isn't a Razorpay order ID (order_…): the backend logs one for each payment it starts", orderID)
	}

	rzp, err := payments.NewRazorpay(keyID, keySecret, secret)
	if err != nil {
		return err
	}
	return postMockWebhook(ctx, rzp, secret, orderID, webhookURL)
}

// postMockWebhook is sendMockWebhook after its checks, with the Razorpay
// client passed in so tests can point it at a stand-in server.
func postMockWebhook(ctx context.Context, rzp *payments.Razorpay, secret, orderID, webhookURL string) error {
	order, err := rzp.FetchOrder(ctx, orderID)
	if err != nil {
		return fmt.Errorf("look up the order on Razorpay: %w", err)
	}

	// One made-up payment per order, so sending it again is a retry of the
	// same payment (applied once), not a second payment to refund.
	sum := sha256.Sum256([]byte(order.ID))
	paymentID := "pay_mock" + hex.EncodeToString(sum[:7])
	body, err := json.Marshal(map[string]any{
		"entity":     "event",
		"event":      payments.EventPaymentCaptured,
		"contains":   []string{"payment"},
		"created_at": time.Now().Unix(),
		"payload": map[string]any{
			"payment": map[string]any{
				"entity": map[string]any{
					"id": paymentID, "entity": "payment", "order_id": order.ID,
					"amount": order.AmountPaise, "currency": order.Currency,
					"status": payments.StatusCaptured, "method": "upi", "captured": true,
				},
			},
		},
	})
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Razorpay-Signature", hex.EncodeToString(mac.Sum(nil)))
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post to %s (is the backend running?): %w", webhookURL, err)
	}
	defer func() { _ = res.Body.Close() }()
	reply, _ := io.ReadAll(io.LimitReader(res.Body, 4096))

	log.Printf("mock webhook: payment.captured %s for %s (%d paise) → %s %s",
		paymentID, order.ID, order.AmountPaise, res.Status, strings.TrimSpace(string(reply)))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the backend answered %s", res.Status)
	}
	return nil
}
