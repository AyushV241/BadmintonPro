package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
	"github.com/AyushV241/BadmintonPro/backend/internal/otp"
)

// smsInbox stands in for the user's phone: the memory provider delivers
// codes here instead of sending them.
type smsInbox struct {
	mu    sync.Mutex
	codes map[string]string
}

func (in *smsInbox) deliver(phone, code string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.codes[phone] = code
}

func (in *smsInbox) code(t *testing.T, phone string) string {
	t.Helper()
	in.mu.Lock()
	defer in.mu.Unlock()
	code, ok := in.codes[phone]
	if !ok {
		t.Fatalf("no code was sent to %s", phone)
	}
	return code
}

type phoneTestOptions struct {
	sendsPerHour   int
	clientIPHeader string
}

func newPhoneTestAPI(t *testing.T, opts phoneTestOptions) (http.Handler, *MemoryStore, *smsInbox) {
	t.Helper()
	if opts.sendsPerHour == 0 {
		opts.sendsPerHour = 1000
	}
	store := NewMemoryStore()
	if _, err := store.CreateUser(t.Context(), NewUser{ID: "usr_1", Name: "Demo Player", Email: demoEmail, Password: demoPassword}); err != nil {
		t.Fatal(err)
	}
	inbox := &smsInbox{codes: make(map[string]string)}
	phone := NewPhoneLogin(otp.NewMemory("test", inbox.deliver), mustPhonePolicy(t, "IN", "IN"), opts.clientIPHeader, opts.sendsPerHour)
	return NewAPI(store, oauth.NewRegistry(), phone, false).Routes(), store, inbox
}

func postJSON(h http.Handler, path, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return serve(h, req)
}

func startPhone(h http.Handler, phone string, headers ...string) *httptest.ResponseRecorder {
	return postJSON(h, "/api/auth/phone/start", fmt.Sprintf(`{"phone":%q}`, phone), headers...)
}

func verifyPhone(h http.Handler, phone, code string) *httptest.ResponseRecorder {
	return postJSON(h, "/api/auth/phone/verify", fmt.Sprintf(`{"phone":%q,"code":%q}`, phone, code))
}

// phoneLogin runs the whole flow and returns the signed-in user.
func phoneLogin(t *testing.T, h http.Handler, inbox *smsInbox, typed string) User {
	t.Helper()
	rec := startPhone(h, typed)
	if rec.Code != http.StatusOK {
		t.Fatalf("start %q: status %d: %s", typed, rec.Code, rec.Body)
	}
	var started phoneStartResponse
	decodeJSON(t, rec.Body.Bytes(), &started)

	rec = verifyPhone(h, started.Phone, inbox.code(t, started.Phone))
	if rec.Code != http.StatusOK {
		t.Fatalf("verify: status %d: %s", rec.Code, rec.Body)
	}
	session := findCookie(rec, sessionCookieName)
	if session == nil || !session.HttpOnly {
		t.Fatalf("expected an httpOnly session cookie, got %+v", session)
	}

	var me userResponse
	decodeJSON(t, getMe(h, session).Body.Bytes(), &me)
	return me.User
}

func TestPhoneLoginCreatesAnAccountPerNumber(t *testing.T) {
	h, _, inbox := newPhoneTestAPI(t, phoneTestOptions{})

	first := phoneLogin(t, h, inbox, "+91 98765 43210")
	if first.ID == "" || first.ID == "usr_1" {
		t.Fatalf("expected a new account, got %+v", first)
	}
	// A phone proves nothing about email, so the account has none.
	if first.Email != "" || first.EmailVerified {
		t.Errorf("phone account has email %q (verified %v), want none", first.Email, first.EmailVerified)
	}

	second := phoneLogin(t, h, inbox, "098765 43211")
	if second.ID == first.ID {
		t.Fatal("different numbers must be different accounts")
	}
}

func TestPhoneLoginSameNumberIsSameAccount(t *testing.T) {
	h, store, inbox := newPhoneTestAPI(t, phoneTestOptions{})
	first := phoneLogin(t, h, inbox, "+919876543210")

	// Skip the resend cooldown by resolving the identity directly, as a later
	// login would: it must find the same account.
	again, err := store.ResolveExternalLogin(t.Context(), oauth.Identity{Provider: phoneIdentityProvider, Subject: "+919876543210"})
	if err != nil || again.ID != first.ID {
		t.Fatalf("second login = %+v, %v; want account %s", again, err, first.ID)
	}
}

func TestPhoneVerifyRejectsWrongCode(t *testing.T) {
	h, _, inbox := newPhoneTestAPI(t, phoneTestOptions{})
	if rec := startPhone(h, "+919876543210"); rec.Code != http.StatusOK {
		t.Fatalf("start: %d", rec.Code)
	}
	wrong := "000000"
	if inbox.code(t, "+919876543210") == wrong {
		wrong = "000001"
	}
	for _, code := range []string{wrong, "12345", "abcdef", ""} {
		rec := verifyPhone(h, "+919876543210", code)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("code %q: status %d, want 401", code, rec.Code)
		}
		if findCookie(rec, sessionCookieName) != nil {
			t.Errorf("code %q: a failed verify must not set a session cookie", code)
		}
	}
}

func TestPhoneStartValidatesNumber(t *testing.T) {
	h, _, _ := newPhoneTestAPI(t, phoneTestOptions{})
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"not a number":  {`{"phone":"hello"}`, "enter a valid mobile number"},
		"landline":      {`{"phone":"+91 11 2345 6789"}`, "enter a valid mobile number"},
		"other country": {`{"phone":"+44 7400 123456"}`, "phone login isn't available for this country yet"},
		"unknown field": {`{"phone":"+919876543210","email":"x@y.z"}`, "request body must be valid JSON"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := postJSON(h, "/api/auth/phone/start", tc.body)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("status %d body %s, want 400 with %q", rec.Code, rec.Body, tc.want)
			}
		})
	}
}

func TestPhoneStartResendCooldown(t *testing.T) {
	h, _, _ := newPhoneTestAPI(t, phoneTestOptions{})
	if rec := startPhone(h, "+919876543210"); rec.Code != http.StatusOK {
		t.Fatalf("first send: %d", rec.Code)
	}
	// The same number in another format is still the same number.
	rec := startPhone(h, "98765 43210")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("resend within 30s: status %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 must carry Retry-After")
	}
}

func TestPhoneStartGlobalCap(t *testing.T) {
	h, _, _ := newPhoneTestAPI(t, phoneTestOptions{sendsPerHour: 2})
	for i := range 2 {
		if rec := startPhone(h, fmt.Sprintf("+9198765%05d", i)); rec.Code != http.StatusOK {
			t.Fatalf("send %d: %d", i+1, rec.Code)
		}
	}
	// A different number: per-number limits don't apply, the global cap does.
	if rec := startPhone(h, "+919876500099"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("send beyond the global cap: status %d, want 429", rec.Code)
	}
}

func TestPhoneStartPerIPLimitOnlyWithTrustedHeader(t *testing.T) {
	t.Run("trusted header configured", func(t *testing.T) {
		h, _, _ := newPhoneTestAPI(t, phoneTestOptions{clientIPHeader: "X-Client-IP"})
		for i := range 10 {
			if rec := startPhone(h, fmt.Sprintf("+9198765%05d", i), "X-Client-IP", "203.0.113.7"); rec.Code != http.StatusOK {
				t.Fatalf("send %d: %d", i+1, rec.Code)
			}
		}
		if rec := startPhone(h, "+919876500099", "X-Client-IP", "203.0.113.7"); rec.Code != http.StatusTooManyRequests {
			t.Errorf("11th send from one IP: status %d, want 429", rec.Code)
		}
		if rec := startPhone(h, "+919876500098", "X-Client-IP", "198.51.100.1"); rec.Code != http.StatusOK {
			t.Errorf("another IP: status %d, want 200", rec.Code)
		}
	})
	t.Run("no trusted header", func(t *testing.T) {
		// Behind the Next.js proxy every request comes from the same address,
		// so a per-IP limit would lock everyone out together. It must be off.
		h, _, _ := newPhoneTestAPI(t, phoneTestOptions{})
		for i := range 12 {
			if rec := startPhone(h, fmt.Sprintf("+9198765%05d", i), "X-Forwarded-For", "203.0.113.7"); rec.Code != http.StatusOK {
				t.Fatalf("send %d: %d", i+1, rec.Code)
			}
		}
	})
}

func TestPhoneLoginDisabled(t *testing.T) {
	h, _ := newTestAPI(t) // no PhoneLogin configured
	if rec := startPhone(h, "+919876543210"); rec.Code == http.StatusOK {
		t.Errorf("phone start with phone login disabled: status %d", rec.Code)
	}
	var resp providersResponse
	decodeJSON(t, serve(h, httptest.NewRequest(http.MethodGet, "/api/auth/providers", nil)).Body.Bytes(), &resp)
	if resp.Phone {
		t.Error("providers must report phone login as disabled")
	}
}

func TestProvidersReportsPhoneSeparately(t *testing.T) {
	h, _, _ := newPhoneTestAPI(t, phoneTestOptions{})
	var resp providersResponse
	decodeJSON(t, serve(h, httptest.NewRequest(http.MethodGet, "/api/auth/providers", nil)).Body.Bytes(), &resp)
	if !resp.Phone {
		t.Error("phone = false, want true")
	}
	// It isn't a redirect provider, so it must not get a "Continue with" button.
	for _, name := range resp.Providers {
		if name == phoneIdentityProvider {
			t.Error("phone must not be listed among redirect providers")
		}
	}
}
