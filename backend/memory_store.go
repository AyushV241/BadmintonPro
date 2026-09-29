package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

type identityKey struct {
	provider, subject string
}

type memorySession struct {
	userID    string
	expiresAt time.Time
}

// MemoryStore keeps everything in process. It backs the test suite; data is
// lost on restart.
type MemoryStore struct {
	mu         sync.Mutex
	users      map[string]*User       // keyed by ID
	identities map[identityKey]string // → user ID
	sessions   map[string]memorySession
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:      make(map[string]*User),
		identities: make(map[identityKey]string),
		sessions:   make(map[string]memorySession),
	}
}

func (s *MemoryStore) ResolveExternalLogin(_ context.Context, ident oauth.Identity) (User, error) {
	if ident.Provider == "" || ident.Subject == "" {
		return User{}, errors.New("identity is missing provider or subject")
	}
	key := identityKey{ident.Provider, ident.Subject}

	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.identities[key]; ok {
		return *s.users[id], nil
	}

	id, err := newUserID()
	if err != nil {
		return User{}, err
	}
	u := newAccount(id, ident)
	s.users[id] = &u
	s.identities[key] = id
	return u, nil
}

func (s *MemoryStore) CreateSession(_ context.Context, userID string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.sessions[token] = memorySession{userID: userID, expiresAt: time.Now().Add(sessionTTL)}
	s.mu.Unlock()
	return token, nil
}

func (s *MemoryStore) UserForToken(_ context.Context, token string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[token]
	if !ok {
		return User{}, ErrInvalidToken
	}
	if time.Now().After(sess.expiresAt) {
		delete(s.sessions, token)
		return User{}, ErrInvalidToken
	}
	u, ok := s.users[sess.userID]
	if !ok {
		return User{}, ErrInvalidToken
	}
	return *u, nil
}

func (s *MemoryStore) Revoke(_ context.Context, token string) error {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	return nil
}
