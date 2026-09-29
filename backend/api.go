package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

type API struct {
	store     Store
	providers *oauth.Registry
	// phone is nil when phone login is disabled.
	phone *PhoneLogin
	// cookieSecure marks cookies Secure. It must be true anywhere served over
	// HTTPS; it is false locally because http://localhost has no TLS.
	cookieSecure bool
}

// NewAPI builds the HTTP API. phone may be nil to disable phone login.
func NewAPI(store Store, providers *oauth.Registry, phone *PhoneLogin, cookieSecure bool) *API {
	return &API{store: store, providers: providers, phone: phone, cookieSecure: cookieSecure}
}

func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.handleHealth)

	mux.HandleFunc("POST /api/logout", a.handleLogout)
	mux.HandleFunc("GET /api/me", a.handleMe)

	// Provider-agnostic: adding a provider means registering an adapter, not
	// adding routes.
	mux.HandleFunc("GET /api/auth/providers", a.handleProviders)
	mux.HandleFunc("GET /api/auth/{provider}/start", a.handleOAuthStart)
	// Apple posts its callback as a form, so the callback accepts both.
	mux.HandleFunc("GET /api/auth/{provider}/callback", a.handleOAuthCallback)
	mux.HandleFunc("POST /api/auth/{provider}/callback", a.handleOAuthCallback)

	// Phone login is a code typed into the page, not a redirect, so it has its
	// own routes rather than being an OAuth provider.
	if a.phone != nil {
		mux.HandleFunc("POST /api/auth/phone/start", a.handlePhoneStart)
		mux.HandleFunc("POST /api/auth/phone/verify", a.handlePhoneVerify)
	}
	return mux
}

type userResponse struct {
	User User `json:"user"`
}

type providersResponse struct {
	Providers []string `json:"providers"`
	Phone     bool     `json:"phone"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	if token := sessionToken(r); token != "" {
		if err := a.store.Revoke(r.Context(), token); err != nil {
			log.Printf("logout: %v", err)
		}
	}
	a.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	token := sessionToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	user, err := a.store.UserForToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session expired or invalid")
		return
	}
	writeJSON(w, http.StatusOK, userResponse{User: user})
}

func (a *API) handleProviders(w http.ResponseWriter, r *http.Request) {
	// providers lists redirect logins ("Continue with …" buttons); phone is
	// separate because it isn't one.
	writeJSON(w, http.StatusOK, providersResponse{Providers: a.providers.Names(), Phone: a.phone != nil})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
