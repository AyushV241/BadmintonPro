package payments

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
	"net/http"
	"net/url"
	"strings"
	"time"
)

const razorpayAPIURL = "https://api.razorpay.com/v1"

// Razorpay adapts Razorpay's Orders and Payments APIs. They are a handful of
// JSON calls with HTTP Basic auth, so this uses net/http rather than the
// Razorpay SDK.
type Razorpay struct {
	keyID     string // public: rzp_test_… or rzp_live_…
	keySecret string // signs checkout responses and authenticates API calls
	// webhookSecret is set in the Razorpay dashboard for the webhook; ""
	// disables webhooks.
	webhookSecret string
	baseURL       string
	client        *http.Client
}

// NewRazorpay authenticates with a key ID and secret. webhookSecret may be
// "" to leave webhooks disabled.
func NewRazorpay(keyID, keySecret, webhookSecret string) (*Razorpay, error) {
	if keyID == "" || keySecret == "" {
		return nil, errors.New("razorpay: key ID and key secret are both required")
	}
	return &Razorpay{
		keyID:         keyID,
		keySecret:     keySecret,
		webhookSecret: webhookSecret,
		baseURL:       razorpayAPIURL,
		client:        &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// WithBaseURL points the client at another API root, such as an httptest
// server. It returns r for chaining.
func (r *Razorpay) WithBaseURL(baseURL string) *Razorpay {
	r.baseURL = strings.TrimRight(baseURL, "/")
	return r
}

func (r *Razorpay) Name() string  { return "razorpay" }
func (r *Razorpay) KeyID() string { return r.keyID }

// razorpayPayment is a payment entity, in API responses and webhooks alike.
type razorpayPayment struct {
	ID       string `json:"id"`
	OrderID  string `json:"order_id"`
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
	Status   string `json:"status"`
}

func (p razorpayPayment) payment() Payment {
	return Payment{ID: p.ID, OrderID: p.OrderID, AmountPaise: p.Amount, Currency: p.Currency, Status: p.Status}
}

func (r *Razorpay) CreateOrder(ctx context.Context, req OrderRequest) (Order, error) {
	body := map[string]any{"amount": req.AmountPaise, "currency": req.Currency, "receipt": req.Receipt}
	if len(req.Notes) > 0 {
		body["notes"] = req.Notes
	}
	var order struct {
		ID       string `json:"id"`
		Amount   int    `json:"amount"`
		Currency string `json:"currency"`
		Status   string `json:"status"`
	}
	if err := r.call(ctx, http.MethodPost, "/orders", body, &order, "create order"); err != nil {
		return Order{}, err
	}
	if order.ID == "" {
		return Order{}, errors.New("razorpay: create order: response has no order ID")
	}
	return Order{ID: order.ID, AmountPaise: order.Amount, Currency: order.Currency, Status: order.Status}, nil
}

// FetchOrder reads an order back, e.g. for the local mock webhook to learn
// its amount.
func (r *Razorpay) FetchOrder(ctx context.Context, orderID string) (Order, error) {
	var order struct {
		ID       string `json:"id"`
		Amount   int    `json:"amount"`
		Currency string `json:"currency"`
		Status   string `json:"status"`
	}
	err := r.call(ctx, http.MethodGet, "/orders/"+url.PathEscape(orderID), nil, &order, "fetch order")
	var apiErr *razorpayError
	if errors.As(err, &apiErr) && (apiErr.status == http.StatusBadRequest || apiErr.status == http.StatusNotFound) {
		return Order{}, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	if err != nil {
		return Order{}, err
	}
	return Order{ID: order.ID, AmountPaise: order.Amount, Currency: order.Currency, Status: order.Status}, nil
}

// VerifyCheckoutSignature checks the razorpay_signature that Checkout hands
// the page: hex(HMAC-SHA256(key secret, order_id + "|" + payment_id)).
func (r *Razorpay) VerifyCheckoutSignature(orderID, paymentID, signature string) error {
	return verifyHMAC(r.keySecret, []byte(orderID+"|"+paymentID), signature)
}

func (r *Razorpay) FetchPayment(ctx context.Context, paymentID string) (Payment, error) {
	var p razorpayPayment
	err := r.call(ctx, http.MethodGet, "/payments/"+url.PathEscape(paymentID), nil, &p, "fetch payment")
	var apiErr *razorpayError
	// An unknown ID is a 400 ("The id provided does not exist"); a 404 is
	// handled the same way in case that changes.
	if errors.As(err, &apiErr) && (apiErr.status == http.StatusBadRequest || apiErr.status == http.StatusNotFound) {
		return Payment{}, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	if err != nil {
		return Payment{}, err
	}
	return p.payment(), nil
}

func (r *Razorpay) Capture(ctx context.Context, paymentID string, amountPaise int, currency string) (Payment, error) {
	var p razorpayPayment
	if err := r.call(ctx, http.MethodPost, "/payments/"+url.PathEscape(paymentID)+"/capture",
		map[string]any{"amount": amountPaise, "currency": currency}, &p, "capture payment"); err != nil {
		return Payment{}, err
	}
	return p.payment(), nil
}

func (r *Razorpay) Refund(ctx context.Context, paymentID string, amountPaise int) (Refund, error) {
	var refund struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := r.call(ctx, http.MethodPost, "/payments/"+url.PathEscape(paymentID)+"/refund",
		map[string]any{"amount": amountPaise}, &refund, "refund payment"); err != nil {
		return Refund{}, err
	}
	if refund.ID == "" {
		return Refund{}, errors.New("razorpay: refund payment: response has no refund ID")
	}
	return Refund{ID: refund.ID, Status: refund.Status}, nil
}

// VerifyWebhookSignature checks X-Razorpay-Signature:
// hex(HMAC-SHA256(webhook secret, raw body)).
func (r *Razorpay) VerifyWebhookSignature(body []byte, signature string) error {
	if r.webhookSecret == "" {
		return ErrWebhookDisabled
	}
	return verifyHMAC(r.webhookSecret, body, signature)
}

func (r *Razorpay) ParseWebhook(body []byte) (WebhookEvent, error) {
	return parseRazorpayWebhook(body)
}

// parseRazorpayWebhook reads Razorpay's webhook body. Both events the app
// handles carry the payment at payload.payment.entity. It isn't strict:
// Razorpay adds fields without notice.
func parseRazorpayWebhook(body []byte) (WebhookEvent, error) {
	var w struct {
		Event   string `json:"event"`
		Payload struct {
			Payment *struct {
				Entity razorpayPayment `json:"entity"`
			} `json:"payment"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &w); err != nil {
		return WebhookEvent{}, fmt.Errorf("razorpay: decode webhook: %w", err)
	}
	ev := WebhookEvent{Event: w.Event}
	if w.Payload.Payment != nil && w.Payload.Payment.Entity.ID != "" {
		p := w.Payload.Payment.Entity.payment()
		ev.Payment = &p
	}
	return ev, nil
}

// verifyHMAC checks that signature is hex(HMAC-SHA256(secret, message)).
// hmac.Equal compares in constant time, so a wrong signature takes as long
// to reject however much of it matches.
func verifyHMAC(secret string, message []byte, signature string) error {
	got, err := hex.DecodeString(signature)
	if err != nil || len(got) != sha256.Size {
		return ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(message)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return ErrInvalidSignature
	}
	return nil
}

// razorpayError is a non-2xx API response.
type razorpayError struct {
	status      int
	code        string
	description string
}

func (e *razorpayError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("HTTP %d, Razorpay %s: %s", e.status, e.code, e.description)
	}
	return fmt.Sprintf("HTTP %d", e.status)
}

// call sends a JSON request (body may be nil) and decodes a 2xx response
// into out. A non-2xx response is a *razorpayError.
func (r *Razorpay) call(ctx context.Context, method, path string, body, out any, action string) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("razorpay: %s: %w", action, err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("razorpay: %s: %w", action, err)
	}
	req.SetBasicAuth(r.keyID, r.keySecret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("razorpay: %s: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("razorpay: read %s response: %w", action, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &razorpayError{status: resp.StatusCode}
		var e struct {
			Error struct {
				Code        string `json:"code"`
				Description string `json:"description"`
			} `json:"error"`
		}
		if json.Unmarshal(respBody, &e) == nil {
			apiErr.code, apiErr.description = e.Error.Code, e.Error.Description
		}
		return fmt.Errorf("razorpay: %s: %w", action, apiErr)
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("razorpay: decode %s response: %w", action, err)
	}
	return nil
}
