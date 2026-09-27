package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

type API struct {
	store     Store
	providers *oauth.Registry
	// cookieSecure marks cookies Secure. It must be true anywhere served over
	// HTTPS; it is false locally because http://localhost has no TLS.
	cookieSecure bool
}

func NewAPI(store Store, providers *oauth.Registry, cookieSecure bool) *API {
	return &API{store: store, providers: providers, cookieSecure: cookieSecure}
}

func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.handleHealth)

	mux.HandleFunc("POST /api/login", a.handleLogin)
	mux.HandleFunc("POST /api/logout", a.handleLogout)
	mux.HandleFunc("GET /api/me", a.handleMe)

	// Provider-agnostic: adding a provider means registering an adapter, not
	// adding routes.
	mux.HandleFunc("GET /api/auth/providers", a.handleProviders)
	mux.HandleFunc("GET /api/auth/{provider}/start", a.handleOAuthStart)
	// Apple posts its callback as a form, so the callback accepts both.
	mux.HandleFunc("GET /api/auth/{provider}/callback", a.handleOAuthCallback)
	mux.HandleFunc("POST /api/auth/{provider}/callback", a.handleOAuthCallback)
	return mux
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	User User `json:"user"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be valid JSON with email and password")
		return
	}
	if strings.TrimSpace(req.Email) == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, err := a.store.VerifyPassword(r.Context(), req.Email, req.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "incorrect email or password")
		return
	}
	if err != nil {
		log.Printf("login: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	if err := a.startSession(w, r, user.ID); err != nil {
		log.Printf("login: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, userResponse{User: user})
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
	writeJSON(w, http.StatusOK, map[string][]string{"providers": a.providers.Names()})
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
