package main

import (
	"errors"
	"testing"
)

func mustPhonePolicy(t *testing.T, defaultRegion string, allowed ...string) phonePolicy {
	t.Helper()
	p, err := newPhonePolicy(defaultRegion, allowed)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPhoneNormalizeToE164(t *testing.T) {
	p := mustPhonePolicy(t, "IN", "IN")
	// Every way of typing the same number must give the same identity.
	for _, raw := range []string{
		"+91 98765 43210",
		"+919876543210",
		"98765 43210",
		"098765-43210",
		" (+91) 98765 43210 ",
	} {
		got, err := p.normalize(raw)
		if err != nil || got != "+919876543210" {
			t.Errorf("normalize(%q) = %q, %v; want +919876543210", raw, got, err)
		}
	}
}

func TestPhoneNormalizeRejects(t *testing.T) {
	p := mustPhonePolicy(t, "IN", "IN")
	for raw, want := range map[string]error{
		"":                  errInvalidPhone,
		"hello":             errInvalidPhone,
		"12345":             errInvalidPhone,
		"+91 11 2345 6789":  errInvalidPhone, // Delhi landline: can't receive SMS
		"+44 7400 123456":   errPhoneRegionBlocked,
		"+1 650 253 0000":   errPhoneRegionBlocked,
		"+9198765432101234": errInvalidPhone,
	} {
		if _, err := p.normalize(raw); !errors.Is(err, want) {
			t.Errorf("normalize(%q) err = %v, want %v", raw, err, want)
		}
	}
}

func TestPhonePolicyAllowsConfiguredRegions(t *testing.T) {
	p := mustPhonePolicy(t, "IN", "IN", "gb")
	if got, err := p.normalize("+44 7400 123456"); err != nil || got != "+447400123456" {
		t.Errorf("UK mobile = %q, %v; want +447400123456", got, err)
	}
}

func TestNewPhonePolicyValidates(t *testing.T) {
	for name, tc := range map[string]struct {
		def     string
		allowed []string
	}{
		"no regions":             {"IN", nil},
		"unknown region":         {"IN", []string{"IN", "XX"}},
		"default not in allowed": {"US", []string{"IN"}},
	} {
		if _, err := newPhonePolicy(tc.def, tc.allowed); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
