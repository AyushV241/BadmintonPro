package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestAPI(t *testing.T) http.Handler {
	t.Helper()
	store := NewMemoryStore()
	if err := store.CreateUser(context.Background(), "usr_1", "Demo Player", "player@badmintonpro.local", "smash123"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return NewAPI(store).Routes()
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginSucceedsWithValidCredentials(t *testing.T) {
	h := newTestAPI(t)
	rec := post(t, h, "/api/login", `{"email":"player@badmintonpro.local","password":"smash123"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	var resp loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected a session token")
	}
	if resp.User.Email != "player@badmintonpro.local" {
		t.Errorf("email = %q, want the demo user", resp.User.Email)
	}
}

func TestLoginIsCaseInsensitiveOnEmail(t *testing.T) {
	h := newTestAPI(t)
	rec := post(t, h, "/api/login", `{"email":"  PLAYER@BadmintonPro.local ","password":"smash123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	h := newTestAPI(t)

	cases := map[string]string{
		"wrong password": `{"email":"player@badmintonpro.local","password":"nope"}`,
		"unknown email":  `{"email":"nobody@badmintonpro.local","password":"smash123"}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := post(t, h, "/api/login", body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			// Both failures must look identical, or the endpoint leaks which
			// emails are registered.
			var resp errorResponse
			json.Unmarshal(rec.Body.Bytes(), &resp)
			if resp.Error != "incorrect email or password" {
				t.Errorf("error = %q, want the generic message", resp.Error)
			}
		})
	}
}

func TestLoginRejectsMalformedBody(t *testing.T) {
	h := newTestAPI(t)
	for name, body := range map[string]string{
		"not json":       `{`,
		"missing fields": `{}`,
		"empty password": `{"email":"player@badmintonpro.local","password":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			if rec := post(t, h, "/api/login", body); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestMeRequiresValidToken(t *testing.T) {
	h := newTestAPI(t)

	login := post(t, h, "/api/login", `{"email":"player@badmintonpro.local","password":"smash123"}`)
	var resp loginResponse
	json.Unmarshal(login.Body.Bytes(), &resp)

	t.Run("with token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var user User
		json.Unmarshal(rec.Body.Bytes(), &user)
		if user.ID != "usr_1" {
			t.Errorf("id = %q, want usr_1", user.ID)
		}
	})

	t.Run("without token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

func TestLogoutInvalidatesToken(t *testing.T) {
	h := newTestAPI(t)

	login := post(t, h, "/api/login", `{"email":"player@badmintonpro.local","password":"smash123"}`)
	var resp loginResponse
	json.Unmarshal(login.Body.Bytes(), &resp)

	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("after logout /api/me = %d, want 401", rec.Code)
	}
}
