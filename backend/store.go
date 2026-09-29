package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

var ErrInvalidToken = errors.New("invalid or expired token")

const sessionTTL = 24 * time.Hour

// User is the publicly serialisable view of an account.
type User struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
}

// Store persists accounts, sign-in identities and sessions. MemoryStore and
// PostgresStore both implement it; handlers depend only on this interface.
type Store interface {
	// ResolveExternalLogin signs in with a phone or provider identity: an
	// identity seen before gets its account, a new one gets a new account.
	// Each sign-in method is its own account. Accounts are never found or
	// joined by email, so an email can't be used to reach someone else's.
	ResolveExternalLogin(ctx context.Context, ident oauth.Identity) (User, error)

	CreateSession(ctx context.Context, userID string) (string, error)
	UserForToken(ctx context.Context, token string) (User, error)
	Revoke(ctx context.Context, token string) error
}

func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// displayName picks a name for an account created through a provider that may
// not supply one (Apple sends it only on the first login).
func displayName(ident oauth.Identity) string {
	if name := strings.TrimSpace(ident.Name); name != "" {
		return name
	}
	if local, _, ok := strings.Cut(ident.Email, "@"); ok && local != "" {
		return local
	}
	return "Player"
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func newToken() (string, error) { return randomHex(32) }

func newUserID() (string, error) {
	id, err := randomHex(12)
	if err != nil {
		return "", err
	}
	return "usr_" + id, nil
}

// newAccount is the account created on an identity's first sign-in. A
// provider's email is kept only when the provider vouches for it, and even
// then only as contact information.
func newAccount(id string, ident oauth.Identity) User {
	u := User{ID: id, Name: displayName(ident)}
	if email := normaliseEmail(ident.Email); ident.EmailVerified && email != "" {
		u.Email, u.EmailVerified = email, true
	}
	return u
}
