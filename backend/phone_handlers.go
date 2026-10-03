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
//
// There is no rate limiting of our own yet (see TODO.md "Rate limiting"):
// only the vendor's limits apply, such as Twilio Verify's 5 sends per number
// per 10 minutes, which surface as ErrTooManyAttempts.
type PhoneLogin struct {
	Provider otp.Provider
	Policy   phonePolicy
}

// NewPhoneLogin wires a provider and policy.
func NewPhoneLogin(provider otp.Provider, policy phonePolicy) *PhoneLogin {
	return &PhoneLogin{Provider: provider, Policy: policy}
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

// handlePhoneStart sends a sign-in code. The response is identical whether or
// not the number already has an account, so it can't be used to discover
// accounts.
func (a *API) handlePhoneStart(w http.ResponseWriter, r *http.Request) {
	var req phoneStartRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	if phone, ok := a.sendCode(w, r, req.Phone); ok {
		writeJSON(w, http.StatusOK, phoneStartResponse{Phone: phone})
	}
}

// handlePhoneVerify checks a code and signs the number's account in, creating
// the account on its first login.
func (a *API) handlePhoneVerify(w http.ResponseWriter, r *http.Request) {
	var req phoneVerifyRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	phone, ok := a.checkCode(w, r, req.Phone, req.Code)
	if !ok {
		return
	}

	// No email, and never EmailVerified: a verified phone proves nothing about
	// an email address.
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

// sendCode normalises raw and sends a code. It is shared by phone sign-in and
// by verifying a contact phone, so both validate the same way. On failure it
// has already written the response.
func (a *API) sendCode(w http.ResponseWriter, r *http.Request, raw string) (string, bool) {
	pl := a.phone
	phone, err := pl.Policy.normalize(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, phoneErrorMessage(err))
		return "", false
	}

	err = pl.Provider.Start(r.Context(), phone, otp.SMS)
	switch {
	case errors.Is(err, otp.ErrUnsupportedPhone):
		writeError(w, http.StatusBadRequest, "this number can't receive SMS codes")
		return "", false
	case errors.Is(err, otp.ErrTooManyAttempts):
		writeRateLimited(w, 10*time.Minute, "too many codes requested")
		return "", false
	case err != nil:
		log.Printf("send code (%s): %v", pl.Provider.Name(), err)
		writeError(w, http.StatusBadGateway, "couldn't send a code right now, please try again")
		return "", false
	}
	return phone, true
}

// checkCode normalises raw and checks code. Each code still allows only a few
// wrong guesses (the provider enforces that). On failure it has already
// written the response.
func (a *API) checkCode(w http.ResponseWriter, r *http.Request, raw, code string) (string, bool) {
	pl := a.phone
	phone, err := pl.Policy.normalize(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, phoneErrorMessage(err))
		return "", false
	}
	if !sixDigits.MatchString(code) {
		writeError(w, http.StatusUnauthorized, "incorrect or expired code")
		return "", false
	}

	err = pl.Provider.Check(r.Context(), phone, code)
	switch {
	case errors.Is(err, otp.ErrInvalidCode):
		writeError(w, http.StatusUnauthorized, "incorrect or expired code")
		return "", false
	case errors.Is(err, otp.ErrTooManyAttempts):
		writeRateLimited(w, 10*time.Minute, "too many attempts, request a new code")
		return "", false
	case err != nil:
		log.Printf("check code (%s): %v", pl.Provider.Name(), err)
		writeError(w, http.StatusBadGateway, "couldn't check the code right now, please try again")
		return "", false
	}
	return phone, true
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

// writeRateLimited sends 429 with Retry-After in whole seconds. Used when the
// OTP provider reports too many attempts.
func writeRateLimited(w http.ResponseWriter, wait time.Duration, message string) {
	seconds := int(math.Ceil(wait.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", fmt.Sprint(seconds))
	writeError(w, http.StatusTooManyRequests, fmt.Sprintf("%s, try again in %d seconds", message, seconds))
}
