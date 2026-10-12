package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/payments"
)

const (
	// maxWebhookBytes caps a webhook body. Razorpay's payment events are a
	// few kilobytes.
	maxWebhookBytes = 64 << 10
	// webhookSignatureHeader carries Razorpay's webhook signature. The fake
	// provider's test webhooks use it too.
	webhookSignatureHeader = "X-Razorpay-Signature"
	// refundTimeout bounds the refund call made after a confirmation, which
	// runs even if the caller has gone away.
	refundTimeout = 20 * time.Second
)

// errUnusablePayment means the provider's payment can't pay for this order:
// unknown, for another order or amount, or not captured.
var errUnusablePayment = errors.New("payment can't be used")

type checkoutResponse struct {
	Checkout Checkout `json:"checkout"`
}

// writePaymentError maps payment errors to responses, like writeEntryError.
func writePaymentError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, ErrNoSuchEntry):
		writeError(w, http.StatusNotFound, "entry not found")
	case errors.Is(err, ErrNoSuchPayment):
		writeError(w, http.StatusNotFound, "payment not found")
	case errors.Is(err, ErrEntryNotPending):
		writeError(w, http.StatusConflict, "this entry isn't waiting for payment")
	case errors.Is(err, ErrHoldExpired):
		writeError(w, http.StatusConflict, "your hold on this spot has expired; join again to pay")
	case errors.Is(err, ErrEventNotOpen):
		writeError(w, http.StatusConflict, "this event isn't open any more")
	case errors.Is(err, ErrPaymentMismatch), errors.Is(err, errUnusablePayment):
		writeError(w, http.StatusConflict, "this payment can't be used for this entry")
	default:
		log.Printf("payments %s: %v", action, err)
		writeError(w, http.StatusInternalServerError, "couldn't "+action)
	}
}

func writeProviderError(w http.ResponseWriter, err error, action string) {
	log.Printf("payments %s: provider: %v", action, err)
	writeError(w, http.StatusBadGateway, "the payment provider didn't respond properly; try again")
}

func writePaymentsDisabled(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "payments aren't set up")
}

// handlePayEntry starts paying for the caller's held entry. It creates an
// order with the provider, or reuses the entry's open one, and returns what
// the page needs to open the checkout. A free event's entry is confirmed at
// once instead, and the entry is returned.
//
//	POST /api/me/entries/{id}/pay
//	→ {"checkout": {"provider","keyId","orderId","amountPaise","currency","name","description","prefill"}}
//	→ {"entry": Entry} for a free event
func (a *API) handlePayEntry(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	ctx, now := r.Context(), time.Now()
	providerName := ""
	if a.paymentProvider != nil {
		providerName = a.paymentProvider.Name()
	}
	payable, err := a.payments.PayableEntry(ctx, user.ID, r.PathValue("id"), providerName, now)
	if err != nil {
		writePaymentError(w, err, "start the payment")
		return
	}
	en := payable.Entry

	// Nothing to collect, so no provider is needed.
	if en.Event.FeePaise == 0 {
		out, err := a.payments.ConfirmPayment(ctx, PaymentConfirmation{EntryID: en.ID, UserID: user.ID, Now: now})
		if err != nil {
			writePaymentError(w, err, "confirm the entry")
			return
		}
		writeJSON(w, http.StatusOK, entryResponse{Entry: out.Entry})
		return
	}
	if a.paymentProvider == nil {
		writePaymentsDisabled(w)
		return
	}

	order := payable.Order
	if order == nil {
		created, err := a.paymentProvider.CreateOrder(ctx, payments.OrderRequest{
			AmountPaise: en.Event.FeePaise,
			Currency:    en.Event.Currency,
			Receipt:     en.ID, // "ent_" + 24 hex: inside Razorpay's 40 characters
			Notes:       map[string]string{"entry_id": en.ID},
		})
		if err == nil && (created.AmountPaise != en.Event.FeePaise || created.Currency != en.Event.Currency) {
			err = errors.New("order created for a different amount")
		}
		if err != nil {
			writeProviderError(w, err, "create order")
			return
		}
		saved, err := a.payments.SaveOrder(ctx, NewOrder{
			EntryID: en.ID, UserID: user.ID, Provider: providerName, OrderID: created.ID,
			AmountPaise: created.AmountPaise, Currency: created.Currency, Now: now,
		})
		if err != nil {
			writePaymentError(w, err, "start the payment")
			return
		}
		order = &saved
		log.Printf("payments: order %s for entry %s (%d paise)", saved.OrderID, en.ID, saved.AmountPaise)
	}

	writeJSON(w, http.StatusOK, checkoutResponse{Checkout: Checkout{
		Provider:    providerName,
		KeyID:       a.paymentProvider.KeyID(),
		OrderID:     order.OrderID,
		AmountPaise: order.AmountPaise,
		Currency:    order.Currency,
		Name:        checkoutName,
		Description: en.Event.Title + " · 1 entry",
		Prefill:     CheckoutPrefill{Name: user.Name, Email: user.Email, Contact: user.Phone},
	}})
}

// handleVerifyPayment confirms the caller's entry from what the checkout
// handed the page. The signature only proves the pair came from the
// provider's checkout, so the payment is fetched (and captured if only
// authorized) before the entry is confirmed. The webhook confirms the same
// payment too; whichever arrives second changes nothing.
//
// The entry comes back in whatever state the payment left it: confirmed,
// or refunded when its hold ran out and the event filled up meanwhile.
//
//	POST /api/payments/verify  {"orderId","paymentId","signature"}  → {"entry": Entry}
func (a *API) handleVerifyPayment(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if a.paymentProvider == nil {
		writePaymentsDisabled(w)
		return
	}
	var req verifyRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	if req.OrderID == "" || req.PaymentID == "" || req.Signature == "" {
		writeError(w, http.StatusBadRequest, "orderId, paymentId and signature are all required")
		return
	}
	if err := a.paymentProvider.VerifyCheckoutSignature(req.OrderID, req.PaymentID, req.Signature); err != nil {
		writeError(w, http.StatusBadRequest, "the payment's signature doesn't match")
		return
	}

	ctx := r.Context()
	order, err := a.payments.OnlinePayment(ctx, a.paymentProvider.Name(), req.OrderID)
	if err == nil && order.UserID != user.ID {
		err = ErrNoSuchPayment // someone else's order looks like no order
	}
	if err != nil {
		writePaymentError(w, err, "verify the payment")
		return
	}
	payment, err := a.capturedPayment(ctx, req.PaymentID, order)
	if errors.Is(err, errUnusablePayment) {
		log.Printf("payments verify: %s for order %s: %v", req.PaymentID, req.OrderID, err)
		writePaymentError(w, err, "verify the payment")
		return
	}
	if err != nil {
		writeProviderError(w, err, "verify the payment")
		return
	}

	out, err := a.applyPayment(ctx, payment)
	if err != nil {
		writePaymentError(w, err, "confirm the payment")
		return
	}
	writeJSON(w, http.StatusOK, entryResponse{Entry: out.Entry})
}

// capturedPayment fetches the provider's payment and checks it pays this
// order in full, capturing it if it's only authorized.
func (a *API) capturedPayment(ctx context.Context, paymentID string, order OnlinePayment) (payments.Payment, error) {
	p, err := a.paymentProvider.FetchPayment(ctx, paymentID)
	if errors.Is(err, payments.ErrNotFound) {
		return payments.Payment{}, errors.Join(errUnusablePayment, err)
	}
	if err != nil {
		return payments.Payment{}, err
	}
	if p.OrderID != order.OrderID || p.AmountPaise != order.AmountPaise || p.Currency != order.Currency {
		return payments.Payment{}, errors.Join(errUnusablePayment, errors.New("payment is for another order or amount"))
	}
	switch p.Status {
	case payments.StatusCaptured:
		return p, nil
	case payments.StatusAuthorized:
		captured, err := a.paymentProvider.Capture(ctx, paymentID, order.AmountPaise, order.Currency)
		if err != nil {
			// Someone else (the other of verify and a retry, or the
			// dashboard's auto-capture) may have captured it first.
			if again, fetchErr := a.paymentProvider.FetchPayment(ctx, paymentID); fetchErr == nil && again.Status == payments.StatusCaptured {
				return again, nil
			}
			return payments.Payment{}, err
		}
		if captured.Status != payments.StatusCaptured {
			return payments.Payment{}, errors.Join(errUnusablePayment, errors.New("capture left it "+captured.Status))
		}
		return captured, nil
	default:
		return payments.Payment{}, errors.Join(errUnusablePayment, errors.New("payment is "+p.Status))
	}
}

// applyPayment confirms a captured payment, then refunds it if it couldn't
// be used.
func (a *API) applyPayment(ctx context.Context, p payments.Payment) (PaymentOutcome, error) {
	out, err := a.payments.ConfirmPayment(ctx, PaymentConfirmation{
		Provider: a.paymentProvider.Name(), OrderID: p.OrderID, PaymentID: p.ID,
		AmountPaise: p.AmountPaise, Currency: p.Currency, Now: time.Now(),
	})
	if err != nil {
		return PaymentOutcome{}, err
	}
	if out.Refund != nil {
		a.refund(ctx, *out.Refund)
	}
	return out, nil
}

// refund asks the provider to refund a refund_pending payment and records
// the refund. It is called after the confirmation has committed, so a slow
// or failing provider never holds the event's lock.
//
// TODO(payments): a retry job for refunds that fail here. The payment stays
// refund_pending, which is what such a job (or an admin, until then) looks
// for; nothing retries it automatically yet.
func (a *API) refund(ctx context.Context, pr PendingRefund) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refundTimeout)
	defer cancel()
	rf, err := a.paymentProvider.Refund(ctx, pr.ProviderPaymentID, pr.AmountPaise)
	if err != nil {
		log.Printf("payments: refund of %s (%s, %d paise) failed; left refund_pending: %v",
			pr.PaymentID, pr.ProviderPaymentID, pr.AmountPaise, err)
		return
	}
	if err := a.payments.FinishRefund(ctx, pr.PaymentID, rf.ID, time.Now()); err != nil {
		log.Printf("payments: refund %s of %s accepted but not recorded: %v", rf.ID, pr.PaymentID, err)
		return
	}
	log.Printf("payments: refunded %s (%s) as %s", pr.PaymentID, pr.ProviderPaymentID, rf.ID)
}

// handlePaymentWebhook is the provider's server-to-server notice that a
// payment was captured: the source of truth, since the page may never get
// back to verify. Webhooks repeat and arrive in any order, so it is
// idempotent, and it answers 200 to everything it ignores or has already
// applied so the provider stops retrying. Only a failure on our side is a
// 5xx, which the provider retries.
//
// It has no session: the signature over the raw body authenticates it.
//
//	POST /api/payments/webhook  (X-Razorpay-Signature)
func (a *API) handlePaymentWebhook(w http.ResponseWriter, r *http.Request) {
	if a.paymentProvider == nil {
		writePaymentsDisabled(w)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "webhook body too large or unreadable")
		return
	}
	switch err := a.paymentProvider.VerifyWebhookSignature(body, r.Header.Get(webhookSignatureHeader)); {
	case errors.Is(err, payments.ErrWebhookDisabled):
		writeError(w, http.StatusServiceUnavailable, "payment webhooks aren't set up")
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, "bad webhook signature")
		return
	}
	ev, err := a.paymentProvider.ParseWebhook(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "webhook body must be valid JSON")
		return
	}

	ignore := func(why string) {
		if why != "" {
			log.Printf("payments webhook: ignored %s: %s", ev.Event, why)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
	if ev.Event != payments.EventPaymentCaptured && ev.Event != payments.EventOrderPaid {
		ignore("") // events we don't subscribe to for anything
		return
	}
	p := ev.Payment
	if p == nil || p.OrderID == "" || p.Status != payments.StatusCaptured {
		ignore("no captured payment for an order")
		return
	}
	ctx := r.Context()
	order, err := a.payments.OnlinePayment(ctx, a.paymentProvider.Name(), p.OrderID)
	if errors.Is(err, ErrNoSuchPayment) {
		// Not one of ours: another app on the same account, or a test.
		ignore("unknown order " + p.OrderID)
		return
	}
	if err != nil {
		log.Printf("payments webhook: %v", err)
		writeError(w, http.StatusInternalServerError, "couldn't load the order")
		return
	}
	if p.AmountPaise != order.AmountPaise || p.Currency != order.Currency {
		ignore("payment " + p.ID + " doesn't match order " + p.OrderID)
		return
	}

	out, err := a.applyPayment(ctx, *p)
	if errors.Is(err, ErrPaymentMismatch) {
		ignore("payment " + p.ID + " doesn't match order " + p.OrderID)
		return
	}
	if err != nil {
		log.Printf("payments webhook: confirm %s: %v", p.ID, err)
		writeError(w, http.StatusInternalServerError, "couldn't apply the payment")
		return
	}
	status := "applied"
	if out.Repeat {
		status = "already applied"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}
