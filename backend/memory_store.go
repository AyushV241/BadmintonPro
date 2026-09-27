package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
	"golang.org/x/crypto/bcrypt"
)

type memoryUser struct {
	User
	passwordHash []byte // nil when the account has no password
}

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
	users      map[string]*memoryUser // keyed by ID
	byEmail    map[string]string      // normalised email → user ID
	identities map[identityKey]string // → user ID
	sessions   map[string]memorySession
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:      make(map[string]*memoryUser),
		byEmail:    make(map[string]string),
		identities: make(map[identityKey]string),
		sessions:   make(map[string]memorySession),
	}
}

func (s *MemoryStore) CreateUser(_ context.Context, nu NewUser) (User, error) {
	hash, err := hashPassword(nu.Password)
	if err != nil {
		return User{}, err
	}
	id := nu.ID
	if id == "" {
		if id, err = newUserID(); err != nil {
			return User{}, err
		}
	}
	email := normaliseEmail(nu.Email)

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, taken := s.byEmail[email]; taken {
		return User{}, ErrEmailTaken
	}
	u := &memoryUser{
		User:         User{ID: id, Name: nu.Name, Email: email, EmailVerified: nu.EmailVerified},
		passwordHash: hash,
	}
	s.users[id] = u
	s.byEmail[email] = id
	return u.User, nil
}

func (s *MemoryStore) VerifyPassword(_ context.Context, email, password string) (User, error) {
	s.mu.Lock()
	var (
		user User
		hash []byte
	)
	if id, ok := s.byEmail[normaliseEmail(email)]; ok {
		user, hash = s.users[id].User, s.users[id].passwordHash
	}
	s.mu.Unlock()

	// bcrypt runs outside the lock so slow comparisons don't serialise logins.
	if hash == nil {
		equaliseTiming(password)
		return User{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (s *MemoryStore) ResolveExternalLogin(_ context.Context, ident oauth.Identity) (User, error) {
	if ident.Provider == "" || ident.Subject == "" {
		return User{}, errors.New("identity is missing provider or subject")
	}
	key := identityKey{ident.Provider, ident.Subject}
	email := normaliseEmail(ident.Email)

	s.mu.Lock()
	defer s.mu.Unlock()

	linkedID, linked := s.identities[key]

	var owner *emailOwner
	if !linked && email != "" {
		if id, ok := s.byEmail[email]; ok {
			owner = &emailOwner{userID: id, emailVerified: s.users[id].EmailVerified}
		}
	}

	switch decideLink(linked, owner, ident.EmailVerified) {
	case linkLogin:
		return s.users[linkedID].User, nil

	case linkCreate:
		id, err := newUserID()
		if err != nil {
			return User{}, err
		}
		u := &memoryUser{User: User{ID: id, Name: displayName(ident)}}
		// Only an email the provider vouches for may claim the address.
		if ident.EmailVerified && email != "" {
			u.Email, u.EmailVerified = email, true
			s.byEmail[email] = id
		}
		s.users[id] = u
		s.identities[key] = id
		return u.User, nil

	case linkAttach:
		s.identities[key] = owner.userID
		return s.users[owner.userID].User, nil

	case linkTakeover:
		u := s.users[owner.userID]
		u.passwordHash = nil
		u.EmailVerified = true
		for token, sess := range s.sessions {
			if sess.userID == owner.userID {
				delete(s.sessions, token)
			}
		}
		s.identities[key] = owner.userID
		return u.User, nil

	default: // linkRefuse
		return User{}, ErrAccountConflict
	}
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
	return u.User, nil
}

func (s *MemoryStore) Revoke(_ context.Context, token string) error {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	return nil
}
