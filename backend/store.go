package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrInvalidCredentials is deliberately returned for an unknown email, a
	// wrong password, and an account with no password alike, so the API
	// cannot be used to enumerate accounts.
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrEmailTaken         = errors.New("email already registered")
	// ErrAccountConflict means an external login presented the email of an
	// existing account, but the provider does not vouch for that email, so
	// linking could hand the account to the wrong person.
	ErrAccountConflict = errors.New("email belongs to an existing account that cannot be safely linked")
)

const sessionTTL = 24 * time.Hour

// User is the publicly serialisable view of an account.
type User struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
}

// NewUser describes a password account to create.
type NewUser struct {
	ID            string // generated when empty
	Name          string
	Email         string
	Password      string
	EmailVerified bool
}

// Store persists accounts, login methods and sessions. MemoryStore and
// PostgresStore both implement it; handlers depend only on this interface.
type Store interface {
	// CreateUser registers a password account. It returns ErrEmailTaken if
	// any account already uses the email, however that account signs in.
	CreateUser(ctx context.Context, u NewUser) (User, error)

	// VerifyPassword checks a password login.
	VerifyPassword(ctx context.Context, email, password string) (User, error)

	// ResolveExternalLogin maps a provider identity to a user, creating or
	// linking accounts according to decideLink. It is atomic.
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

func hashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
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

// equaliseTiming spends roughly as long as a real bcrypt comparison would, so
// response latency does not reveal whether an account exists or has a password.
func equaliseTiming(password string) {
	bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)
