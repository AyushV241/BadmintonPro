package otp

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"
)

// recorder captures delivered codes, standing in for a phone.
type recorder struct {
	mu    sync.Mutex
	codes map[string]string
}

func (r *recorder) deliver(phone, code string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.codes[phone] = code
}

func (r *recorder) codeFor(t *testing.T, phone string) string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	code, ok := r.codes[phone]
	if !ok {
		t.Fatalf("no code delivered to %s", phone)
	}
	return code
}

func newMemoryHarness(t *testing.T) harness {
	r := &recorder{codes: make(map[string]string)}
	return harness{provider: NewMemory("memory", r.deliver), codeFor: r.codeFor}
}

func TestMemoryContract(t *testing.T) {
	runContract(t, newMemoryHarness)
}

func TestMemoryCodesAreSixDigits(t *testing.T) {
	r := &recorder{codes: make(map[string]string)}
	m := NewMemory("memory", r.deliver)
	for range 50 {
		if err := m.Start(context.Background(), "+15550100", SMS); err != nil {
			t.Fatal(err)
		}
		if code := r.codeFor(t, "+15550100"); !regexp.MustCompile(`^\d{6}$`).MatchString(code) {
			t.Fatalf("code %q is not six digits", code)
		}
	}
}

func TestMemoryCodesExpire(t *testing.T) {
	r := &recorder{codes: make(map[string]string)}
	m := NewMemory("memory", r.deliver)
	now := time.Now()
	m.now = func() time.Time { return now }

	if err := m.Start(context.Background(), "+15550100", SMS); err != nil {
		t.Fatal(err)
	}
	now = now.Add(codeTTL + time.Second)
	if err := m.Check(context.Background(), "+15550100", r.codeFor(t, "+15550100")); !errors.Is(err, ErrInvalidCode) {
		t.Errorf("expired code: err = %v, want ErrInvalidCode", err)
	}
}

func TestMemoryNeverStoresTheCode(t *testing.T) {
	r := &recorder{codes: make(map[string]string)}
	m := NewMemory("memory", r.deliver)
	if err := m.Start(context.Background(), "+15550100", SMS); err != nil {
		t.Fatal(err)
	}
	code := r.codeFor(t, "+15550100")
	for _, p := range m.codes {
		if string(p.hash[:]) == code {
			t.Fatal("the plain code is stored")
		}
	}
}
