package otp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"math/big"
	"sync"
	"time"
)

const (
	codeDigits       = 6
	codeTTL          = 10 * time.Minute
	maxCheckAttempts = 5
)

// Memory generates, stores and checks codes itself and hands each new code to
// a deliver function. It is the local-development provider (deliver logs the
// code) and the test fake (deliver records it). Codes live in process memory,
// so it only suits a single instance.
type Memory struct {
	name    string
	deliver func(phone, code string)
	now     func() time.Time

	mu    sync.Mutex
	codes map[string]*pending
}

type pending struct {
	hash     [sha256.Size]byte // never keep the code itself
	expires  time.Time
	attempts int
}

// NewMemory returns a provider that calls deliver with every code it issues.
func NewMemory(name string, deliver func(phone, code string)) *Memory {
	return &Memory{name: name, deliver: deliver, now: time.Now, codes: make(map[string]*pending)}
}

func (m *Memory) Name() string { return m.name }

func (m *Memory) Start(_ context.Context, phone string, _ Channel) error {
	code, err := randomCode()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.codes[phone] = &pending{hash: sha256.Sum256([]byte(code)), expires: m.now().Add(codeTTL)}
	m.sweepLocked()
	m.mu.Unlock()

	m.deliver(phone, code)
	return nil
}

func (m *Memory) Check(_ context.Context, phone, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.codes[phone]
	if !ok || m.now().After(p.expires) {
		delete(m.codes, phone)
		return ErrInvalidCode
	}
	if p.attempts >= maxCheckAttempts {
		return ErrTooManyAttempts
	}
	p.attempts++

	sum := sha256.Sum256([]byte(code))
	if subtle.ConstantTimeCompare(sum[:], p.hash[:]) != 1 {
		return ErrInvalidCode
	}
	delete(m.codes, phone) // single-use
	return nil
}

// sweepLocked drops expired codes so abandoned logins don't accumulate.
func (m *Memory) sweepLocked() {
	now := m.now()
	for phone, p := range m.codes {
		if now.After(p.expires) {
			delete(m.codes, phone)
		}
	}
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", codeDigits, n.Int64()), nil
}
