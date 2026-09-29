package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nyaruka/phonenumbers"
)

var (
	errInvalidPhone       = errors.New("not a valid mobile number")
	errPhoneRegionBlocked = errors.New("phone login is not available for this country")
)

// phonePolicy turns what a user typed into the one canonical form used
// everywhere else: E.164, e.g. +919876543210. Every Twilio call and every
// phone identity uses that form, so "+91 98765 43210", "098765 43210" and
// "9876543210" are the same account.
type phonePolicy struct {
	// defaultRegion applies to numbers typed without a country code.
	defaultRegion string
	// allowed limits phone login to countries the club serves. It is the
	// first line of defence against SMS fraud to premium-rate numbers.
	allowed map[string]bool
}

func newPhonePolicy(defaultRegion string, allowedRegions []string) (phonePolicy, error) {
	p := phonePolicy{defaultRegion: strings.ToUpper(defaultRegion), allowed: make(map[string]bool)}
	for _, region := range allowedRegions {
		region = strings.ToUpper(strings.TrimSpace(region))
		if region == "" {
			continue
		}
		if phonenumbers.GetCountryCodeForRegion(region) == 0 {
			return phonePolicy{}, fmt.Errorf("unknown phone region %q", region)
		}
		p.allowed[region] = true
	}
	if len(p.allowed) == 0 {
		return phonePolicy{}, errors.New("at least one allowed phone region is required")
	}
	if !p.allowed[p.defaultRegion] {
		return phonePolicy{}, fmt.Errorf("default phone region %q is not in the allowed regions", p.defaultRegion)
	}
	return p, nil
}

// normalize returns raw as E.164, or errInvalidPhone / errPhoneRegionBlocked.
func (p phonePolicy) normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 32 {
		return "", errInvalidPhone
	}
	num, err := phonenumbers.Parse(raw, p.defaultRegion)
	if err != nil || !phonenumbers.IsValidNumber(num) {
		return "", errInvalidPhone
	}
	// Codes go by SMS, so a number that can only be a landline is useless.
	switch phonenumbers.GetNumberType(num) {
	case phonenumbers.MOBILE, phonenumbers.FIXED_LINE_OR_MOBILE:
	default:
		return "", errInvalidPhone
	}
	if !p.allowed[phonenumbers.GetRegionCodeForNumber(num)] {
		return "", errPhoneRegionBlocked
	}
	return phonenumbers.Format(num, phonenumbers.E164), nil
}
