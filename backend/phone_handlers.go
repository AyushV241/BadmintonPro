package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
	"github.com/AyushV241/BadmintonPro/backend/internal/otp"
)

// phoneIdentityProvider is the user_identities.provider value for phone
// logins; the subject is the E.164 number. No OAuth provider may use this
// name (see configureProviders).
const phoneIdentityProvider = "phone"

// PhoneLogin enables sign-in with a one-time code sent to a mobile number.
type PhoneLogin struct {
	Provider otp.Provider
	Policy   phonePolicy
	// ClientIPHeader names a header, set by trusted infrastructure, that
	// carries the caller's IP. Empty disables the per-IP limits.
	ClientIPHeader string

	limits phoneLimits
}

// phoneLimits cap how often codes are sent and checked. Every SMS costs money
// and SMS pumping fraud triggers thousands, so sends are limited per number,
// per IP (when known) and overall. The global cap bounds the worst-case bill
// however many numbers or IPs an attacker uses.
type phoneLimits struct {
	sendCooldown  *limiter // per number: one send per 30s
	sendPerPhone  *limiter // per number: 5 an hour
	sendPerIP     *limiter // per IP: 10 an hour
	sendGlobal    *limiter // everyone: sendGlobalPerHour an hour
	checkPerPhone *limiter // per number: 10 every 10 minutes
	checkPerIP    *limiter // per IP: 30 an hour
}

func newPhoneLimits(sendGlobalPerHour int) phoneLimits {
	return phoneLimits{
		sendCooldown:  newLimiter(1, 30*time.Second),
		sendPerPhone:  newLimiter(5, time.Hour),
		sendPerIP:     newLimiter(10, time.Hour),
		sendGlobal:    newLimiter(sendGlobalPerHour, time.Hour),
		checkPerPhone: newLimiter(10, 10*time.Minute),
		checkPerIP:    newLimiter(30, time.Hour),
	}
}

// NewPhoneLogin wires a provider and policy with the default limits.
func NewPhoneLogin(provider otp.Provider, policy phonePolicy, clientIPHeader string, sendGlobalPerHour int) *PhoneLogin {
	return &PhoneLogin{
		Provider:       provider,
		Policy:         policy,
		ClientIPHeader: clientIPHeader,
		limits:         newPhoneLimits(sendGlobalPerHour),
	}
}

type phoneStartRequest struct {
	Phone string `json:"phone"`
}

type phoneStartResponse struct {
	// Phone is the number the code went to, in E.164, for the page to show
	// and send back with the code.
	Phone string `json:"phone"`
}

type phoneVerifyRequest struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

var sixDigits = regexp.MustCompile(`^\d{6}$`)

// handlePhoneStart sends a code. The response is identical whether or not the
// number already has an account, so it can't be used to discover accounts.
func (a *API) handlePhoneStart(w http.ResponseWriter, r *http.Request) {
	pl := a.phone
	var req phoneStartRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}

	ip := clientIP(r, pl.ClientIPHeader)
	// Per-IP first, before any parsing work, then the number-specific limits.
	if ok, wait := allowAll(rule{pl.limits.sendPerIP, ip}); !ok {
		writeRateLimited(w, wait, "too many codes requested")
		return
	}
	phone, err := pl.Policy.normalize(req.Phone)
	if err != nil {
		writeError(w, http.StatusBadRequest, phoneErrorMessage(err))
		return
	}
	if ok, wait := allowAll(
		rule{pl.limits.sendCooldown, phone},
		rule{pl.limits.sendPerPhone, phone},
		rule{pl.limits.sendGlobal, "all"},
	); !ok {
		writeRateLimited(w, wait, "too many codes requested")
		return
	}

	err = pl.Provider.Start(r.Context(), phone, otp.SMS)
	switch {
	case errors.Is(err, otp.ErrUnsupportedPhone):
		writeError(w, http.StatusBadRequest, "this number can't receive SMS codes")
		return
	case errors.Is(err, otp.ErrTooManyAttempts):
		writeRateLimited(w, 10*time.Minute, "too many codes requested")
		return
	case err != nil:
		log.Printf("phone start (%s): %v", pl.Provider.Name(), err)
		writeError(w, http.StatusBadGateway, "couldn't send a code right now, please try again")
		return
	}
	writeJSON(w, http.StatusOK, phoneStartResponse{Phone: phone})
}

// handlePhoneVerify checks a code and signs the number's account in, creating
// the account on its first login.
func (a *API) handlePhoneVerify(w http.ResponseWriter, r *http.Request) {
	pl := a.phone
	var req phoneVerifyRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}

	ip := clientIP(r, pl.ClientIPHeader)
	if ok, wait := allowAll(rule{pl.limits.checkPerIP, ip}); !ok {
		writeRateLimited(w, wait, "too many attempts")
		return
	}
	phone, err := pl.Policy.normalize(req.Phone)
	if err != nil {
		writeError(w, http.StatusBadRequest, phoneErrorMessage(err))
		return
	}
	if ok, wait := allowAll(rule{pl.limits.checkPerPhone, phone}); !ok {
		writeRateLimited(w, wait, "too many attempts")
		return
	}
	if !sixDigits.MatchString(req.Code) {
		writeError(w, http.StatusUnauthorized, "incorrect or expired code")
		return
	}

	err = pl.Provider.Check(r.Context(), phone, req.Code)
	switch {
	case errors.Is(err, otp.ErrInvalidCode):
		writeError(w, http.StatusUnauthorized, "incorrect or expired code")
		return
	case errors.Is(err, otp.ErrTooManyAttempts):
		writeRateLimited(w, 10*time.Minute, "too many attempts, request a new code")
		return
	case err != nil:
		log.Printf("phone verify (%s): %v", pl.Provider.Name(), err)
		writeError(w, http.StatusBadGateway, "couldn't check the code right now, please try again")
		return
	}

	// No email, and never EmailVerified: a verified phone proves nothing about
	// an email address, so decideLink can only create or log in, never attach
	// to or take over an email account.
	user, err := a.store.ResolveExternalLogin(r.Context(), oauth.Identity{
		Provider: phoneIdentityProvider,
		Subject:  phone,
	})
	if err != nil {
		log.Printf("phone verify: resolve login: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	if err := a.startSession(w, r, user.ID); err != nil {
		log.Printf("phone verify: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, userResponse{User: user})
}

func phoneErrorMessage(err error) string {
	if errors.Is(err, errPhoneRegionBlocked) {
		return "phone login isn't available for this country yet"
	}
	return "enter a valid mobile number"
}

// decodeSmallJSON decodes a small, strict JSON body, writing a 400 on failure.
func decodeSmallJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be valid JSON")
		return false
	}
	return true
}

// writeRateLimited sends 429 with Retry-After in whole seconds.
func writeRateLimited(w http.ResponseWriter, wait time.Duration, message string) {
	seconds := int(math.Ceil(wait.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", fmt.Sprint(seconds))
	writeError(w, http.StatusTooManyRequests, fmt.Sprintf("%s, try again in %d seconds", message, seconds))
}
