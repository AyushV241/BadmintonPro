package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

// fakeProvider is a complete Provider in a few lines, standing in for Google in
// tests. That it needs no changes to routes, handlers or storage is the point
// of the abstraction.
type fakeProvider struct {
	name       string
	identities map[string]oauth.Identity // authorization code → identity

	// Recorded so tests can check the handler threads the flow's secrets
	// from start through to the exchange.
	authVerifier, authNonce         string
	exchangeVerifier, exchangeNonce string
	exchanged                       bool
}

func newFakeProvider(name string) *fakeProvider {
	return &fakeProvider{name: name, identities: make(map[string]oauth.Identity)}
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) AuthURL(state, nonce, verifier string) string {
	f.authVerifier, f.authNonce = verifier, nonce
	return "https://" + f.name + ".example/authorize?state=" + url.QueryEscape(state)
}

func (f *fakeProvider) Exchange(_ context.Context, code, verifier, nonce string) (oauth.Identity, error) {
	f.exchanged = true
	f.exchangeVerifier, f.exchangeNonce = verifier, nonce
	ident, ok := f.identities[code]
	if !ok {
		return oauth.Identity{}, errors.New("unknown code")
	}
	return ident, nil
}

// startFlow begins a login and returns the flow cookie and issued state.
func startFlow(t *testing.T, h http.Handler, provider string) (*http.Cookie, string) {
	t.Helper()
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/api/auth/"+provider+"/start", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("start status = %d, want 302 (body: %s)", rec.Code, rec.Body)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	flow := findCookie(rec, flowCookieName)
	if flow == nil {
		t.Fatal("start must set the flow cookie")
	}
	return flow, loc.Query().Get("state")
}

func callback(h http.Handler, provider string, flow *http.Cookie, query url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/auth/"+provider+"/callback?"+query.Encode(), nil)
	if flow != nil {
		req.AddCookie(flow)
	}
	return serve(h, req)
}

func assertRedirect(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

func TestProvidersEndpointListsRegisteredProviders(t *testing.T) {
	h, _ := newTestAPI(t, newFakeProvider("zeta"), newFakeProvider("alpha"))
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/api/auth/providers", nil))

	var resp struct{ Providers []string }
	decodeJSON(t, rec.Body.Bytes(), &resp)
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(resp.Providers, want) {
		t.Errorf("providers = %v, want %v", resp.Providers, want)
	}
}

func TestOAuthStartRedirectsWithScopedFlowCookie(t *testing.T) {
	fake := newFakeProvider("fake")
	h, _ := newTestAPI(t, fake)

	flow, state := startFlow(t, h, "fake")
	if state == "" {
		t.Fatal("redirect must carry a state parameter")
	}
	if !flow.HttpOnly || flow.Path != flowCookiePath || flow.MaxAge <= 0 {
		t.Errorf("flow cookie = %+v, want HttpOnly, path %s, short-lived", flow, flowCookiePath)
	}
	if fake.authVerifier == "" || fake.authNonce == "" {
		t.Error("start must generate a PKCE verifier and a nonce")
	}
}

func TestOAuthCallbackSignsInNewUser(t *testing.T) {
	fake := newFakeProvider("fake")
	fake.identities["good-code"] = oauth.Identity{Subject: "sub-1", Email: "new@example.com", EmailVerified: true, Name: "New Person"}
	h, _ := newTestAPI(t, fake)

	flow, state := startFlow(t, h, "fake")
	rec := callback(h, "fake", flow, url.Values{"state": {state}, "code": {"good-code"}})
	assertRedirect(t, rec, "/dashboard")

	// Secrets generated at start must reach the exchange unchanged.
	if fake.exchangeVerifier != fake.authVerifier || fake.exchangeNonce != fake.authNonce {
		t.Error("callback did not pass the flow's verifier and nonce to Exchange")
	}
	if c := findCookie(rec, flowCookieName); c == nil || c.MaxAge >= 0 {
		t.Error("the flow cookie must be cleared after use")
	}

	session := findCookie(rec, sessionCookieName)
	if session == nil {
		t.Fatal("callback must start a session")
	}
	me := getMe(h, session)
	var resp userResponse
	decodeJSON(t, me.Body.Bytes(), &resp)
	if resp.User.Email != "new@example.com" || resp.User.Name != "New Person" {
		t.Errorf("signed in as %+v", resp.User)
	}
}

func TestOAuthCallbackAcceptsFormPost(t *testing.T) {
	// Apple returns the callback as a form POST rather than a GET.
	fake := newFakeProvider("fake")
	fake.identities["code"] = oauth.Identity{Subject: "sub-post", Email: "post@example.com", EmailVerified: true}
	h, _ := newTestAPI(t, fake)

	flow, state := startFlow(t, h, "fake")
	form := url.Values{"state": {state}, "code": {"code"}}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/fake/callback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(flow)

	assertRedirect(t, serve(h, req), "/dashboard")
}

func TestOAuthCallbackRejectsBadState(t *testing.T) {
	cases := map[string]func(flow *http.Cookie, state string) (*http.Cookie, url.Values){
		"state mismatch": func(f *http.Cookie, _ string) (*http.Cookie, url.Values) {
			return f, url.Values{"state": {"attacker-state"}, "code": {"code"}}
		},
		"missing state": func(f *http.Cookie, _ string) (*http.Cookie, url.Values) {
			return f, url.Values{"code": {"code"}}
		},
		"no flow cookie": func(_ *http.Cookie, s string) (*http.Cookie, url.Values) {
			return nil, url.Values{"state": {s}, "code": {"code"}}
		},
		"tampered flow cookie": func(f *http.Cookie, s string) (*http.Cookie, url.Values) {
			return &http.Cookie{Name: f.Name, Value: "not-base64-json"}, url.Values{"state": {s}, "code": {"code"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			fake := newFakeProvider("fake")
			fake.identities["code"] = oauth.Identity{Subject: "s", Email: "x@example.com", EmailVerified: true}
			h, _ := newTestAPI(t, fake)

			flow, state := startFlow(t, h, "fake")
			cookie, query := mutate(flow, state)
			rec := callback(h, "fake", cookie, query)

			assertRedirect(t, rec, "/login?error="+loginErrExpired)
			if fake.exchanged {
				t.Error("the code must not be exchanged when state fails")
			}
			if findCookie(rec, sessionCookieName) != nil {
				t.Error("no session may be started")
			}
		})
	}
}

func TestOAuthCallbackRejectsFlowFromAnotherProvider(t *testing.T) {
	a, b := newFakeProvider("a"), newFakeProvider("b")
	b.identities["code"] = oauth.Identity{Subject: "s", EmailVerified: true}
	h, _ := newTestAPI(t, a, b)

	flow, state := startFlow(t, h, "a")
	rec := callback(h, "b", flow, url.Values{"state": {state}, "code": {"code"}})
	assertRedirect(t, rec, "/login?error="+loginErrExpired)
}

func TestOAuthCallbackUserCancelled(t *testing.T) {
	h, _ := newTestAPI(t, newFakeProvider("fake"))
	flow, state := startFlow(t, h, "fake")
	rec := callback(h, "fake", flow, url.Values{"state": {state}, "error": {"access_denied"}})
	assertRedirect(t, rec, "/login?error="+loginErrCancelled)
}

func TestOAuthCallbackExchangeFailure(t *testing.T) {
	h, _ := newTestAPI(t, newFakeProvider("fake"))
	flow, state := startFlow(t, h, "fake")
	rec := callback(h, "fake", flow, url.Values{"state": {state}, "code": {"not-a-real-code"}})
	assertRedirect(t, rec, "/login?error="+loginErrFailed)
}

func TestOAuthNeverJoinsAccountsByEmail(t *testing.T) {
	// A phone account lists an email; a Google login with that same verified
	// email must get its own account, not the phone account.
	fake := newFakeProvider("fake")
	fake.identities["code"] = oauth.Identity{Subject: "s", Email: "shared@gmail.com", EmailVerified: true}
	h, store := newTestAPI(t, fake)
	_, phoneUser := signIn(t, store, "+919876543210")

	flow, state := startFlow(t, h, "fake")
	rec := callback(h, "fake", flow, url.Values{"state": {state}, "code": {"code"}})
	var resp userResponse
	decodeJSON(t, getMe(h, findCookie(rec, sessionCookieName)).Body.Bytes(), &resp)
	if resp.User.ID == "" || resp.User.ID == phoneUser.ID {
		t.Fatalf("Google login got %+v; want a new account separate from %s", resp.User, phoneUser.ID)
	}
}

func TestOAuthIgnoresProviderClaimedByAdapter(t *testing.T) {
	// An adapter labelling its identity as another provider must not be able
	// to sign in as that provider's user.
	fake := newFakeProvider("fake")
	fake.identities["code"] = oauth.Identity{Provider: "google", Subject: "victim-sub", EmailVerified: true}
	h, store := newTestAPI(t, fake)

	victim, _ := store.ResolveExternalLogin(context.Background(), oauth.Identity{Provider: "google", Subject: "victim-sub", Email: "victim@gmail.com", EmailVerified: true})

	flow, state := startFlow(t, h, "fake")
	rec := callback(h, "fake", flow, url.Values{"state": {state}, "code": {"code"}})
	var resp userResponse
	decodeJSON(t, getMe(h, findCookie(rec, sessionCookieName)).Body.Bytes(), &resp)
	if resp.User.ID == victim.ID {
		t.Fatal("adapter-supplied provider name was trusted")
	}
}

func TestOAuthUnknownProviderIs404(t *testing.T) {
	h, _ := newTestAPI(t)
	for _, path := range []string{"/api/auth/nope/start", "/api/auth/nope/callback"} {
		if rec := serve(h, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
	}
}
