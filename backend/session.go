package main

import (
	"net/http"
	"time"
)

const sessionCookieName = "bp_session"

// The session lives in an httpOnly cookie rather than localStorage, so page
// scripts, including any injected ones, cannot read it. SameSite=Lax keeps it
// off cross-site POSTs, which covers CSRF for the state-changing endpoints,
// while still sending it on the top-level redirect back from a provider.
func (a *API) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	token, err := a.store.CreateSession(r.Context(), userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is config-driven
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(sessionTTL),
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is config-driven
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
