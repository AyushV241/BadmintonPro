package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrInvalidCredentials is deliberately returned for both an unknown email
	// and a wrong password, so the API cannot be used to enumerate accounts.
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrEmailTaken         = errors.New("email already registered")
)

const sessionTTL = 24 * time.Hour

// User is the publicly serialisable view of an account.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Store persists accounts and sessions. MemoryStore and PostgresStore both
// implement it; handlers depend only on this interface.
type Store interface {
	CreateUser(ctx context.Context, id, name, email, password string) error
	Authenticate(ctx context.Context, email, password string) (User, string, error)
	UserForToken(ctx context.Context, token string) (User, error)
	Revoke(ctx context.Context, token string) error
}

func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func hashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// equaliseTiming spends roughly as long as a real bcrypt comparison would, so
// response latency on an unknown email matches that of a wrong password.
func equaliseTiming(password string) {
	bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)
