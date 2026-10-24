package main

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

// meResponse is GET /api/me: the account plus what its session is for.
type meResponse struct {
	User User `json:"user"`
	// Mode is the home page this session lands on, "player" or "judge".
	Mode string `json:"mode"`
	// IsJudge is whether the account is a registered judge right now.
	IsJudge bool `json:"isJudge"`
}

type registeredJudgeResponse struct {
	Judge RegisteredJudge `json:"judge"`
}

type registerJudgeRequest struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

type setModeRequest struct {
	Mode string `json:"mode"`
}

// registerJudgeRoutes adds the judge home page and switching between the
// player and judge views. The admin's judge list is in registerAdminRoutes.
func (a *API) registerJudgeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/judge/home", a.handleJudgeHome)
	mux.HandleFunc("POST /api/me/mode", a.handleSetMode)
}

// sessionInfo describes the signed-in user's session. A judge-mode session
// whose account is no longer a judge reports player mode.
func (a *API) sessionInfo(r *http.Request, user User) (meResponse, error) {
	resp := meResponse{User: user, Mode: SessionPlayer}
	if a.judges == nil {
		return resp, nil
	}
	isJudge, err := a.judges.IsJudge(r.Context(), user.ID)
	if err != nil {
		return meResponse{}, err
	}
	resp.IsJudge = isJudge
	if isJudge {
		mode, err := a.store.SessionMode(r.Context(), sessionToken(r))
		if err != nil && !errors.Is(err, ErrInvalidToken) {
			return meResponse{}, err
		}
		if mode == SessionJudge {
			resp.Mode = SessionJudge
		}
	}
	return resp, nil
}

// requireJudge returns the signed-in user if they're a registered judge, or
// writes an error. It checks the judges table on every request, so removing
// a judge takes effect at once.
func (a *API) requireJudge(w http.ResponseWriter, r *http.Request) (User, bool) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return User{}, false
	}
	isJudge, err := a.judges.IsJudge(r.Context(), user.ID)
	if err != nil {
		log.Printf("require judge: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return User{}, false
	}
	if !isJudge {
		writeError(w, http.StatusForbidden, "you aren't a registered judge")
		return User{}, false
	}
	return user, true
}

// finishJudgeLogin signs a verified number in as a judge. It never creates an
// account: only a number an admin registered gets in. The code was already
// checked, so only the number's owner learns whether it's registered.
func (a *API) finishJudgeLogin(w http.ResponseWriter, r *http.Request, phone string) {
	user, err := a.judges.JudgeForPhone(r.Context(), phone)
	if errors.Is(err, ErrNotAJudge) {
		writeError(w, http.StatusForbidden, "this number isn't registered as a judge. Ask an admin to add you")
		return
	}
	if err != nil {
		log.Printf("judge login: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	if err := a.startSessionAs(w, r, user.ID, SessionJudge); err != nil {
		log.Printf("judge login: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{User: user, Mode: SessionJudge, IsJudge: true})
}

func (a *API) handleJudgeHome(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireJudge(w, r)
	if !ok {
		return
	}
	home, err := a.judges.JudgeHome(r.Context(), user.ID, time.Now())
	if errors.Is(err, ErrNotAJudge) {
		writeError(w, http.StatusForbidden, "you aren't a registered judge")
		return
	}
	if err != nil {
		log.Printf("judge home: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, home)
}

// handleSetMode switches the session between the player and judge views.
// Judge view needs a registered judge; player view is open to everyone.
func (a *API) handleSetMode(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req setModeRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	switch req.Mode {
	case SessionPlayer:
	case SessionJudge:
		if _, ok := a.requireJudge(w, r); !ok {
			return
		}
	default:
		writeError(w, http.StatusBadRequest, `mode must be "player" or "judge"`)
		return
	}
	if err := a.store.SetSessionMode(r.Context(), sessionToken(r), req.Mode); err != nil {
		if errors.Is(err, ErrInvalidToken) {
			writeError(w, http.StatusUnauthorized, "session expired or invalid")
			return
		}
		log.Printf("set mode: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	resp, err := a.sessionInfo(r, user)
	if err != nil {
		log.Printf("set mode: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- the admin's judge list ---

func (a *API) handleAdminListJudges(w http.ResponseWriter, r *http.Request, _ User) {
	judges, err := a.judges.ListJudges(r.Context())
	if err != nil {
		log.Printf("admin list judges: %v", err)
		writeError(w, http.StatusInternalServerError, "couldn't list judges")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]RegisteredJudge{"judges": judges})
}

// handleAdminRegisterJudge registers a judge by phone number and name. The
// number's phone account is found, or created if it has never signed in, so
// it is the same account the person gets when they sign in to play.
func (a *API) handleAdminRegisterJudge(w http.ResponseWriter, r *http.Request, admin User) {
	if a.phone == nil {
		writeError(w, http.StatusServiceUnavailable, "phone login is disabled, so judges can't sign in")
		return
	}
	var req registerJudgeRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if msg := validateJudgeName(name); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	phone, err := a.phone.Policy.normalize(req.Phone)
	if err != nil {
		writeError(w, http.StatusBadRequest, phoneErrorMessage(err))
		return
	}

	// The account the number signs in to. A new one is named after the
	// judge; an existing one keeps the name its owner chose.
	account, err := a.store.ResolveExternalLogin(r.Context(), oauth.Identity{
		Provider: phoneIdentityProvider,
		Subject:  phone,
		Name:     name,
	})
	if err != nil {
		log.Printf("admin register judge: %v", err)
		writeError(w, http.StatusInternalServerError, "couldn't register the judge")
		return
	}
	judge, err := a.judges.AddJudge(r.Context(), account.ID, name, phone, admin.ID)
	if errors.Is(err, ErrJudgeExists) {
		writeError(w, http.StatusConflict, "that number is already a judge")
		return
	}
	if err != nil {
		log.Printf("admin register judge: %v", err)
		writeError(w, http.StatusInternalServerError, "couldn't register the judge")
		return
	}
	writeJSON(w, http.StatusCreated, registeredJudgeResponse{Judge: judge})
}

func (a *API) handleAdminDeleteJudge(w http.ResponseWriter, r *http.Request, _ User) {
	err := a.judges.RemoveJudge(r.Context(), r.PathValue("userId"))
	if errors.Is(err, ErrNotAJudge) {
		writeError(w, http.StatusNotFound, "judge not found")
		return
	}
	if err != nil {
		log.Printf("admin remove judge: %v", err)
		writeError(w, http.StatusInternalServerError, "couldn't remove the judge")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
