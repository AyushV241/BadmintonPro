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

var (
	ErrInvalidToken  = errors.New("invalid or expired token")
	ErrUsernameTaken = errors.New("username taken")
	ErrNoSuchUser    = errors.New("no such user")
)

const sessionTTL = 24 * time.Hour

// User is the publicly serialisable view of an account.
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	// Email and Phone are contact details, never ways to sign in. Each is
	// verified when a provider vouched for it or a code proved it.
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	Phone         string `json:"phone"`
	PhoneVerified bool   `json:"phoneVerified"`
	// SignInMethod is how the account signs in: "phone", "google", ...
	SignInMethod string `json:"signInMethod"`
	// ProfileComplete is false until the "Set up your profile" step has
	// given the account a name and a username. See withProfileStatus.
	ProfileComplete bool `json:"profileComplete"`
}

// ProfileUpdate is what the profile step can change.
type ProfileUpdate struct {
	Name     string
	Username string // already validated and lowercased
	// Email, when non-nil, replaces the contact email ("" clears it). It is
	// stored unverified; only a provider or a code can verify an email.
	Email *string
}

// withProfileStatus fills in the derived ProfileComplete flag. Stores call it
// on every User they return.
func withProfileStatus(u User) User {
	u.ProfileComplete = u.Name != "" && u.Username != ""
	return u
}

// Store persists accounts, sign-in identities and sessions. MemoryStore and
// PostgresStore both implement it; handlers depend only on this interface.
type Store interface {
	// ResolveExternalLogin signs in with a phone or provider identity: an
	// identity seen before gets its account, a new one gets a new account.
	// Each sign-in method is its own account. Accounts are never found or
	// joined by email, so an email can't be used to reach someone else's.
	ResolveExternalLogin(ctx context.Context, ident oauth.Identity) (User, error)

	// UpdateProfile saves the profile step. It returns ErrUsernameTaken if
	// another account has the username in any letter case, and ErrNoSuchUser
	// for an unknown ID.
	UpdateProfile(ctx context.Context, userID string, p ProfileUpdate) (User, error)

	// SetVerifiedPhone records a contact phone a code has just proved. It
	// does not make the number a way to sign in.
	SetVerifiedPhone(ctx context.Context, userID, phone string) (User, error)

	CreateSession(ctx context.Context, userID string) (string, error)
	UserForToken(ctx context.Context, token string) (User, error)
	Revoke(ctx context.Context, token string) error
}

func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
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

// newAccount is the account created on an identity's first sign-in. It has
// no username yet, so its profile is incomplete. A provider's name is kept as
// a starting point; a provider's email only when the provider vouches for it,
// and even then only as contact information. A phone account's number is its
// verified contact phone.
func newAccount(id string, ident oauth.Identity) User {
	u := User{ID: id, Name: strings.TrimSpace(ident.Name), SignInMethod: ident.Provider}
	if email := normaliseEmail(ident.Email); ident.EmailVerified && email != "" {
		u.Email, u.EmailVerified = email, true
	}
	if ident.Provider == phoneIdentityProvider {
		u.Phone, u.PhoneVerified = ident.Subject, true
	}
	return withProfileStatus(u)
}
