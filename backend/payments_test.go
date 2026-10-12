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
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
	"github.com/AyushV241/BadmintonPro/backend/internal/payments"
)

const testWebhookSecret = "whsec_test"

// testProvider is the fake provider with hooks: it records refunds and can
// fail them, and can make fetched payments look authorized, failed or for
// another amount.
type testProvider struct {
	*payments.Fake

	mu          sync.Mutex
	refunds     []string // provider payment IDs refunded
	failRefunds bool
	captures    int
	// status and amount, when set, replace what FetchPayment reports until
	// a capture.
	status string
	amount int
}

func newTestProvider() *testProvider {
	return &testProvider{Fake: payments.NewFake().WithWebhookSecret(testWebhookSecret)}
}

func (p *testProvider) FetchPayment(ctx context.Context, id string) (payments.Payment, error) {
	pay, err := p.Fake.FetchPayment(ctx, id)
	if err != nil {
		return pay, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status != "" {
		pay.Status = p.status
	}
	if p.amount != 0 {
		pay.AmountPaise = p.amount
	}
	return pay, nil
}

func (p *testProvider) Capture(ctx context.Context, id string, amount int, currency string) (payments.Payment, error) {
	p.mu.Lock()
	p.captures++
	p.status = ""
	p.mu.Unlock()
	return p.Fake.Capture(ctx, id, amount, currency)
}

func (p *testProvider) Refund(ctx context.Context, id string, amount int) (payments.Refund, error) {
	p.mu.Lock()
	p.refunds = append(p.refunds, id)
	fail := p.failRefunds
	p.mu.Unlock()
	if fail {
		return payments.Refund{}, errors.New("provider down")
	}
	return p.Fake.Refund(ctx, id, amount)
}

func (p *testProvider) refunded() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.refunds...)
}

// paymentsTestServer is entriesTestServer with payments through provider
// (nil disables them).
func paymentsTestServer(t *testing.T, provider payments.Provider) (*PostgresStore, *httptest.Server) {
	t.Helper()
	store := eventsTestStore(t)
	if _, err := store.pool.Exec(context.Background(),
		`TRUNCATE users, sessions, user_identities, event_judges, entries, payments CASCADE`); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewAPI(store, oauth.NewRegistry(), nil, false).
		WithEvents(store).WithAdmin(store).WithEntries(store).WithMatches(store).WithUserSearch(store).
		WithPayments(store, provider).Routes())
	t.Cleanup(srv.Close)
	return store, srv
}

// payResult is either answer of the pay endpoint.
type payResult struct {
	Checkout *Checkout `json:"checkout"`
	Entry    *Entry    `json:"entry"`
}

func joinEntry(t *testing.T, c adminClient, eventID string) Entry {
	t.Helper()
	var created entryResponse
	if code := join(c, eventID, singles, &created); code != http.StatusCreated {
		t.Fatalf("join: %d", code)
	}
	return created.Entry
}

func pay(c adminClient, entryID string) (payResult, int) {
	c.t.Helper()
	var out payResult
	code := c.do("POST", "/api/me/entries/"+entryID+"/pay", nil, &out)
	return out, code
}

// checkout pays for the entry and returns its order ID.
func checkout(t *testing.T, c adminClient, entryID string) string {
	t.Helper()
	out, code := pay(c, entryID)
	if code != http.StatusOK || out.Checkout == nil {
		t.Fatalf("pay: %d %+v", code, out)
	}
	return out.Checkout.OrderID
}

func verify(c adminClient, orderID, paymentID, signature string) (Entry, int) {
	c.t.Helper()
	var out entryResponse
	code := c.do("POST", "/api/payments/verify",
		map[string]string{"orderId": orderID, "paymentId": paymentID, "signature": signature}, &out)
	return out.Entry, code
}

// fakePay verifies a fake checkout payment for the order.
func fakePay(c adminClient, orderID, paymentID string) (Entry, int) {
	c.t.Helper()
	return verify(c, orderID, paymentID, payments.FakeSignature(orderID, paymentID))
}

func webhookBody(event, orderID, paymentID string, amount int) []byte {
	b, _ := json.Marshal(map[string]any{
		"entity": "event", "event": event, "contains": []string{"payment"},
		"payload": map[string]any{"payment": map[string]any{"entity": map[string]any{
			"id": paymentID, "order_id": orderID, "amount": amount, "currency": "INR", "status": "captured",
		}}},
	})
	return b
}

func signWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// sendWebhook posts a webhook body with a signature and returns the status
// and the reply's "status".
func sendWebhook(t *testing.T, srv *httptest.Server, body []byte, signature string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("POST", srv.URL+"/api/payments/webhook", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Razorpay-Signature", signature)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var reply map[string]string
	_ = json.NewDecoder(res.Body).Decode(&reply)
	return res.StatusCode, reply["status"]
}

func webhook(t *testing.T, srv *httptest.Server, event, orderID, paymentID string) (int, string) {
	t.Helper()
	body := webhookBody(event, orderID, paymentID, 5000)
	return sendWebhook(t, srv, body, signWebhook(testWebhookSecret, body))
}

// paymentRow is a payments row as the tests check it.
type paymentRow struct {
	Provider, Kind, Status       string
	OrderID, PaymentID, RefundID *string
	RecordedBy                   *string
	Amount                       int
	PaidAt, RefundedAt           *time.Time
}

func paymentRows(t *testing.T, store *PostgresStore, entryID string) []paymentRow {
	t.Helper()
	rows, err := store.pool.Query(context.Background(), `
		SELECT provider, kind, status, provider_order_id, provider_payment_id, refund_id, recorded_by,
			amount_paise, paid_at, refunded_at
		FROM payments WHERE entry_id = $1 ORDER BY created_at, id`, entryID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []paymentRow
	for rows.Next() {
		var r paymentRow
		if err := rows.Scan(&r.Provider, &r.Kind, &r.Status, &r.OrderID, &r.PaymentID, &r.RefundID, &r.RecordedBy,
			&r.Amount, &r.PaidAt, &r.RefundedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func statuses(rows []paymentRow) string {
	s := make([]string, len(rows))
	for i, r := range rows {
		s[i] = r.Status
	}
	return strings.Join(s, ",")
}

func entryStatus(t *testing.T, store *PostgresStore, entryID string) string {
	t.Helper()
	var s string
	if err := store.pool.QueryRow(context.Background(), `SELECT status FROM entries WHERE id = $1`, entryID).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func expireHold(t *testing.T, store *PostgresStore, entryID string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(),
		`UPDATE entries SET hold_expires_at = now() - interval '1 minute' WHERE id = $1`, entryID); err != nil {
		t.Fatal(err)
	}
}

func execSQL(t *testing.T, store *PostgresStore, sql string, args ...any) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// --- configuration (no database) ---

func TestNewPaymentProvider(t *testing.T) {
	t.Setenv("RAZORPAY_KEY_ID", "")
	t.Setenv("RAZORPAY_KEY_SECRET", "")
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", "")

	if p, err := newPaymentProvider("", false); err != nil || p != nil {
		t.Errorf("unset: %v, %v", p, err)
	}
	if p, err := newPaymentProvider("fake", false); err != nil || p == nil || p.Name() != "fake" {
		t.Errorf("fake: %v, %v", p, err)
	}
	if _, err := newPaymentProvider("fake", true); err == nil {
		t.Error("fake accepted with COOKIE_SECURE=true")
	}
	if _, err := newPaymentProvider("razorpay", false); err == nil {
		t.Error("razorpay accepted without keys")
	}
	if _, err := newPaymentProvider("stripe", false); err == nil {
		t.Error("unknown provider accepted")
	}
	t.Setenv("RAZORPAY_KEY_ID", "rzp_test_x")
	t.Setenv("RAZORPAY_KEY_SECRET", "not-a-real-secret")
	p, err := newPaymentProvider("razorpay", true)
	if err != nil || p.Name() != "razorpay" || p.KeyID() != "rzp_test_x" {
		t.Fatalf("razorpay: %v, %v", p, err)
	}
	// No webhook secret: webhooks are refused, not accepted unsigned.
	if err := p.VerifyWebhookSignature([]byte("{}"), ""); !errors.Is(err, payments.ErrWebhookDisabled) {
		t.Errorf("webhook without a secret: %v", err)
	}
}

// --- payments against Postgres ---

func TestPayEntry(t *testing.T) {
	provider := newTestProvider()
	store, srv := paymentsTestServer(t, provider)
	insertEvent(t, store, "evt_t_pay", testEvent{})
	asha, ashaUser := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")
	anon := adminClient{t: t, srv: srv}

	en := joinEntry(t, asha, "evt_t_pay")
	for _, route := range []string{"/api/me/entries/" + en.ID + "/pay", "/api/payments/verify"} {
		if code := anon.do("POST", route, map[string]string{}, nil); code != http.StatusUnauthorized {
			t.Errorf("%s without a session = %d", route, code)
		}
	}

	// The checkout, with exactly the keys the page reads.
	res, err := http.DefaultClient.Do(func() *http.Request {
		req, _ := http.NewRequest("POST", srv.URL+"/api/me/entries/"+en.ID+"/pay", nil)
		req.AddCookie(asha.cookie)
		return req
	}())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]map[string]any
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("pay: %d", res.StatusCode)
	}
	var keys []string
	for k := range raw["checkout"] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "amountPaise,currency,description,keyId,name,orderId,prefill,provider" {
		t.Errorf("checkout keys = %v", keys)
	}

	out, code := pay(asha, en.ID)
	if code != http.StatusOK || out.Checkout == nil || out.Entry != nil {
		t.Fatalf("pay: %d %+v", code, out)
	}
	co := *out.Checkout
	want := Checkout{
		Provider: "fake", KeyID: "", OrderID: co.OrderID, AmountPaise: 5000, Currency: "INR",
		Name: "BadmintonPro", Description: "Event evt_t_pay · 1 entry",
		Prefill: CheckoutPrefill{Name: "Player asha", Email: "", Contact: ashaUser.Phone},
	}
	if co != want || !strings.HasPrefix(co.OrderID, "order_fake_") || ashaUser.Phone == "" {
		t.Errorf("checkout = %+v", co)
	}
	// Paying again reuses the order (the first call above made it).
	if rows := paymentRows(t, store, en.ID); len(rows) != 1 || rows[0].Status != PaymentCreated ||
		*rows[0].OrderID != co.OrderID || rows[0].Kind != PaymentOnline || rows[0].Provider != "fake" || rows[0].Amount != 5000 {
		t.Errorf("rows = %+v", rows)
	}

	// Not yours, not there.
	if _, code := pay(ravi, en.ID); code != http.StatusNotFound {
		t.Errorf("someone else's entry: %d", code)
	}
	if _, code := pay(asha, "ent_nope"); code != http.StatusNotFound {
		t.Errorf("unknown entry: %d", code)
	}

	// A new fee means a new order; the old one is retired.
	execSQL(t, store, `UPDATE events SET fee_paise = 6000 WHERE id = 'evt_t_pay'`)
	out, _ = pay(asha, en.ID)
	if out.Checkout == nil || out.Checkout.OrderID == co.OrderID || out.Checkout.AmountPaise != 6000 {
		t.Errorf("after a fee change: %+v", out.Checkout)
	}
	if s := statuses(paymentRows(t, store, en.ID)); s != "failed,created" {
		t.Errorf("statuses = %s", s)
	}

	// Cancelling closes the open order, and the entry can't be paid for.
	if code := asha.do("DELETE", "/api/me/entries/"+en.ID, nil, nil); code != http.StatusOK {
		t.Fatalf("cancel: %d", code)
	}
	if s := statuses(paymentRows(t, store, en.ID)); s != "failed,failed" {
		t.Errorf("after cancel = %s", s)
	}
	if _, code := pay(asha, en.ID); code != http.StatusConflict {
		t.Errorf("cancelled entry: %d", code)
	}

	// An expired hold can't start a payment.
	stale, err := store.CreateEntry(context.Background(), EntryInput{
		EventID: "evt_t_pay", UserID: ashaUser.ID, Formats: []string{FormatSingles}, Now: time.Now().Add(-11 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, code := pay(asha, stale.ID); code != http.StatusConflict {
		t.Errorf("expired hold: %d", code)
	}

	// Nor can a cancelled event's.
	held := joinEntry(t, ravi, "evt_t_pay")
	execSQL(t, store, `UPDATE events SET status = 'cancelled' WHERE id = 'evt_t_pay'`)
	if _, code := pay(ravi, held.ID); code != http.StatusConflict {
		t.Errorf("cancelled event: %d", code)
	}
	if len(provider.refunded()) != 0 {
		t.Errorf("refunds = %v", provider.refunded())
	}
}

func TestPayFreeEntry(t *testing.T) {
	store, srv := paymentsTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_free", testEvent{})
	execSQL(t, store, `UPDATE events SET fee_paise = 0 WHERE id = 'evt_t_free'`)
	asha, _ := player(t, store, srv, 1, "asha")

	en := joinEntry(t, asha, "evt_t_free")
	out, code := pay(asha, en.ID)
	if code != http.StatusOK || out.Checkout != nil || out.Entry == nil || out.Entry.Status != EntryConfirmed || out.Entry.ID != en.ID {
		t.Fatalf("pay a free entry: %d %+v", code, out)
	}
	if rows := paymentRows(t, store, en.ID); len(rows) != 0 {
		t.Errorf("free entry has payments: %+v", rows)
	}
	if _, code := pay(asha, en.ID); code != http.StatusConflict {
		t.Errorf("pay twice: %d", code)
	}
}

func TestPaymentsDisabled(t *testing.T) {
	store, srv := paymentsTestServer(t, nil)
	insertEvent(t, store, "evt_t_off", testEvent{})
	insertEvent(t, store, "evt_t_off_free", testEvent{})
	execSQL(t, store, `UPDATE events SET fee_paise = 0 WHERE id = 'evt_t_off_free'`)
	asha, _ := player(t, store, srv, 1, "asha")

	en := joinEntry(t, asha, "evt_t_off")
	if _, code := pay(asha, en.ID); code != http.StatusServiceUnavailable {
		t.Errorf("pay: %d", code)
	}
	if _, code := fakePay(asha, "order_fake_1", "pay_fake_1"); code != http.StatusServiceUnavailable {
		t.Errorf("verify: %d", code)
	}
	if code, _ := webhook(t, srv, payments.EventPaymentCaptured, "order_fake_1", "pay_fake_1"); code != http.StatusServiceUnavailable {
		t.Errorf("webhook: %d", code)
	}
	// A free event needs no provider.
	free := joinEntry(t, asha, "evt_t_off_free")
	if out, code := pay(asha, free.ID); code != http.StatusOK || out.Entry == nil || out.Entry.Status != EntryConfirmed {
		t.Errorf("free entry: %d %+v", code, out)
	}
}

// The fake provider end to end: join, pay, the fake checkout's verify, and
// the webhook arriving after.
func TestVerifyConfirmsEntry(t *testing.T) {
	provider := newTestProvider()
	store, srv := paymentsTestServer(t, provider)
	insertEvent(t, store, "evt_t_verify", testEvent{})
	asha, _ := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")

	en := joinEntry(t, asha, "evt_t_verify")
	orderID := checkout(t, asha, en.ID)

	// Bad requests and signatures change nothing.
	for name, body := range map[string]any{
		"missing signature": map[string]string{"orderId": orderID, "paymentId": "pay_fake_1"},
		"unknown field":     map[string]string{"orderId": orderID, "paymentId": "pay_fake_1", "signature": "x", "amount": "1"},
		"not JSON":          "{",
	} {
		if code := asha.do("POST", "/api/payments/verify", body, nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
	for name, sig := range map[string]string{
		"wrong payment": payments.FakeSignature(orderID, "pay_fake_2"),
		"wrong order":   payments.FakeSignature("order_fake_x", "pay_fake_1"),
		"garbage":       "fake:",
	} {
		if _, code := verify(asha, orderID, "pay_fake_1", sig); code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
	// A valid signature for an order that isn't ours, or isn't the caller's.
	if _, code := fakePay(asha, "order_fake_unknown", "pay_fake_1"); code != http.StatusNotFound {
		t.Errorf("unknown order: %d", code)
	}
	if _, code := fakePay(ravi, orderID, "pay_fake_1"); code != http.StatusNotFound {
		t.Errorf("someone else's order: %d", code)
	}
	if s := entryStatus(t, store, en.ID); s != EntryPendingPayment {
		t.Fatalf("entry = %s before paying", s)
	}

	before := time.Now()
	got, code := fakePay(asha, orderID, "pay_fake_1")
	if code != http.StatusOK || got.ID != en.ID || got.Status != EntryConfirmed || got.Event.ID != "evt_t_verify" {
		t.Fatalf("verify: %d %+v", code, got)
	}
	rows := paymentRows(t, store, en.ID)
	if len(rows) != 1 || rows[0].Status != PaymentPaid || rows[0].PaymentID == nil || *rows[0].PaymentID != "pay_fake_1" ||
		rows[0].PaidAt == nil || rows[0].PaidAt.Before(before.Add(-time.Second)) {
		t.Errorf("rows = %+v", rows)
	}
	var confirmedAt *time.Time
	if err := store.pool.QueryRow(context.Background(), `SELECT confirmed_at FROM entries WHERE id = $1`, en.ID).Scan(&confirmedAt); err != nil || confirmedAt == nil {
		t.Errorf("confirmed_at = %v, %v", confirmedAt, err)
	}

	// Verify again, then the webhook: nothing changes.
	if got, code := fakePay(asha, orderID, "pay_fake_1"); code != http.StatusOK || got.Status != EntryConfirmed {
		t.Errorf("verify twice: %d %+v", code, got)
	}
	for _, event := range []string{payments.EventOrderPaid, payments.EventPaymentCaptured, payments.EventPaymentCaptured} {
		if code, status := webhook(t, srv, event, orderID, "pay_fake_1"); code != http.StatusOK || status != "already applied" {
			t.Errorf("webhook %s after verify: %d %q", event, code, status)
		}
	}
	if s := statuses(paymentRows(t, store, en.ID)); s != "paid" {
		t.Errorf("statuses = %s", s)
	}
	var list entriesResponse
	asha.do("GET", "/api/me/entries", nil, &list)
	if len(list.Entries) != 1 || list.Entries[0].Status != EntryConfirmed {
		t.Errorf("entries = %+v", list.Entries)
	}
	if _, code := pay(asha, en.ID); code != http.StatusConflict {
		t.Errorf("pay a confirmed entry: %d", code)
	}
	if len(provider.refunded()) != 0 {
		t.Errorf("refunds = %v", provider.refunded())
	}
}

func TestWebhookConfirmsEntry(t *testing.T) {
	provider := newTestProvider()
	store, srv := paymentsTestServer(t, provider)
	insertEvent(t, store, "evt_t_hook", testEvent{})
	asha, _ := player(t, store, srv, 1, "asha")

	en := joinEntry(t, asha, "evt_t_hook")
	orderID := checkout(t, asha, en.ID)
	provider.RecordPayment(orderID, "pay_fake_w")
	body := webhookBody(payments.EventPaymentCaptured, orderID, "pay_fake_w", 5000)

	// Signatures: wrong secret, tampered body, none.
	for name, sig := range map[string]string{
		"wrong secret": signWebhook("other", body),
		"no signature": "",
		"other body":   signWebhook(testWebhookSecret, webhookBody(payments.EventPaymentCaptured, orderID, "pay_fake_w", 1)),
	} {
		if code, _ := sendWebhook(t, srv, body, sig); code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code, _ := sendWebhook(t, srv, bytes.Repeat([]byte(" "), maxWebhookBytes+1), "x"); code != http.StatusBadRequest {
		t.Errorf("huge body: %d", code)
	}

	// Ignored, with 200 so the provider stops retrying.
	ignored := map[string][]byte{
		"other event":     []byte(`{"event":"refund.processed","payload":{"refund":{"entity":{"id":"rfnd_1"}}}}`),
		"unknown order":   webhookBody(payments.EventPaymentCaptured, "order_fake_unknown", "pay_fake_u", 5000),
		"other amount":    webhookBody(payments.EventPaymentCaptured, orderID, "pay_fake_w", 4000),
		"no order":        webhookBody(payments.EventPaymentCaptured, "", "pay_fake_w", 5000),
		"payment.failed":  webhookBody("payment.failed", orderID, "pay_fake_w", 5000),
		"no payment data": []byte(`{"event":"order.paid","payload":{}}`),
	}
	for name, b := range ignored {
		if code, status := sendWebhook(t, srv, b, signWebhook(testWebhookSecret, b)); code != http.StatusOK || status != "ignored" {
			t.Errorf("%s: %d %q", name, code, status)
		}
	}
	if s := entryStatus(t, store, en.ID); s != EntryPendingPayment {
		t.Fatalf("entry = %s after ignored webhooks", s)
	}

	if code, status := sendWebhook(t, srv, body, signWebhook(testWebhookSecret, body)); code != http.StatusOK || status != "applied" {
		t.Fatalf("webhook: %d %q", code, status)
	}
	if s := entryStatus(t, store, en.ID); s != EntryConfirmed {
		t.Errorf("entry = %s", s)
	}
	// Repeats and the late verify change nothing.
	if code, status := webhook(t, srv, payments.EventOrderPaid, orderID, "pay_fake_w"); code != http.StatusOK || status != "already applied" {
		t.Errorf("order.paid after: %d %q", code, status)
	}
	if got, code := fakePay(asha, orderID, "pay_fake_w"); code != http.StatusOK || got.Status != EntryConfirmed {
		t.Errorf("verify after the webhook: %d %+v", code, got)
	}
	if s := statuses(paymentRows(t, store, en.ID)); s != "paid" {
		t.Errorf("statuses = %s", s)
	}

	// Without a webhook secret the webhook is off.
	_, noHook := paymentsTestServer(t, payments.NewFake())
	if code, _ := sendWebhook(t, noHook, body, signWebhook(testWebhookSecret, body)); code != http.StatusServiceUnavailable {
		t.Errorf("no secret: %d", code)
	}
}

func TestVerifyChecksProviderPayment(t *testing.T) {
	provider := newTestProvider()
	store, srv := paymentsTestServer(t, provider)
	insertEvent(t, store, "evt_t_check", testEvent{})
	asha, _ := player(t, store, srv, 1, "asha")
	en := joinEntry(t, asha, "evt_t_check")
	orderID := checkout(t, asha, en.ID)

	// Failed at the provider, or for less: unusable.
	provider.status = payments.StatusFailed
	if _, code := fakePay(asha, orderID, "pay_fake_1"); code != http.StatusConflict {
		t.Errorf("failed payment: %d", code)
	}
	provider.status, provider.amount = "", 100
	if _, code := fakePay(asha, orderID, "pay_fake_1"); code != http.StatusConflict {
		t.Errorf("short payment: %d", code)
	}
	provider.amount = 0
	if s := entryStatus(t, store, en.ID); s != EntryPendingPayment {
		t.Fatalf("entry = %s", s)
	}

	// Authorized: captured, then confirmed.
	provider.status = payments.StatusAuthorized
	if got, code := fakePay(asha, orderID, "pay_fake_1"); code != http.StatusOK || got.Status != EntryConfirmed {
		t.Errorf("authorized payment: %d %+v", code, got)
	}
	if provider.captures != 1 {
		t.Errorf("captures = %d", provider.captures)
	}
}

func TestPaymentAfterHoldExpired(t *testing.T) {
	provider := newTestProvider()
	store, srv := paymentsTestServer(t, provider)
	insertEvent(t, store, "evt_t_late", testEvent{capacity: 2})
	asha, _ := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")
	kiran, _ := player(t, store, srv, 3, "kiran")

	// Asha's payment lands after her hold ran out, but there's room.
	en := joinEntry(t, asha, "evt_t_late")
	orderID := checkout(t, asha, en.ID)
	expireHold(t, store, en.ID)
	if got, code := fakePay(asha, orderID, "pay_fake_a"); code != http.StatusOK || got.Status != EntryConfirmed {
		t.Errorf("late with room: %d %+v", code, got)
	}

	// Ravi's lands after Kiran took the last place: refunded.
	late := joinEntry(t, ravi, "evt_t_late")
	lateOrder := checkout(t, ravi, late.ID)
	expireHold(t, store, late.ID)
	joinEntry(t, kiran, "evt_t_late")
	got, code := fakePay(ravi, lateOrder, "pay_fake_r")
	if code != http.StatusOK || got.Status != EntryRefunded {
		t.Fatalf("late without room: %d %+v", code, got)
	}
	rows := paymentRows(t, store, late.ID)
	if len(rows) != 1 || rows[0].Status != PaymentRefunded || rows[0].RefundID == nil ||
		!strings.HasPrefix(*rows[0].RefundID, "rfnd_fake_") || rows[0].RefundedAt == nil {
		t.Errorf("rows = %+v", rows)
	}
	if r := provider.refunded(); len(r) != 1 || r[0] != "pay_fake_r" {
		t.Errorf("refunds = %v", r)
	}
	// The webhook for it changes nothing, and refunds nothing again.
	if code, status := webhook(t, srv, payments.EventPaymentCaptured, lateOrder, "pay_fake_r"); code != http.StatusOK || status != "already applied" {
		t.Errorf("webhook: %d %q", code, status)
	}
	if len(provider.refunded()) != 1 {
		t.Errorf("refunds = %v", provider.refunded())
	}
}

func TestRefundFailureLeavesRefundPending(t *testing.T) {
	provider := newTestProvider()
	provider.failRefunds = true
	store, srv := paymentsTestServer(t, provider)
	insertEvent(t, store, "evt_t_rfail", testEvent{})
	asha, _ := player(t, store, srv, 1, "asha")

	// The event is called off while Asha pays.
	en := joinEntry(t, asha, "evt_t_rfail")
	orderID := checkout(t, asha, en.ID)
	execSQL(t, store, `UPDATE events SET status = 'cancelled' WHERE id = 'evt_t_rfail'`)
	if got, code := fakePay(asha, orderID, "pay_fake_1"); code != http.StatusOK || got.Status != EntryRefunded {
		t.Errorf("verify: %d %+v", code, got)
	}
	rows := paymentRows(t, store, en.ID)
	if len(rows) != 1 || rows[0].Status != PaymentRefundPending || rows[0].RefundID != nil || rows[0].PaidAt == nil {
		t.Errorf("rows = %+v", rows)
	}
	if len(provider.refunded()) != 1 {
		t.Errorf("refund attempts = %v", provider.refunded())
	}
}

func TestPaymentAfterCancel(t *testing.T) {
	provider := newTestProvider()
	store, srv := paymentsTestServer(t, provider)
	insertEvent(t, store, "evt_t_cancelpay", testEvent{})
	asha, _ := player(t, store, srv, 1, "asha")

	// Asha cancels in one tab while paying in another.
	en := joinEntry(t, asha, "evt_t_cancelpay")
	orderID := checkout(t, asha, en.ID)
	if code := asha.do("DELETE", "/api/me/entries/"+en.ID, nil, nil); code != http.StatusOK {
		t.Fatalf("cancel: %d", code)
	}
	provider.RecordPayment(orderID, "pay_fake_1")
	if code, status := webhook(t, srv, payments.EventPaymentCaptured, orderID, "pay_fake_1"); code != http.StatusOK || status != "applied" {
		t.Fatalf("webhook: %d %q", code, status)
	}
	if s := entryStatus(t, store, en.ID); s != EntryRefunded {
		t.Errorf("entry = %s", s)
	}
	if s := statuses(paymentRows(t, store, en.ID)); s != "refunded" {
		t.Errorf("statuses = %s", s)
	}
	if r := provider.refunded(); len(r) != 1 || r[0] != "pay_fake_1" {
		t.Errorf("refunds = %v", r)
	}
}

func TestDoublePaymentRefunded(t *testing.T) {
	provider := newTestProvider()
	store, srv := paymentsTestServer(t, provider)
	insertEvent(t, store, "evt_t_double", testEvent{})
	asha, _ := player(t, store, srv, 1, "asha")
	ravi, _ := player(t, store, srv, 2, "ravi")
	admin, adminUser := player(t, store, srv, 99, "desk")

	// Two orders for one entry (the fee changed in between), both paid.
	en := joinEntry(t, asha, "evt_t_double")
	first := checkout(t, asha, en.ID)
	execSQL(t, store, `UPDATE events SET fee_paise = 6000 WHERE id = 'evt_t_double'`)
	second := checkout(t, asha, en.ID)
	if got, code := fakePay(asha, second, "pay_fake_2"); code != http.StatusOK || got.Status != EntryConfirmed {
		t.Fatalf("second order: %d %+v", code, got)
	}
	if got, code := fakePay(asha, first, "pay_fake_1"); code != http.StatusOK || got.Status != EntryConfirmed {
		t.Errorf("first order, paid late: %d %+v", code, got)
	}
	if s := statuses(paymentRows(t, store, en.ID)); s != "refunded,paid" {
		t.Errorf("statuses = %s", s)
	}
	if r := provider.refunded(); len(r) != 1 || r[0] != "pay_fake_1" {
		t.Errorf("refunds = %v", r)
	}

	// Paid at the desk, then online as well.
	desk := joinEntry(t, ravi, "evt_t_double")
	order := checkout(t, ravi, desk.ID)
	if code := admin.do("POST", "/api/admin/entries/"+desk.ID+"/mark-paid", nil, nil); code != http.StatusOK {
		t.Fatalf("mark paid: %d", code)
	}
	if got, code := fakePay(ravi, order, "pay_fake_3"); code != http.StatusOK || got.Status != EntryConfirmed {
		t.Errorf("online after the desk: %d %+v", code, got)
	}
	rows := paymentRows(t, store, desk.ID)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	online, offline := rows[0], rows[1]
	if online.Status != PaymentRefunded || offline.Status != PaymentPaid || offline.Kind != PaymentOffline ||
		offline.Provider != ProviderOffline || offline.RecordedBy == nil || *offline.RecordedBy != adminUser.ID ||
		offline.Amount != 6000 || offline.OrderID != nil {
		t.Errorf("online %+v, offline %+v", online, offline)
	}
	if len(provider.refunded()) != 2 {
		t.Errorf("refunds = %v", provider.refunded())
	}

	// A second payment against an order already paid gets its own row.
	if got, code := fakePay(asha, second, "pay_fake_4"); code != http.StatusOK || got.Status != EntryConfirmed {
		t.Errorf("second payment on one order: %d %+v", code, got)
	}
	rows = paymentRows(t, store, en.ID)
	if s := statuses(rows); s != "refunded,paid,refunded" || rows[2].OrderID != nil || *rows[2].PaymentID != "pay_fake_4" {
		t.Errorf("rows = %+v", rows)
	}
	// ...and repeating it changes nothing.
	if _, code := fakePay(asha, second, "pay_fake_4"); code != http.StatusOK || len(provider.refunded()) != 3 {
		t.Errorf("repeat: %d, refunds %v", code, provider.refunded())
	}
}

func TestMarkEntryPaidRecordsPayment(t *testing.T) {
	store, srv := paymentsTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_desk_pay", testEvent{})
	insertEvent(t, store, "evt_t_desk_free", testEvent{})
	execSQL(t, store, `UPDATE events SET fee_paise = 0 WHERE id = 'evt_t_desk_free'`)
	admin, adminUser := player(t, store, srv, 99, "desk")
	asha, _ := player(t, store, srv, 1, "asha")

	en := joinEntry(t, asha, "evt_t_desk_pay")
	if code := admin.do("POST", "/api/admin/entries/"+en.ID+"/mark-paid", nil, nil); code != http.StatusOK {
		t.Fatalf("mark paid: %d", code)
	}
	rows := paymentRows(t, store, en.ID)
	if len(rows) != 1 || rows[0].Status != PaymentPaid || rows[0].Provider != ProviderOffline || rows[0].Kind != PaymentOffline ||
		rows[0].Amount != 5000 || rows[0].RecordedBy == nil || *rows[0].RecordedBy != adminUser.ID || rows[0].PaidAt == nil {
		t.Errorf("rows = %+v", rows)
	}
	free := joinEntry(t, asha, "evt_t_desk_free")
	if code := admin.do("POST", "/api/admin/entries/"+free.ID+"/mark-paid", nil, nil); code != http.StatusOK {
		t.Fatalf("mark a free entry paid: %d", code)
	}
	if rows := paymentRows(t, store, free.ID); len(rows) != 0 {
		t.Errorf("free entry rows = %+v", rows)
	}
}

// The page's verify and the webhooks for one payment racing: exactly one
// of them confirms, the rest find it already applied.
func TestVerifyAndWebhookRace(t *testing.T) {
	provider := newTestProvider()
	store, srv := paymentsTestServer(t, provider)
	insertEvent(t, store, "evt_t_race_pay", testEvent{capacity: 100, maxMatches: 50})

	for round := range 5 {
		c, _ := player(t, store, srv, round+1, fmt.Sprintf("racer%d", round))
		en := joinEntry(t, c, "evt_t_race_pay")
		orderID := checkout(t, c, en.ID)
		paymentID := fmt.Sprintf("pay_fake_race%d", round)
		provider.RecordPayment(orderID, paymentID)

		var wg sync.WaitGroup
		var mu sync.Mutex
		var codes []int
		var applied int
		for i := range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i == 0 {
					_, code := fakePay(c, orderID, paymentID)
					mu.Lock()
					codes = append(codes, code)
					mu.Unlock()
					return
				}
				event := payments.EventPaymentCaptured
				if i%2 == 0 {
					event = payments.EventOrderPaid
				}
				code, status := webhook(t, srv, event, orderID, paymentID)
				mu.Lock()
				codes = append(codes, code)
				if status == "applied" {
					applied++
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
		for _, code := range codes {
			if code != http.StatusOK {
				t.Errorf("round %d: codes = %v", round, codes)
				break
			}
		}
		if applied > 1 {
			t.Errorf("round %d: %d webhooks applied the payment", round, applied)
		}
		if s := statuses(paymentRows(t, store, en.ID)); s != "paid" {
			t.Errorf("round %d: statuses = %s", round, s)
		}
		if s := entryStatus(t, store, en.ID); s != EntryConfirmed {
			t.Errorf("round %d: entry = %s", round, s)
		}
	}
	if len(provider.refunded()) != 0 {
		t.Errorf("refunds = %v", provider.refunded())
	}

	// The same at the store: of many confirmations of one payment, exactly
	// one isn't a repeat.
	c, _ := player(t, store, srv, 50, "storeracer")
	en := joinEntry(t, c, "evt_t_race_pay")
	orderID := checkout(t, c, en.ID)
	var wg sync.WaitGroup
	results := make([]PaymentOutcome, 10)
	errs := make([]error, 10)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = store.ConfirmPayment(context.Background(), PaymentConfirmation{
				Provider: "fake", OrderID: orderID, PaymentID: "pay_fake_store", AmountPaise: 5000, Currency: "INR", Now: time.Now(),
			})
		}()
	}
	wg.Wait()
	firsts := 0
	for i, out := range results {
		if errs[i] != nil {
			t.Fatalf("confirm %d: %v", i, errs[i])
		}
		if out.Refund != nil || out.Entry.Status != EntryConfirmed {
			t.Errorf("confirm %d: %+v", i, out)
		}
		if !out.Repeat {
			firsts++
		}
	}
	if firsts != 1 {
		t.Errorf("%d confirmations applied the payment", firsts)
	}
}

func TestConfirmPaymentErrors(t *testing.T) {
	store, srv := paymentsTestServer(t, newTestProvider())
	insertEvent(t, store, "evt_t_confirm", testEvent{})
	asha, ashaUser := player(t, store, srv, 1, "asha")
	ctx := context.Background()
	en := joinEntry(t, asha, "evt_t_confirm")
	orderID := checkout(t, asha, en.ID)

	if _, err := store.ConfirmPayment(ctx, PaymentConfirmation{
		Provider: "fake", OrderID: "order_fake_nope", PaymentID: "pay_fake_1", AmountPaise: 5000, Currency: "INR", Now: time.Now(),
	}); !errors.Is(err, ErrNoSuchPayment) {
		t.Errorf("unknown order: %v", err)
	}
	if _, err := store.ConfirmPayment(ctx, PaymentConfirmation{
		Provider: "razorpay", OrderID: orderID, PaymentID: "pay_fake_1", AmountPaise: 5000, Currency: "INR", Now: time.Now(),
	}); !errors.Is(err, ErrNoSuchPayment) {
		t.Errorf("another provider's order: %v", err)
	}
	if _, err := store.ConfirmPayment(ctx, PaymentConfirmation{
		Provider: "fake", OrderID: orderID, PaymentID: "pay_fake_1", AmountPaise: 4999, Currency: "INR", Now: time.Now(),
	}); !errors.Is(err, ErrPaymentMismatch) {
		t.Errorf("short amount: %v", err)
	}
	// A free confirmation for an event with a fee.
	if _, err := store.ConfirmPayment(ctx, PaymentConfirmation{EntryID: en.ID, UserID: ashaUser.ID, Now: time.Now()}); !errors.Is(err, ErrPaymentMismatch) {
		t.Errorf("free confirmation of a paid event: %v", err)
	}
	if s := entryStatus(t, store, en.ID); s != EntryPendingPayment {
		t.Errorf("entry = %s", s)
	}
}
