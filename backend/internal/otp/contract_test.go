package otp

import (
	"context"
	"errors"
	"testing"
)

// harness is a Provider under test plus a way to read the code it delivered,
// which a real phone would receive by SMS.
type harness struct {
	provider Provider
	codeFor  func(t *testing.T, phone string) string
}

// runContract is the behaviour every adapter must have. A new vendor's adapter
// is correct when it passes this suite.
func runContract(t *testing.T, newHarness func(t *testing.T) harness) {
	ctx := context.Background()
	const phone = "+919876543210"

	t.Run("correct code succeeds once", func(t *testing.T) {
		h := newHarness(t)
		if err := h.provider.Start(ctx, phone, SMS); err != nil {
			t.Fatal(err)
		}
		code := h.codeFor(t, phone)
		if err := h.provider.Check(ctx, phone, code); err != nil {
			t.Fatalf("correct code: %v", err)
		}
		if err := h.provider.Check(ctx, phone, code); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("reused code: err = %v, want ErrInvalidCode", err)
		}
	})

	t.Run("wrong code fails", func(t *testing.T) {
		h := newHarness(t)
		if err := h.provider.Start(ctx, phone, SMS); err != nil {
			t.Fatal(err)
		}
		if err := h.provider.Check(ctx, phone, wrongCode(h.codeFor(t, phone))); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("err = %v, want ErrInvalidCode", err)
		}
	})

	t.Run("check without start fails", func(t *testing.T) {
		h := newHarness(t)
		if err := h.provider.Check(ctx, phone, "123456"); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("err = %v, want ErrInvalidCode", err)
		}
	})

	t.Run("codes are per number", func(t *testing.T) {
		h := newHarness(t)
		if err := h.provider.Start(ctx, phone, SMS); err != nil {
			t.Fatal(err)
		}
		if err := h.provider.Check(ctx, "+919999999999", h.codeFor(t, phone)); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("another number's code: err = %v, want ErrInvalidCode", err)
		}
	})

	t.Run("a new start replaces the old code", func(t *testing.T) {
		h := newHarness(t)
		if err := h.provider.Start(ctx, phone, SMS); err != nil {
			t.Fatal(err)
		}
		old := h.codeFor(t, phone)
		if err := h.provider.Start(ctx, phone, SMS); err != nil {
			t.Fatal(err)
		}
		current := h.codeFor(t, phone)
		if old != current {
			if err := h.provider.Check(ctx, phone, old); !errors.Is(err, ErrInvalidCode) {
				t.Errorf("old code: err = %v, want ErrInvalidCode", err)
			}
		}
		if err := h.provider.Check(ctx, phone, current); err != nil {
			t.Errorf("current code: %v", err)
		}
	})

	t.Run("too many wrong guesses lock the code", func(t *testing.T) {
		h := newHarness(t)
		if err := h.provider.Start(ctx, phone, SMS); err != nil {
			t.Fatal(err)
		}
		code := h.codeFor(t, phone)
		for range 5 {
			_ = h.provider.Check(ctx, phone, wrongCode(code))
		}
		// Vendors differ on which error they report once locked (Twilio
		// deletes the verification), but the right code must no longer work.
		err := h.provider.Check(ctx, phone, code)
		if !errors.Is(err, ErrTooManyAttempts) && !errors.Is(err, ErrInvalidCode) {
			t.Errorf("right code after lockout: err = %v, want ErrTooManyAttempts or ErrInvalidCode", err)
		}
	})
}

// wrongCode returns a same-length code that differs from code.
func wrongCode(code string) string {
	if code == "000000" {
		return "000001"
	}
	return "000000"
}
