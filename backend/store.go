package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrInvalidCredentials is deliberately returned for both an unknown email
	// and a wrong password, so the API cannot be used to enumerate accounts.
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid or expired token")
)

const sessionTTL = 24 * time.Hour

// User is the publicly serialisable view of an account.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type account struct {
	User
	passwordHash []byte
}

type session struct {
	userID    string
	expiresAt time.Time
}

// Store is an in-memory user and session store. It is a stand-in for a real
// database so the app runs locally with no external dependencies; swapping it
// for Postgres means reimplementing these four methods.
type Store struct {
	mu       sync.RWMutex
	accounts map[string]*account // keyed by normalised email
	byID     map[string]*account // same accounts, keyed by ID
	sessions map[string]session  // keyed by token
}

func NewStore() *Store {
	return &Store{
		accounts: make(map[string]*account),
		byID:     make(map[string]*account),
		sessions: make(map[string]session),
	}
}

func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// CreateUser registers an account, hashing the password with bcrypt.
func (s *Store) CreateUser(id, name, email, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	key := normaliseEmail(email)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.accounts[key]; exists {
		return errors.New("email already registered")
	}
	acct := &account{
		User:         User{ID: id, Name: name, Email: key},
		passwordHash: hash,
	}
	s.accounts[key] = acct
	s.byID[id] = acct
	return nil
}

// Authenticate verifies credentials and issues a session token.
func (s *Store) Authenticate(email, password string) (User, string, error) {
	s.mu.RLock()
	acct, ok := s.accounts[normaliseEmail(email)]
	s.mu.RUnlock()

	if !ok {
		// Spend roughly the same time as a real comparison would, so response
		// latency does not reveal whether the email exists.
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return User{}, "", ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword(acct.passwordHash, []byte(password)); err != nil {
		return User{}, "", ErrInvalidCredentials
	}

	token, err := newToken()
	if err != nil {
		return User{}, "", err
	}

	s.mu.Lock()
	s.sessions[token] = session{userID: acct.ID, expiresAt: time.Now().Add(sessionTTL)}
	s.mu.Unlock()

	return acct.User, token, nil
}

// UserForToken resolves a session token back to its account.
func (s *Store) UserForToken(token string) (User, error) {
	s.mu.RLock()
	sess, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return User{}, ErrInvalidToken
	}
	if time.Now().After(sess.expiresAt) {
		s.Revoke(token)
		return User{}, ErrInvalidToken
	}

	s.mu.RLock()
	acct, ok := s.byID[sess.userID]
	s.mu.RUnlock()
	if !ok {
		return User{}, ErrInvalidToken
	}
	return acct.User, nil
}

// Revoke deletes a session, making the token unusable.
func (s *Store) Revoke(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// dummyHash is a valid bcrypt hash used only to equalise timing on the
// unknown-email path.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)
