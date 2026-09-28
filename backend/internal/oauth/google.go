package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	googleIssuer  = "https://accounts.google.com"
	googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"
)

// Google's endpoints are fixed rather than fetched through OIDC discovery, so
// the server can start without reaching Google. Signing keys are fetched on
// the first login and cached.
var googleEndpoint = oauth2.Endpoint{ //nolint:gosec // G101: public endpoint URLs, not credentials
	AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// Google adapts Sign in with Google (OpenID Connect) to Provider.
type Google struct {
	config   *oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
}

func NewGoogle(clientID, clientSecret, redirectURL string) (*Google, error) {
	if clientID == "" || clientSecret == "" {
		return nil, errors.New("google: client ID and client secret are both required")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), googleJWKSURL)

	return &Google{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     googleEndpoint,
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		// Checks the token's signature, issuer, audience (our client ID) and
		// expiry.
		verifier: oidc.NewVerifier(googleIssuer, keys, &oidc.Config{ClientID: clientID}),
		client:   client,
	}, nil
}

func (g *Google) Name() string { return "google" }

func (g *Google) AuthURL(state, nonce, verifier string) string {
	return g.config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}

func (g *Google) Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, g.client)

	token, err := g.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("google: exchange code: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Identity{}, errors.New("google: token response has no id_token")
	}

	idToken, err := g.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Identity{}, fmt.Errorf("google: verify id_token: %w", err)
	}
	if idToken.Nonce != nonce {
		return Identity{}, errors.New("google: id_token nonce does not match this login")
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("google: read id_token claims: %w", err)
	}

	return Identity{
		Provider:      g.Name(),
		Subject:       idToken.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		Name:          claims.Name,
	}, nil
}
