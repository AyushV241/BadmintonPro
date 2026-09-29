package main

import (
	"context"
	"errors"
	"strings"
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
	usernames  map[string]string      // lowercased username → user ID
	sessions   map[string]memorySession
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:      make(map[string]*User),
		usernames:  make(map[string]string),
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

func (s *MemoryStore) UpdateProfile(_ context.Context, userID string, p ProfileUpdate) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.users[userID]
	if !ok {
		return User{}, ErrNoSuchUser
	}
	key := strings.ToLower(p.Username)
	if owner, taken := s.usernames[key]; taken && owner != userID {
		return User{}, ErrUsernameTaken
	}
	delete(s.usernames, strings.ToLower(u.Username))
	s.usernames[key] = userID

	u.Name, u.Username = p.Name, p.Username
	if p.Email != nil {
		u.Email, u.EmailVerified = *p.Email, false
	}
	*u = withProfileStatus(*u)
	return *u, nil
}

func (s *MemoryStore) SetVerifiedPhone(_ context.Context, userID, phone string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.users[userID]
	if !ok {
		return User{}, ErrNoSuchUser
	}
	u.Phone, u.PhoneVerified = phone, true
	return *u, nil
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
