package main

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

const (
	flowCookieName = "bp_oauth_flow"
	flowCookiePath = "/api/auth/"
	flowTTL        = 10 * time.Minute
)

// Error codes the login page turns into messages. Deliberately coarse: the
// detail goes to the server log, not the URL.
const (
	loginErrCancelled = "cancelled" // user backed out on the provider's screen
	loginErrExpired   = "expired"   // missing, stale or mismatched state
	loginErrFailed    = "failed"    // anything else
)

func (a *API) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	provider, ok := a.providers.Get(r.PathValue("provider"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown login provider")
		return
	}

	flow, err := oauth.NewFlow(provider.Name())
	if err != nil {
		log.Printf("oauth start: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	if err := a.setFlowCookie(w, flow); err != nil {
		log.Printf("oauth start: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	// The target is built by the registered provider from its fixed config,
	// never from request input.
	http.Redirect(w, r, provider.AuthURL(flow.State, flow.Nonce, flow.Verifier), http.StatusFound) //nolint:gosec // G710: provider-built URL
}

func (a *API) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	provider, ok := a.providers.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown login provider")
		return
	}

	// The flow is single-use whatever happens next.
	flow, hasFlow := readFlowCookie(r)
	a.clearFlowCookie(w)

	// FormValue reads the query string and, for Apple's form_post, the body.
	if providerErr := r.FormValue("error"); providerErr != "" {
		log.Printf("oauth %s: provider returned error %q", provider.Name(), providerErr)
		redirectToLogin(w, r, loginErrCancelled)
		return
	}

	state := r.FormValue("state")
	if !hasFlow || flow.Provider != name || state == "" ||
		subtle.ConstantTimeCompare([]byte(state), []byte(flow.State)) != 1 {
		redirectToLogin(w, r, loginErrExpired)
		return
	}

	code := r.FormValue("code")
	if code == "" {
		redirectToLogin(w, r, loginErrFailed)
		return
	}

	ident, err := provider.Exchange(r.Context(), code, flow.Verifier, flow.Nonce)
	if err != nil {
		log.Printf("oauth %s: %v", provider.Name(), err)
		redirectToLogin(w, r, loginErrFailed)
		return
	}
	// Never trust an adapter to label itself; the route decides the provider.
	ident.Provider = provider.Name()

	user, err := a.store.ResolveExternalLogin(r.Context(), ident)
	if err != nil {
		log.Printf("oauth %s: resolve login: %v", provider.Name(), err)
		redirectToLogin(w, r, loginErrFailed)
		return
	}

	if err := a.startSession(w, r, user.ID); err != nil {
		log.Printf("oauth %s: %v", provider.Name(), err)
		redirectToLogin(w, r, loginErrFailed)
		return
	}

	// 303 so a POST callback (Apple) becomes a GET of the dashboard.
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func redirectToLogin(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?error="+code, http.StatusSeeOther)
}

// The flow cookie is httpOnly and scoped to /api/auth/, so it is only ever
// sent back to the callback. SameSite=Lax is right for Google, whose callback
// is a top-level GET. Apple's callback is a cross-site POST, which Lax cookies
// are not sent on; the Apple adapter will need SameSite=None (and therefore
// Secure, meaning HTTPS) for this cookie.
func (a *API) setFlowCookie(w http.ResponseWriter, flow oauth.Flow) error {
	raw, err := json.Marshal(flow)
	if err != nil {
		return err
	}
	// Secure comes from config: false only on http://localhost.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is config-driven
		Name:     flowCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     flowCookiePath,
		MaxAge:   int(flowTTL / time.Second),
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (a *API) clearFlowCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is config-driven
		Name:     flowCookieName,
		Value:    "",
		Path:     flowCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func readFlowCookie(r *http.Request) (oauth.Flow, bool) {
	c, err := r.Cookie(flowCookieName)
	if err != nil {
		return oauth.Flow{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return oauth.Flow{}, false
	}
	var flow oauth.Flow
	if err := json.Unmarshal(raw, &flow); err != nil {
		return oauth.Flow{}, false
	}
	return flow, true
}
