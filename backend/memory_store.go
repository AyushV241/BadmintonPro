package main

import (
	"context"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type memoryAccount struct {
	User
	passwordHash []byte
}

type memorySession struct {
	userID    string
	expiresAt time.Time
}

// MemoryStore keeps everything in process. It backs the test suite and is a
// useful fallback when Postgres is not running; data is lost on restart.
type MemoryStore struct {
	mu       sync.RWMutex
	accounts map[string]*memoryAccount // keyed by normalised email
	byID     map[string]*memoryAccount // same accounts, keyed by ID
	sessions map[string]memorySession  // keyed by token
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		accounts: make(map[string]*memoryAccount),
		byID:     make(map[string]*memoryAccount),
		sessions: make(map[string]memorySession),
	}
}

func (s *MemoryStore) CreateUser(_ context.Context, id, name, email, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	key := normaliseEmail(email)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.accounts[key]; exists {
		return ErrEmailTaken
	}
	acct := &memoryAccount{
		User:         User{ID: id, Name: name, Email: key},
		passwordHash: hash,
	}
	s.accounts[key] = acct
	s.byID[id] = acct
	return nil
}

func (s *MemoryStore) Authenticate(_ context.Context, email, password string) (User, string, error) {
	s.mu.RLock()
	acct, ok := s.accounts[normaliseEmail(email)]
	s.mu.RUnlock()

	if !ok {
		equaliseTiming(password)
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
	s.sessions[token] = memorySession{userID: acct.ID, expiresAt: time.Now().Add(sessionTTL)}
	s.mu.Unlock()

	return acct.User, token, nil
}

func (s *MemoryStore) UserForToken(ctx context.Context, token string) (User, error) {
	s.mu.RLock()
	sess, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return User{}, ErrInvalidToken
	}
	if time.Now().After(sess.expiresAt) {
		s.Revoke(ctx, token)
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

func (s *MemoryStore) Revoke(_ context.Context, token string) error {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	return nil
}
