package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

// signInGoogle creates a Google account and returns a session cookie for it.
func signInGoogle(t *testing.T, store Store, subject, email string) (*http.Cookie, User) {
	t.Helper()
	ctx := context.Background()
	user, err := store.ResolveExternalLogin(ctx, oauth.Identity{Provider: "google", Subject: subject, Email: email, EmailVerified: true, Name: "Google Person"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: token}, user
}

func sendAuthed(h http.Handler, method, path, body string, session *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if session != nil {
		req.AddCookie(session)
	}
	return serve(h, req)
}

func putProfile(h http.Handler, session *http.Cookie, body string) *httptest.ResponseRecorder {
	return sendAuthed(h, http.MethodPut, "/api/me/profile", body, session)
}

func TestProfileSetupCompletesAPhoneAccount(t *testing.T) {
	h, store, _ := newPhoneTestAPI(t, phoneTestOptions{})
	session, user := signIn(t, store, "+919876543210")
	if user.ProfileComplete {
		t.Fatal("a new account must start with an incomplete profile")
	}

	rec := putProfile(h, session, `{"name":"  Asha Rao ","username":" Asha_R ","email":" Asha@Example.com "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp userResponse
	decodeJSON(t, rec.Body.Bytes(), &resp)
	got := resp.User
	if got.Name != "Asha Rao" || got.Username != "asha_r" || got.Email != "asha@example.com" || got.EmailVerified || !got.ProfileComplete {
		t.Errorf("profile = %+v; want trimmed name, lowercased username, unverified normalised email", got)
	}
	if got.Phone != "+919876543210" || !got.PhoneVerified {
		t.Errorf("the sign-in number must stay as the verified phone, got %+v", got)
	}
}

func TestProfileEmailIsOptional(t *testing.T) {
	h, store, _ := newPhoneTestAPI(t, phoneTestOptions{})
	session, _ := signIn(t, store, "+919876543210")
	if rec := putProfile(h, session, `{"name":"Asha","username":"asha"}`); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestProfileRequiresSignIn(t *testing.T) {
	h, _, _ := newPhoneTestAPI(t, phoneTestOptions{})
	for _, path := range []string{"/api/me/phone/start", "/api/me/phone/verify"} {
		if rec := sendAuthed(h, http.MethodPost, path, `{"phone":"+919876543210","code":"123456"}`, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without a session = %d, want 401", path, rec.Code)
		}
	}
	if rec := putProfile(h, nil, `{"name":"A","username":"asha"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("PUT profile without a session = %d, want 401", rec.Code)
	}
}

func TestProfileValidation(t *testing.T) {
	h, store, _ := newPhoneTestAPI(t, phoneTestOptions{})
	session, _ := signIn(t, store, "+919876543210")
	for name, body := range map[string]string{
		"missing name":         `{"name":"  ","username":"asha"}`,
		"name too long":        `{"name":"` + strings.Repeat("n", 101) + `","username":"asha"}`,
		"username too short":   `{"name":"A","username":"as"}`,
		"username too long":    `{"name":"A","username":"` + strings.Repeat("a", 21) + `"}`,
		"username with space":  `{"name":"A","username":"asha rao"}`,
		"username leading dot": `{"name":"A","username":".asha"}`,
		"reserved username":    `{"name":"A","username":"Admin"}`,
		"invalid email":        `{"name":"A","username":"asha","email":"not-an-email"}`,
		"email with a name":    `{"name":"A","username":"asha","email":"A <a@b.co>"}`,
		"unknown field":        `{"name":"A","username":"asha","phone":"+919876543210"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if rec := putProfile(h, session, body); rec.Code != http.StatusBadRequest {
				t.Errorf("status %d, want 400: %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestProfileUsernameTaken(t *testing.T) {
	h, store, _ := newPhoneTestAPI(t, phoneTestOptions{})
	first, _ := signIn(t, store, "+919876543210")
	second, _ := signIn(t, store, "+919876543211")
	if rec := putProfile(h, first, `{"name":"A","username":"shuttle"}`); rec.Code != http.StatusOK {
		t.Fatalf("first: %d", rec.Code)
	}
	rec := putProfile(h, second, `{"name":"B","username":"SHUTTLE"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "taken") {
		t.Errorf("status %d body %s, want 409 taken", rec.Code, rec.Body)
	}
}

func TestProfileGoogleEmailCantBeReplaced(t *testing.T) {
	h, store, _ := newPhoneTestAPI(t, phoneTestOptions{})
	session, _ := signInGoogle(t, store, "g-1", "real@gmail.com")
	if rec := putProfile(h, session, `{"name":"G","username":"gee","email":"other@example.com"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("replacing a verified email: status %d, want 400", rec.Code)
	}
	// Leaving the email out keeps Google's.
	rec := putProfile(h, session, `{"name":"G","username":"gee"}`)
	var resp userResponse
	decodeJSON(t, rec.Body.Bytes(), &resp)
	if resp.User.Email != "real@gmail.com" || !resp.User.EmailVerified {
		t.Errorf("profile = %+v, want Google's verified email kept", resp.User)
	}
}

func TestContactPhoneVerificationForGoogleAccount(t *testing.T) {
	h, store, inbox := newPhoneTestAPI(t, phoneTestOptions{})
	session, googleUser := signInGoogle(t, store, "g-1", "real@gmail.com")

	rec := sendAuthed(h, http.MethodPost, "/api/me/phone/start", `{"phone":"98765 43210"}`, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	var started phoneStartResponse
	decodeJSON(t, rec.Body.Bytes(), &started)
	if started.Phone != "+919876543210" {
		t.Fatalf("normalised phone = %q", started.Phone)
	}

	// A wrong code doesn't save anything.
	wrong := "000000"
	if inbox.code(t, "+919876543210") == wrong {
		wrong = "000001"
	}
	if rec := sendAuthed(h, http.MethodPost, "/api/me/phone/verify", `{"phone":"+919876543210","code":"`+wrong+`"}`, session); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong code: status %d, want 401", rec.Code)
	}
	var me userResponse
	decodeJSON(t, getMe(h, session).Body.Bytes(), &me)
	if me.User.Phone != "" {
		t.Fatalf("phone saved before verification: %+v", me.User)
	}

	body := fmt.Sprintf(`{"phone":"+919876543210","code":%q}`, inbox.code(t, "+919876543210"))
	rec = sendAuthed(h, http.MethodPost, "/api/me/phone/verify", body, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", rec.Code, rec.Body)
	}
	decodeJSON(t, getMe(h, session).Body.Bytes(), &me)
	if me.User.ID != googleUser.ID || me.User.Phone != "+919876543210" || !me.User.PhoneVerified {
		t.Errorf("after verify = %+v, want the verified phone on the Google account", me.User)
	}

	// It is contact information only: signing in with the number is a
	// separate account. Resolve directly to skip the resend cooldown.
	phoneUser, err := store.ResolveExternalLogin(context.Background(), oauth.Identity{Provider: phoneIdentityProvider, Subject: "+919876543210"})
	if err != nil || phoneUser.ID == googleUser.ID {
		t.Errorf("phone sign-in = %+v, %v; want a separate account", phoneUser, err)
	}
}

func TestContactPhoneNotForPhoneAccounts(t *testing.T) {
	h, store, _ := newPhoneTestAPI(t, phoneTestOptions{})
	session, _ := signIn(t, store, "+919876543210")
	if rec := sendAuthed(h, http.MethodPost, "/api/me/phone/start", `{"phone":"+919876543299"}`, session); rec.Code != http.StatusBadRequest {
		t.Errorf("phone account changing its number: status %d, want 400", rec.Code)
	}
}

func TestOAuthSendsNewUsersToProfileSetupUntilDone(t *testing.T) {
	fake := newFakeProvider("fake")
	fake.identities["code"] = oauth.Identity{Subject: "s-1", Email: "new@gmail.com", EmailVerified: true, Name: "New Person"}
	h, _ := newTestAPI(t, fake)

	flow, state := startFlow(t, h, "fake")
	rec := callback(h, "fake", flow, url.Values{"state": {state}, "code": {"code"}})
	assertRedirect(t, rec, "/setup-profile")
	session := findCookie(rec, sessionCookieName)

	if rec := putProfile(h, session, `{"name":"New Person","username":"newperson"}`); rec.Code != http.StatusOK {
		t.Fatalf("profile: %d %s", rec.Code, rec.Body)
	}

	// The next sign-in with the same account goes straight to the dashboard.
	flow, state = startFlow(t, h, "fake")
	assertRedirect(t, callback(h, "fake", flow, url.Values{"state": {state}, "code": {"code"}}), "/dashboard")
}

func TestPhoneVerifyReportsProfileStatus(t *testing.T) {
	h, _, inbox := newPhoneTestAPI(t, phoneTestOptions{})
	if rec := startPhone(h, "+919876543210"); rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	rec := verifyPhone(h, "+919876543210", inbox.code(t, "+919876543210"))
	var resp userResponse
	decodeJSON(t, rec.Body.Bytes(), &resp)
	if resp.User.ProfileComplete || resp.User.SignInMethod != phoneIdentityProvider {
		t.Errorf("first phone sign-in = %+v; want an incomplete phone profile", resp.User)
	}
}
