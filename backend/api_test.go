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

func newTestAPI(t *testing.T, providers ...oauth.Provider) (http.Handler, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
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

// signIn creates an account through a phone identity and returns a session
// cookie for it, as the phone or OAuth handlers would after a real sign-in.
func signIn(t *testing.T, store Store, phone string) (*http.Cookie, User) {
	t.Helper()
	ctx := context.Background()
	user, err := store.ResolveExternalLogin(ctx, oauth.Identity{Provider: phoneIdentityProvider, Subject: phone})
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
	token, err := store.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: token}, user
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
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

func TestMeRequiresSessionCookie(t *testing.T) {
	h, store := newTestAPI(t)
	session, user := signIn(t, store, "+919876543210")

	t.Run("with cookie", func(t *testing.T) {
		rec := getMe(h, session)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp userResponse
		decodeJSON(t, rec.Body.Bytes(), &resp)
		if resp.User.ID != user.ID {
			t.Errorf("id = %q, want %q", resp.User.ID, user.ID)
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
	h, store := newTestAPI(t)
	session, _ := signIn(t, store, "+919876543210")

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

func TestPasswordRoutesAreGone(t *testing.T) {
	h, _ := newTestAPI(t)
	for _, path := range []string{"/api/login", "/api/signup"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"a@b.co","password":"longenough"}`))
		req.Header.Set("Content-Type", "application/json")
		if rec := serve(h, req); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, rec.Code)
		}
	}
}
