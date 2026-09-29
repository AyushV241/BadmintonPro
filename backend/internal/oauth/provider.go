// Package oauth adapts external login services (Google today; Facebook, Apple
// and others later) to a single shape the rest of the app can depend on.
//
// Each provider differs: Google and Apple speak OpenID Connect and return a
// signed ID token, Facebook is plain OAuth2 and needs a Graph API call, and
// Apple posts its callback as a form. Every adapter absorbs those differences
// and reduces a successful login to an Identity.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"

	"golang.org/x/oauth2"
)

// Identity is what every provider adapter reduces a successful login to.
type Identity struct {
	Provider string
	// Subject is the provider's stable ID for the user. Accounts are keyed on
	// this, never on email: emails change, Apple issues private relay
	// addresses, and Facebook may not return one at all.
	Subject string
	Email   string
	// EmailVerified is true only when the provider vouches that the user
	// controls Email. Only a vouched-for email is stored, and then only as
	// contact information: accounts are never found or joined by email.
	EmailVerified bool
	Name          string
}

// Provider is implemented once per login service.
type Provider interface {
	// Name is the URL segment identifying the provider, e.g. "google".
	Name() string
	// AuthURL is where to send the browser to start a login.
	AuthURL(state, nonce, verifier string) string
	// Exchange trades the callback's authorization code for a verified
	// Identity. It must check the nonce where the protocol supports one.
	Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error)
}

// Registry maps provider names to adapters. Enabling a new provider is a
// matter of registering it; routes and storage are provider-agnostic.
type Registry struct {
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{providers: make(map[string]Provider)}
	for _, p := range providers {
		r.Register(p)
	}
	return r
}

func (r *Registry) Register(p Provider) {
	r.providers[p.Name()] = p
}

func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// Names lists the enabled providers in a stable order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Flow holds the per-attempt secrets that tie a callback to the browser that
// started the login:
//
//   - State defends against CSRF: the callback must echo the value we issued.
//   - Nonce is embedded in the ID token, so a token replayed from another
//     login is rejected.
//   - Verifier is the PKCE secret, so an intercepted authorization code is
//     useless on its own.
type Flow struct {
	Provider string `json:"p"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
}

func NewFlow(provider string) (Flow, error) {
	state, err := randomHex(32)
	if err != nil {
		return Flow{}, err
	}
	nonce, err := randomHex(32)
	if err != nil {
		return Flow{}, err
	}
	return Flow{
		Provider: provider,
		State:    state,
		Nonce:    nonce,
		Verifier: oauth2.GenerateVerifier(),
	}, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
