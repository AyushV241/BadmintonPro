package main

import (
	"errors"
	"log"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxNameLength  = 100
	maxEmailLength = 254 // RFC 5321 limit on a forward path
)

// A username is 3–20 characters: lowercase letters, digits, "_" and ".",
// starting with a letter or digit. It is stored lowercased, so "Asha" and
// "asha" are the same username.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.]{2,19}$`)

// reservedUsernames can't be taken, so they stay free for pages or staff.
var reservedUsernames = map[string]bool{
	"admin": true, "administrator": true, "api": true, "badmintonpro": true,
	"dashboard": true, "help": true, "login": true, "logout": true, "me": true,
	"root": true, "settings": true, "support": true, "system": true,
}

// requireUser returns the signed-in user, or writes a 401.
func (a *API) requireUser(w http.ResponseWriter, r *http.Request) (User, bool) {
	token := sessionToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "not signed in")
		return User{}, false
	}
	user, err := a.store.UserForToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session expired or invalid")
		return User{}, false
	}
	return user, true
}

type profileRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	// Email is optional and only for accounts without a verified email.
	// Omitted or null leaves it unchanged; "" clears it.
	Email *string `json:"email"`
}

// handleUpdateProfile saves the "Set up your profile" step.
func (a *API) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req profileRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}

	update := ProfileUpdate{
		Name:     strings.TrimSpace(req.Name),
		Username: strings.ToLower(strings.TrimSpace(req.Username)),
	}
	if msg := validateProfile(update.Name, update.Username); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if req.Email != nil {
		// A provider-verified email (Google's) can't be swapped for a typed,
		// unverified one.
		if user.EmailVerified {
			writeError(w, http.StatusBadRequest, "your email comes from your sign-in account and can't be changed here")
			return
		}
		email := normaliseEmail(*req.Email)
		if email != "" && !validEmail(email) {
			writeError(w, http.StatusBadRequest, "enter a valid email address")
			return
		}
		update.Email = &email
	}

	updated, err := a.store.UpdateProfile(r.Context(), user.ID, update)
	switch {
	case errors.Is(err, ErrUsernameTaken):
		writeError(w, http.StatusConflict, "that username is taken")
		return
	case err != nil:
		log.Printf("update profile: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, userResponse{User: updated})
}

// validateProfile returns a message for the first invalid field, or "".
func validateProfile(name, username string) string {
	switch {
	case name == "":
		return "name is required"
	case utf8.RuneCountInString(name) > maxNameLength:
		return "name must be at most 100 characters"
	case !usernamePattern.MatchString(username):
		return "username must be 3–20 characters: letters, numbers, _ and ., starting with a letter or number"
	case reservedUsernames[username]:
		return "that username is reserved"
	}
	return ""
}

// validEmail accepts a bare address only. mail.ParseAddress also accepts
// forms like "Name <a@b.c>", so the parsed address must equal the input.
func validEmail(email string) bool {
	if len(email) > maxEmailLength {
		return false
	}
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}

// handleContactPhoneStart sends a code to a phone a signed-in user wants on
// their profile. It uses the same sending path and limits as phone sign-in.
func (a *API) handleContactPhoneStart(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if user.SignInMethod == phoneIdentityProvider {
		writeError(w, http.StatusBadRequest, "your phone number is how you sign in, so it can't be changed here")
		return
	}
	var req phoneStartRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	if phone, ok := a.sendCode(w, r, req.Phone); ok {
		writeJSON(w, http.StatusOK, phoneStartResponse{Phone: phone})
	}
}

// handleContactPhoneVerify checks the code and saves the number as a
// verified contact phone. It does not make it a way to sign in: each sign-in
// method is its own account.
func (a *API) handleContactPhoneVerify(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if user.SignInMethod == phoneIdentityProvider {
		writeError(w, http.StatusBadRequest, "your phone number is how you sign in, so it can't be changed here")
		return
	}
	var req phoneVerifyRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	phone, ok := a.checkCode(w, r, req.Phone, req.Code)
	if !ok {
		return
	}
	updated, err := a.store.SetVerifiedPhone(r.Context(), user.ID, phone)
	if err != nil {
		log.Printf("set contact phone: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, userResponse{User: updated})
}
