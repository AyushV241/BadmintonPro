package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
	"github.com/AyushV241/BadmintonPro/backend/internal/payments"
)

type API struct {
	store Store
	// events is nil when the API runs without a database (some tests); the
	// events routes are then not registered.
	events EventStore
	// admin is nil without a database too; the /api/admin routes then
	// aren't registered.
	admin AdminStore
	// entries and users are nil without a database too; joining and
	// username search are then not registered.
	entries EntryStore
	users   UserSearchStore
	// matches is nil without a database too; the schedule, desk and
	// player-match routes are then not registered.
	matches MatchStore
	// payments is nil without a database too; paying is then not
	// registered. paymentProvider is nil when payments are disabled: the
	// routes are registered and answer 503 (free events still confirm).
	payments        PaymentStore
	paymentProvider payments.Provider
	searchLimit     *limiter
	providers       *oauth.Registry
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

// WithEvents enables the events endpoints.
func (a *API) WithEvents(events EventStore) *API {
	a.events = events
	return a
}

// WithAdmin enables admin mode's endpoints. They need WithEvents too.
func (a *API) WithAdmin(admin AdminStore) *API {
	a.admin = admin
	return a
}

// WithEntries enables joining events and listing and cancelling your
// entries. They need WithEvents too.
func (a *API) WithEntries(entries EntryStore) *API {
	a.entries = entries
	return a
}

// WithMatches enables the schedule (preview and publish), the admin's entry
// list and offline payments, and a player's own matches. The admin routes
// need WithEvents and WithAdmin too.
func (a *API) WithMatches(matches MatchStore) *API {
	a.matches = matches
	return a
}

// WithPayments enables paying for entries and the provider's webhook.
// provider may be nil to leave payments disabled. They need WithEvents and
// WithEntries too.
func (a *API) WithPayments(store PaymentStore, provider payments.Provider) *API {
	a.payments = store
	a.paymentProvider = provider
	return a
}

// WithUserSearch enables username search.
func (a *API) WithUserSearch(users UserSearchStore) *API {
	a.users = users
	a.searchLimit = newSearchLimiter()
	return a
}

func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.handleHealth)

	mux.HandleFunc("POST /api/logout", a.handleLogout)
	mux.HandleFunc("GET /api/me", a.handleMe)
	mux.HandleFunc("PUT /api/me/profile", a.handleUpdateProfile)

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
		// A signed-in (Google) user verifying a contact phone for the profile.
		mux.HandleFunc("POST /api/me/phone/start", a.handleContactPhoneStart)
		mux.HandleFunc("POST /api/me/phone/verify", a.handleContactPhoneVerify)
	}

	// Public: no sign-in needed (see handleListEvents).
	if a.events != nil {
		mux.HandleFunc("GET /api/events", a.handleListEvents)
		mux.HandleFunc("GET /api/events/{id}", a.handleGetEvent)
	}
	if a.events != nil && a.admin != nil {
		a.registerAdminRoutes(mux)
	}
	// Signed in. Cancelling is a DELETE that keeps the row: entries are never
	// deleted, only moved to cancelled.
	if a.events != nil && a.entries != nil {
		mux.HandleFunc("POST /api/events/{id}/entries", a.handleCreateEntry)
		mux.HandleFunc("GET /api/me/entries", a.handleListMyEntries)
		mux.HandleFunc("DELETE /api/me/entries/{id}", a.handleCancelEntry)
	}
	// Paying for an entry: start (signed in), the page's verify (signed
	// in), and the provider's webhook (no session: signed by the provider).
	if a.events != nil && a.entries != nil && a.payments != nil {
		mux.HandleFunc("POST /api/me/entries/{id}/pay", a.handlePayEntry)
		mux.HandleFunc("POST /api/payments/verify", a.handleVerifyPayment)
		mux.HandleFunc("POST /api/payments/webhook", a.handlePaymentWebhook)
	}
	if a.matches != nil {
		mux.HandleFunc("GET /api/me/matches", a.handleListMyMatches)
	}
	if a.users != nil {
		mux.HandleFunc("GET /api/users/search", a.handleSearchUsers)
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
	if user, ok := a.requireUser(w, r); ok {
		writeJSON(w, http.StatusOK, userResponse{User: user})
	}
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
