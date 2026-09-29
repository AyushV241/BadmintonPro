package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

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

	mux.HandleFunc("POST /api/signup", a.handleSignup)
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

	// Phone login is a code typed into the page, not a redirect, so it has its
	// own routes rather than being an OAuth provider.
	if a.phone != nil {
		mux.HandleFunc("POST /api/auth/phone/start", a.handlePhoneStart)
		mux.HandleFunc("POST /api/auth/phone/verify", a.handlePhoneVerify)
	}
	return mux
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type signupRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

const (
	maxNameLength     = 100
	maxEmailLength    = 254 // RFC 5321 limit on a forward path
	minPasswordLength = 8
	// bcrypt ignores everything past 72 bytes, so a longer password would
	// silently be only partly checked. Reject it instead.
	maxPasswordBytes = 72
)

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

// handleSignup creates a password account and signs it in. The email is
// stored unverified: nothing has proved the person owns it yet, which is what
// lets a provider that does vouch for it take the account over (see
// decideLink).
//
// Unlike login, signup reveals whether an email is registered: the 409 below
// answers that question for anyone who asks. Hiding it needs email
// verification (always reply "check your inbox", and tell an existing owner
// by email instead), which is not built yet. Until then this is a known
// tradeoff, and rate limiting is the mitigation to add first.
func (a *API) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be valid JSON with name, email and password")
		return
	}
	name := strings.TrimSpace(req.Name)
	email := strings.TrimSpace(req.Email)
	if msg := validateSignup(name, email, req.Password); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	user, err := a.store.CreateUser(r.Context(), NewUser{Name: name, Email: email, Password: req.Password})
	if errors.Is(err, ErrEmailTaken) {
		writeError(w, http.StatusConflict, "an account with this email already exists")
		return
	}
	if err != nil {
		log.Printf("signup: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	if err := a.startSession(w, r, user.ID); err != nil {
		log.Printf("signup: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	writeJSON(w, http.StatusCreated, userResponse{User: user})
}

// validateSignup returns a message for the first invalid field, or "".
func validateSignup(name, email, password string) string {
	switch {
	case name == "":
		return "name is required"
	case utf8.RuneCountInString(name) > maxNameLength:
		return "name must be at most 100 characters"
	case !validEmail(email):
		return "enter a valid email address"
	case utf8.RuneCountInString(password) < minPasswordLength:
		return "password must be at least 8 characters"
	case len(password) > maxPasswordBytes:
		return "password must be at most 72 bytes"
	}
	return ""
}

// validEmail accepts a bare address only. mail.ParseAddress also accepts
// forms like "Name <a@b.c>", so the parsed address must equal the input.
func validEmail(email string) bool {
	if len(email) > maxEmailLength {
		return false
	}
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
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
