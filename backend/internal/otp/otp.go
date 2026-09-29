// Package otp sends and checks one-time codes for phone login.
//
// Provider is the only thing the rest of the app depends on. Each vendor
// (Twilio Verify today) has an adapter that translates its API and error codes
// into this interface, so switching vendors means writing one adapter and
// changing OTP_PROVIDER, not touching handlers.
//
// The interface is "send a code / check a code", not "send an SMS". Managed
// services such as Twilio Verify generate, store and expire codes themselves,
// and a plain SMS gateway would get an adapter that does that part itself (see
// Memory), so both fit.
package otp

import (
	"context"
	"errors"
)

// Channel is how a code is delivered.
type Channel string

const (
	SMS      Channel = "sms"
	WhatsApp Channel = "whatsapp"
)

// Provider sends and checks one-time codes. Phone numbers are always E.164
// (+919876543210); normalising user input is the caller's job.
type Provider interface {
	// Name identifies the adapter in logs, e.g. "twilio".
	Name() string
	// Start sends a new code to phone. Starting again replaces any code
	// already outstanding for that number.
	Start(ctx context.Context, phone string, ch Channel) error
	// Check returns nil if code is the current code for phone. A correct code
	// is single-use.
	Check(ctx context.Context, phone, code string) error
}

// Adapters return these (possibly wrapped) so handlers never see vendor error
// codes. Any other error means the provider itself failed.
var (
	// ErrInvalidCode covers a wrong, expired, already-used or never-sent code.
	// They are deliberately indistinguishable to the caller.
	ErrInvalidCode = errors.New("otp: invalid or expired code")
	// ErrTooManyAttempts means too many sends or checks for this number.
	ErrTooManyAttempts = errors.New("otp: too many attempts")
	// ErrUnsupportedPhone means the number can't receive codes: invalid,
	// a landline, or blocked by the provider's fraud or country rules.
	ErrUnsupportedPhone = errors.New("otp: phone number not supported")
)
