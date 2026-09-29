package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

const (
	demoEmail    = "player@badmintonpro.local"
	demoPassword = "smash123"
)

func newTestAPI(t *testing.T, providers ...oauth.Provider) (http.Handler, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	if _, err := store.CreateUser(context.Background(), NewUser{ID: "usr_1", Name: "Demo Player", Email: demoEmail, Password: demoPassword}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return NewAPI(store, oauth.NewRegistry(providers...), nil, false).Routes(), store
}

// decodeJSON fails the test if the body isn't valid JSON for v, so a broken
// response can't pass as a zero-valued struct.
func decodeJSON(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode response %q: %v", body, err)
	}
}

// linkGoogleOnlyAccount creates an account that can only sign in with Google.
func linkGoogleOnlyAccount(t *testing.T, store Store) {
	t.Helper()
	if _, err := store.ResolveExternalLogin(context.Background(), oauth.Identity{Provider: "google", Subject: "g", Email: "googleonly@gmail.com", EmailVerified: true}); err != nil {
		t.Fatalf("seed Google-only account: %v", err)
	}
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postLogin(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return serve(h, req)
}

func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func getMe(h http.Handler, session *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	if session != nil {
		req.AddCookie(session)
	}
	return serve(h, req)
}

func TestLoginSetsHttpOnlySessionCookie(t *testing.T) {
	h, _ := newTestAPI(t)
	rec := postLogin(h, `{"email":"player@badmintonpro.local","password":"smash123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	c := findCookie(rec, sessionCookieName)
	if c == nil || c.Value == "" {
		t.Fatal("expected a session cookie")
	}
	if !c.HttpOnly {
		t.Error("session cookie must be HttpOnly so page scripts cannot read it")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}

	// The token travels only in the cookie, never the body.
	if strings.Contains(rec.Body.String(), c.Value) {
		t.Error("response body leaks the session token")
	}
	var resp userResponse
	decodeJSON(t, rec.Body.Bytes(), &resp)
	if resp.User.Email != demoEmail {
		t.Errorf("user = %+v", resp.User)
	}
}

func TestLoginIsCaseInsensitiveOnEmail(t *testing.T) {
	h, _ := newTestAPI(t)
	if rec := postLogin(h, `{"email":"  PLAYER@BadmintonPro.local ","password":"smash123"}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestLoginRejectsBadCredentialsIdentically(t *testing.T) {
	h, store := newTestAPI(t)
	// An account with no password at all must fail the same way.
	linkGoogleOnlyAccount(t, store)

	for name, body := range map[string]string{
		"wrong password":          `{"email":"player@badmintonpro.local","password":"nope"}`,
		"unknown email":           `{"email":"nobody@badmintonpro.local","password":"smash123"}`,
		"account has no password": `{"email":"googleonly@gmail.com","password":"anything"}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := postLogin(h, body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			var resp errorResponse
			decodeJSON(t, rec.Body.Bytes(), &resp)
			if resp.Error != "incorrect email or password" {
				t.Errorf("error = %q, want the generic message", resp.Error)
			}
			if findCookie(rec, sessionCookieName) != nil {
				t.Error("failed login must not set a session cookie")
			}
		})
	}
}

func TestLoginRejectsMalformedBody(t *testing.T) {
	h, _ := newTestAPI(t)
	for name, body := range map[string]string{
		"not json":       `{`,
		"missing fields": `{}`,
		"empty password": `{"email":"player@badmintonpro.local","password":""}`,
		"unknown field":  `{"email":"a@b.c","password":"x","admin":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			if rec := postLogin(h, body); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestMeRequiresSessionCookie(t *testing.T) {
	h, _ := newTestAPI(t)
	session := findCookie(postLogin(h, `{"email":"player@badmintonpro.local","password":"smash123"}`), sessionCookieName)

	t.Run("with cookie", func(t *testing.T) {
		rec := getMe(h, session)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp userResponse
		decodeJSON(t, rec.Body.Bytes(), &resp)
		if resp.User.ID != "usr_1" {
			t.Errorf("id = %q, want usr_1", resp.User.ID)
		}
	})
	t.Run("without cookie", func(t *testing.T) {
		if rec := getMe(h, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
	t.Run("with forged cookie", func(t *testing.T) {
		if rec := getMe(h, &http.Cookie{Name: sessionCookieName, Value: "forged"}); rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

func TestLogoutRevokesSessionAndClearsCookie(t *testing.T) {
	h, _ := newTestAPI(t)
	session := findCookie(postLogin(h, `{"email":"player@badmintonpro.local","password":"smash123"}`), sessionCookieName)

	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(session)
	rec := serve(h, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", rec.Code)
	}
	if c := findCookie(rec, sessionCookieName); c == nil || c.MaxAge >= 0 {
		t.Error("logout must expire the session cookie")
	}

	// Revoked server-side too: replaying the old cookie must fail.
	if rec := getMe(h, session); rec.Code != http.StatusUnauthorized {
		t.Errorf("after logout /api/me = %d, want 401", rec.Code)
	}
}

func postSignup(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return serve(h, req)
}

func TestSignupCreatesUnverifiedAccountAndSignsIn(t *testing.T) {
	h, _ := newTestAPI(t)
	rec := postSignup(h, `{"name":"  New Player ","email":" New@Example.com ","password":"longenough"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}

	c := findCookie(rec, sessionCookieName)
	if c == nil || c.Value == "" || !c.HttpOnly {
		t.Fatalf("expected an httpOnly session cookie, got %+v", c)
	}
	if strings.Contains(rec.Body.String(), c.Value) {
		t.Error("response body leaks the session token")
	}

	var resp userResponse
	decodeJSON(t, getMe(h, c).Body.Bytes(), &resp)
	if resp.User.Name != "New Player" || resp.User.Email != "new@example.com" {
		t.Errorf("user = %+v, want trimmed name and normalised email", resp.User)
	}
	// Nothing has proved the email yet. Marking it verified would let a
	// squatter keep the account after the real owner signs in with Google.
	if resp.User.EmailVerified {
		t.Error("a signed-up account must start with emailVerified false")
	}

	if rec := postLogin(h, `{"email":"new@example.com","password":"longenough"}`); rec.Code != http.StatusOK {
		t.Errorf("login with the new password = %d, want 200", rec.Code)
	}
}

func TestSignupRefusesTakenEmail(t *testing.T) {
	h, store := newTestAPI(t)
	linkGoogleOnlyAccount(t, store)

	for name, email := range map[string]string{
		"password account":         "player@badmintonpro.local",
		"different case":           "PLAYER@badmintonpro.local",
		"account with no password": "googleonly@gmail.com",
	} {
		t.Run(name, func(t *testing.T) {
			rec := postSignup(h, `{"name":"Someone","email":"`+email+`","password":"longenough"}`)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409", rec.Code)
			}
			if findCookie(rec, sessionCookieName) != nil {
				t.Error("refused signup must not set a session cookie")
			}
		})
	}

	// The existing account is untouched.
	if rec := postLogin(h, `{"email":"player@badmintonpro.local","password":"smash123"}`); rec.Code != http.StatusOK {
		t.Errorf("original login = %d, want 200", rec.Code)
	}
}

func TestSignupRejectsInvalidInput(t *testing.T) {
	h, _ := newTestAPI(t)
	for name, body := range map[string]string{
		"not json":             `{`,
		"unknown field":        `{"name":"A","email":"a@b.co","password":"longenough","emailVerified":true}`,
		"missing name":         `{"name":"  ","email":"a@b.co","password":"longenough"}`,
		"name too long":        `{"name":"` + strings.Repeat("n", 101) + `","email":"a@b.co","password":"longenough"}`,
		"invalid email":        `{"name":"A","email":"not-an-email","password":"longenough"}`,
		"email with name":      `{"name":"A","email":"A <a@b.co>","password":"longenough"}`,
		"short password":       `{"name":"A","email":"a@b.co","password":"short"}`,
		"password over bcrypt": `{"name":"A","email":"a@b.co","password":"` + strings.Repeat("p", 73) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := postSignup(h, body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body: %s)", rec.Code, rec.Body)
			}
			if findCookie(rec, sessionCookieName) != nil {
				t.Error("rejected signup must not set a session cookie")
			}
		})
	}
}
