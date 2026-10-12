package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	testKeyID         = "rzp_test_abc"
	testKeySecret     = "key-secret"
	testWebhookSecret = "webhook-secret"
)

// fakeRazorpayAPI imitates the Razorpay endpoints the adapter uses, closely
// enough to check what it sends and how it reads the answers.
type fakeRazorpayAPI struct {
	t *testing.T

	mu       sync.Mutex
	requests []string // "METHOD /path"
	bodies   []map[string]any
	payments map[string]map[string]any
	// forceStatus, when set, answers every request with this status and a
	// Razorpay error body.
	forceStatus int
}

func newFakeRazorpayAPI(t *testing.T) (*fakeRazorpayAPI, *Razorpay) {
	t.Helper()
	api := &fakeRazorpayAPI{t: t, payments: map[string]map[string]any{}}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	rp, err := NewRazorpay(testKeyID, testKeySecret, testWebhookSecret)
	if err != nil {
		t.Fatal(err)
	}
	return api, rp.WithBaseURL(srv.URL + "/")
}

func razorpayErr(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "description": description}})
}

func (f *fakeRazorpayAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	if !ok || user != testKeyID || pass != testKeySecret {
		razorpayErr(w, http.StatusUnauthorized, "BAD_REQUEST_ERROR", "Authentication failed")
		return
	}
	var body map[string]any
	if r.Method == http.MethodPost {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			razorpayErr(w, http.StatusBadRequest, "BAD_REQUEST_ERROR", "want JSON, got "+ct)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			razorpayErr(w, http.StatusBadRequest, "BAD_REQUEST_ERROR", "bad JSON")
			return
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.bodies = append(f.bodies, body)
	if f.forceStatus != 0 {
		razorpayErr(w, f.forceStatus, "SERVER_ERROR", "forced")
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/orders":
		writeTestJSON(w, map[string]any{
			"id": "order_TEST1", "entity": "order", "amount": body["amount"], "currency": body["currency"],
			"receipt": body["receipt"], "status": "created", "attempts": 0,
		})
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "payments":
		p, ok := f.payments[parts[1]]
		if !ok {
			razorpayErr(w, http.StatusBadRequest, "BAD_REQUEST_ERROR", "The id provided does not exist")
			return
		}
		writeTestJSON(w, p)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "payments" && parts[2] == "capture":
		p, ok := f.payments[parts[1]]
		switch {
		case !ok:
			razorpayErr(w, http.StatusBadRequest, "BAD_REQUEST_ERROR", "The id provided does not exist")
		case p["status"] != "authorized":
			razorpayErr(w, http.StatusBadRequest, "BAD_REQUEST_ERROR", "This payment has already been captured")
		case p["amount"] != body["amount"]:
			razorpayErr(w, http.StatusBadRequest, "BAD_REQUEST_ERROR", "Capture amount must be equal to the amount authorized")
		default:
			p["status"] = "captured"
			writeTestJSON(w, p)
		}
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "payments" && parts[2] == "refund":
		if _, ok := f.payments[parts[1]]; !ok {
			razorpayErr(w, http.StatusBadRequest, "BAD_REQUEST_ERROR", "The id provided does not exist")
			return
		}
		writeTestJSON(w, map[string]any{"id": "rfnd_TEST1", "entity": "refund", "amount": body["amount"], "status": "processed"})
	default:
		razorpayErr(w, http.StatusNotFound, "NOT_FOUND", "no route "+r.Method+" "+r.URL.Path)
	}
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestNewRazorpayRequiresKeys(t *testing.T) {
	for _, keys := range [][2]string{{"", "s"}, {"k", ""}} {
		if _, err := NewRazorpay(keys[0], keys[1], ""); err == nil {
			t.Errorf("NewRazorpay(%q, %q) accepted", keys[0], keys[1])
		}
	}
}

func TestRazorpayCreateOrder(t *testing.T) {
	api, rp := newFakeRazorpayAPI(t)
	if rp.Name() != "razorpay" || rp.KeyID() != testKeyID {
		t.Errorf("name %q, key ID %q", rp.Name(), rp.KeyID())
	}
	order, err := rp.CreateOrder(context.Background(), OrderRequest{
		AmountPaise: 5000, Currency: "INR", Receipt: "ent_123", Notes: map[string]string{"entry_id": "ent_123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if order != (Order{ID: "order_TEST1", AmountPaise: 5000, Currency: "INR", Status: "created"}) {
		t.Errorf("order = %+v", order)
	}
	if got := api.requests; len(got) != 1 || got[0] != "POST /orders" {
		t.Fatalf("requests = %v", got)
	}
	body := api.bodies[0]
	// JSON numbers decode as float64: 5000 must be sent as a whole number.
	if body["amount"] != float64(5000) || body["currency"] != "INR" || body["receipt"] != "ent_123" {
		t.Errorf("body = %v", body)
	}
	if notes, _ := body["notes"].(map[string]any); notes["entry_id"] != "ent_123" {
		t.Errorf("notes = %v", body["notes"])
	}
}

func TestRazorpayFetchCaptureAndRefund(t *testing.T) {
	api, rp := newFakeRazorpayAPI(t)
	ctx := context.Background()
	api.payments["pay_A"] = map[string]any{
		"id": "pay_A", "entity": "payment", "order_id": "order_TEST1", "amount": float64(5000), "currency": "INR",
		"status": "authorized", "method": "upi",
	}

	p, err := rp.FetchPayment(ctx, "pay_A")
	if err != nil {
		t.Fatal(err)
	}
	if p != (Payment{ID: "pay_A", OrderID: "order_TEST1", AmountPaise: 5000, Currency: "INR", Status: StatusAuthorized}) {
		t.Errorf("payment = %+v", p)
	}

	captured, err := rp.Capture(ctx, "pay_A", 5000, "INR")
	if err != nil {
		t.Fatal(err)
	}
	if captured.Status != StatusCaptured {
		t.Errorf("captured = %+v", captured)
	}
	if body := api.bodies[1]; body["amount"] != float64(5000) || body["currency"] != "INR" {
		t.Errorf("capture body = %v", body)
	}
	// Capturing twice is Razorpay's 400, an error.
	if _, err := rp.Capture(ctx, "pay_A", 5000, "INR"); err == nil || !strings.Contains(err.Error(), "already been captured") {
		t.Errorf("second capture: err = %v", err)
	}

	refund, err := rp.Refund(ctx, "pay_A", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if refund != (Refund{ID: "rfnd_TEST1", Status: "processed"}) {
		t.Errorf("refund = %+v", refund)
	}
	if body := api.bodies[3]; body["amount"] != float64(5000) {
		t.Errorf("refund body = %v", body)
	}
	want := []string{"GET /payments/pay_A", "POST /payments/pay_A/capture", "POST /payments/pay_A/capture", "POST /payments/pay_A/refund"}
	if strings.Join(api.requests, ",") != strings.Join(want, ",") {
		t.Errorf("requests = %v", api.requests)
	}

	// An unknown payment is ErrNotFound, and IDs can't escape their path.
	if _, err := rp.FetchPayment(ctx, "pay_nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown payment: err = %v", err)
	}
	if _, err := rp.FetchPayment(ctx, "../orders"); !errors.Is(err, ErrNotFound) {
		t.Errorf("path escape: err = %v", err)
	}
	if last := api.requests[len(api.requests)-1]; last != "GET /payments/../orders" {
		t.Errorf("escaped request = %q", last)
	}
}

func TestRazorpayErrors(t *testing.T) {
	ctx := context.Background()

	api, rp := newFakeRazorpayAPI(t)
	api.forceStatus = http.StatusInternalServerError
	_, err := rp.CreateOrder(ctx, OrderRequest{AmountPaise: 100, Currency: "INR", Receipt: "r"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500, Razorpay SERVER_ERROR: forced") {
		t.Errorf("500: err = %v", err)
	}
	// A server error on fetch is the provider failing, not "no such payment".
	if _, err := rp.FetchPayment(ctx, "pay_A"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("fetch 500: err = %v", err)
	}
	if _, err := rp.Refund(ctx, "pay_A", 100); err == nil {
		t.Error("refund 500 succeeded")
	}

	// Wrong credentials.
	_, good := newFakeRazorpayAPI(t)
	bad, _ := NewRazorpay(testKeyID, "wrong", "")
	bad.WithBaseURL(good.baseURL)
	if _, err := bad.CreateOrder(ctx, OrderRequest{AmountPaise: 100, Currency: "INR", Receipt: "r"}); err == nil ||
		!strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("bad key: err = %v", err)
	}

	// Unreachable, and a 2xx that isn't JSON.
	gone, _ := NewRazorpay(testKeyID, testKeySecret, "")
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	gone.WithBaseURL(dead.URL)
	if _, err := gone.FetchPayment(ctx, "pay_A"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("unreachable: err = %v", err)
	}
	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }))
	t.Cleanup(garbage.Close)
	gone.WithBaseURL(garbage.URL)
	if _, err := gone.CreateOrder(ctx, OrderRequest{AmountPaise: 100, Currency: "INR", Receipt: "r"}); err == nil {
		t.Error("non-JSON response accepted")
	}
}

func sign(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestRazorpayCheckoutSignature(t *testing.T) {
	rp, _ := NewRazorpay(testKeyID, testKeySecret, "")
	good := sign(testKeySecret, "order_1|pay_1")
	if err := rp.VerifyCheckoutSignature("order_1", "pay_1", good); err != nil {
		t.Errorf("good signature: %v", err)
	}
	if err := rp.VerifyCheckoutSignature("order_1", "pay_1", strings.ToUpper(good)); err != nil {
		t.Errorf("upper-case hex: %v", err)
	}
	flipped := []byte(good)
	if flipped[0] == '0' {
		flipped[0] = '1'
	} else {
		flipped[0] = '0'
	}
	for name, tc := range map[string][3]string{
		"another payment":      {"order_1", "pay_2", good},
		"another order":        {"order_2", "pay_1", good},
		"swapped":              {"pay_1", "order_1", good},
		"one digit changed":    {"order_1", "pay_1", string(flipped)},
		"truncated":            {"order_1", "pay_1", good[:62]},
		"extended":             {"order_1", "pay_1", good + "00"},
		"not hex":              {"order_1", "pay_1", strings.Repeat("z", 64)},
		"empty":                {"order_1", "pay_1", ""},
		"signed with the hook": {"order_1", "pay_1", sign(testWebhookSecret, "order_1|pay_1")},
		"fake provider's":      {"order_1", "pay_1", FakeSignature("order_1", "pay_1")},
	} {
		if err := rp.VerifyCheckoutSignature(tc[0], tc[1], tc[2]); !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

const capturedWebhook = `{"entity":"event","account_id":"acc_1","event":"payment.captured","contains":["payment"],
"payload":{"payment":{"entity":{"id":"pay_W","entity":"payment","amount":5000,"currency":"INR","status":"captured",
"order_id":"order_W","method":"upi","captured":true}}},"created_at":1700000000}`

func TestRazorpayWebhook(t *testing.T) {
	rp, _ := NewRazorpay(testKeyID, testKeySecret, testWebhookSecret)
	body := []byte(capturedWebhook)
	if err := rp.VerifyWebhookSignature(body, sign(testWebhookSecret, capturedWebhook)); err != nil {
		t.Errorf("good signature: %v", err)
	}
	tampered := []byte(strings.Replace(capturedWebhook, "5000", "50000", 1))
	for name, sig := range map[string]string{
		"key secret":   sign(testKeySecret, capturedWebhook),
		"empty":        "",
		"another body": sign(testWebhookSecret, string(tampered)),
	} {
		if err := rp.VerifyWebhookSignature(body, sig); !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := rp.VerifyWebhookSignature(tampered, sign(testWebhookSecret, capturedWebhook)); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("tampered body: err = %v", err)
	}

	noHook, _ := NewRazorpay(testKeyID, testKeySecret, "")
	if err := noHook.VerifyWebhookSignature(body, sign("", capturedWebhook)); !errors.Is(err, ErrWebhookDisabled) {
		t.Errorf("no secret: err = %v", err)
	}

	ev, err := rp.ParseWebhook(body)
	if err != nil {
		t.Fatal(err)
	}
	want := Payment{ID: "pay_W", OrderID: "order_W", AmountPaise: 5000, Currency: "INR", Status: StatusCaptured}
	if ev.Event != EventPaymentCaptured || ev.Payment == nil || *ev.Payment != want {
		t.Errorf("event = %+v, payment %+v", ev, ev.Payment)
	}
	orderPaid := `{"event":"order.paid","payload":{"payment":{"entity":{"id":"pay_W","order_id":"order_W","amount":5000,
"currency":"INR","status":"captured"}},"order":{"entity":{"id":"order_W","status":"paid"}}}}`
	if ev, err := rp.ParseWebhook([]byte(orderPaid)); err != nil || ev.Event != EventOrderPaid || ev.Payment == nil || *ev.Payment != want {
		t.Errorf("order.paid = %+v, %v", ev, err)
	}
	if ev, err := rp.ParseWebhook([]byte(`{"event":"refund.processed","payload":{"refund":{"entity":{"id":"rfnd_1"}}}}`)); err != nil ||
		ev.Event != "refund.processed" || ev.Payment != nil {
		t.Errorf("refund event = %+v, %v", ev, err)
	}
	if _, err := rp.ParseWebhook([]byte("not json")); err == nil {
		t.Error("garbage parsed")
	}
}
