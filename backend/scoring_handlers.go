package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// liveHeartbeat keeps idle streams open through proxies.
const liveHeartbeat = 15 * time.Second

type liveMatchResponse struct {
	Match LiveMatch `json:"match"`
}

type startMatchRequest struct {
	// FirstServer is the side serving the first rally, "A" or "B".
	FirstServer string `json:"firstServer"`
}

type retireRequest struct {
	// RetiredSide is the side that can't go on, "A" or "B".
	RetiredSide string `json:"retiredSide"`
}

type walkoverRequest struct {
	// WinnerSide is the side that turned up, "A" or "B".
	WinnerSide string `json:"winnerSide"`
}

type voidRequest struct {
	Reason string `json:"reason"`
}

// maxVoidReason is the longest void reason, in characters.
const maxVoidReason = 200

type pointRequest struct {
	Seq      int    `json:"seq"`
	Kind     string `json:"kind"`
	Side     string `json:"side"`
	ClientID string `json:"clientId"`
}

// registerScoringRoutes adds live scoring: anyone signed in can watch; only
// the match's own judge can start, score and finish it.
func (a *API) registerScoringRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/matches/{id}/live", a.handleLiveMatch)
	mux.HandleFunc("GET /api/matches/{id}/live/stream", a.handleLiveStream)
	mux.HandleFunc("POST /api/judge/matches/{id}/start", a.handleStartMatch)
	mux.HandleFunc("POST /api/judge/matches/{id}/points", a.handleAddPoint)
	mux.HandleFunc("POST /api/judge/matches/{id}/finish", a.handleFinishMatch)
	mux.HandleFunc("POST /api/judge/matches/{id}/retire", a.handleRetireMatch)
	mux.HandleFunc("POST /api/judge/matches/{id}/walkover", a.handleJudgeWalkover)
	mux.HandleFunc("POST /api/admin/matches/{id}/walkover", a.adminRoute(a.handleAdminWalkover))
	mux.HandleFunc("POST /api/admin/matches/{id}/void", a.adminRoute(a.handleAdminVoid))
	mux.HandleFunc("GET /api/players/{id}/ratings", a.handlePlayerRatings)
}

func writeScoringError(w http.ResponseWriter, err error, action string) {
	var early *TooEarlyError
	var seq *SeqMismatchError
	switch {
	case errors.Is(err, ErrNoSuchMatch):
		writeError(w, http.StatusNotFound, "match not found")
	case errors.Is(err, ErrNotYourMatch):
		writeError(w, http.StatusForbidden, ErrNotYourMatch.Error())
	case errors.As(err, &seq):
		// The judge's app reads nextSeq and resends its queue from there.
		writeJSON(w, http.StatusConflict, map[string]any{"error": seq.Error(), "nextSeq": seq.Expected})
	case errors.As(err, &early):
		writeJSON(w, http.StatusConflict, map[string]any{"error": early.Error(), "canStartFrom": early.From.UTC()})
	case errors.Is(err, ErrMatchNotStartable), errors.Is(err, ErrMatchNotLive),
		errors.Is(err, ErrMatchDecided), errors.Is(err, ErrMatchNotDecided), errors.Is(err, ErrNothingToUndo),
		errors.Is(err, ErrNoWalkover), errors.Is(err, ErrNotVoidable):
		writeError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("%s: %v", action, err)
		writeError(w, http.StatusInternalServerError, "couldn't "+action)
	}
}

func (a *API) handleLiveMatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireUser(w, r); !ok {
		return
	}
	m, err := a.scoring.LiveMatch(r.Context(), r.PathValue("id"))
	if err != nil {
		writeScoringError(w, err, "load the match")
		return
	}
	writeJSON(w, http.StatusOK, liveMatchResponse{Match: m})
}

// handleLiveStream sends the match as server-sent events: once on connect,
// again whenever it changes, and a heartbeat comment while idle. The stream
// ends once the match is over, after sending its final state.
func (a *API) handleLiveStream(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireUser(w, r); !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming isn't supported")
		return
	}
	id := r.PathValue("id")
	m, err := a.scoring.LiveMatch(r.Context(), id)
	if err != nil {
		writeScoringError(w, err, "load the match")
		return
	}

	changed, stop := a.live.subscribe(id)
	defer stop()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	// no-transform stops proxies compressing, and so buffering, the stream.
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(m LiveMatch) bool {
		b, err := json.Marshal(m)
		if err != nil {
			log.Printf("live stream: %v", err)
			return false
		}
		// The body is text/event-stream carrying json.Marshal output, never
		// HTML, so there's nothing for a browser to interpret as markup.
		if _, err := fmt.Fprintf(w, "event: match\ndata: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	over := func(m LiveMatch) bool {
		switch m.Status {
		case MatchCompleted, MatchRetired, MatchWalkover, MatchCancelled, MatchVoid:
			return true
		}
		return false
	}
	// A change usually arrives twice (this instance's publish and the
	// database's notification); the second finds nothing new to send.
	last := m
	unchanged := func(m LiveMatch) bool {
		return m.Status == last.Status && m.NextSeq == last.NextSeq
	}

	if !send(m) || over(m) {
		return
	}
	heartbeat := time.NewTicker(liveHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.live.done:
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-changed:
			m, err := a.scoring.LiveMatch(r.Context(), id)
			if err != nil {
				if r.Context().Err() == nil {
					log.Printf("live stream: %v", err)
				}
				return
			}
			if unchanged(m) {
				continue
			}
			last = m
			if !send(m) || over(m) {
				return
			}
		}
	}
}

// judgeAction runs a judge's write and answers with the match. Being a
// registered judge is checked on every request; being this match's judge is
// checked by the store under the match's lock.
func (a *API) judgeAction(w http.ResponseWriter, r *http.Request, action string,
	do func(judge User, matchID string) (LiveMatch, error)) {
	judge, ok := a.requireJudge(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	m, err := do(judge, id)
	if err != nil {
		writeScoringError(w, err, action)
		return
	}
	// This instance's viewers update at once; others hear the database's
	// notification.
	a.live.publish(id)
	writeJSON(w, http.StatusOK, liveMatchResponse{Match: m})
}

func (a *API) handleStartMatch(w http.ResponseWriter, r *http.Request) {
	var req startMatchRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	if req.FirstServer != "A" && req.FirstServer != "B" {
		writeError(w, http.StatusBadRequest, `firstServer must be "A" or "B"`)
		return
	}
	a.judgeAction(w, r, "start the match", func(judge User, id string) (LiveMatch, error) {
		return a.scoring.StartMatch(r.Context(), id, judge.ID, req.FirstServer, time.Now())
	})
}

func (a *API) handleAddPoint(w http.ResponseWriter, r *http.Request) {
	var req pointRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	req.ClientID = strings.TrimSpace(req.ClientID)
	switch {
	case req.Seq < 1:
		writeError(w, http.StatusBadRequest, "seq must be 1 or more")
		return
	case req.ClientID == "" || len(req.ClientID) > 64:
		writeError(w, http.StatusBadRequest, "clientId must be 1 to 64 characters")
		return
	case req.Kind == PointRally && req.Side != "A" && req.Side != "B":
		writeError(w, http.StatusBadRequest, `a point needs side "A" or "B"`)
		return
	case req.Kind == PointUndo && req.Side != "":
		writeError(w, http.StatusBadRequest, "an undo has no side")
		return
	case req.Kind != PointRally && req.Kind != PointUndo:
		writeError(w, http.StatusBadRequest, `kind must be "point" or "undo"`)
		return
	}
	a.judgeAction(w, r, "record the point", func(judge User, id string) (LiveMatch, error) {
		return a.scoring.AddPoint(r.Context(), id, judge.ID, PointInput(req))
	})
}

func (a *API) handleFinishMatch(w http.ResponseWriter, r *http.Request) {
	a.judgeAction(w, r, "finish the match", func(judge User, id string) (LiveMatch, error) {
		return a.scoring.FinishMatch(r.Context(), id, judge.ID, time.Now())
	})
}

func validSide(side string) bool { return side == "A" || side == "B" }

// handleRetireMatch ends a live match when a side can't go on.
//
//	POST /api/judge/matches/{id}/retire
func (a *API) handleRetireMatch(w http.ResponseWriter, r *http.Request) {
	var req retireRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	if !validSide(req.RetiredSide) {
		writeError(w, http.StatusBadRequest, `retiredSide must be "A" or "B"`)
		return
	}
	a.judgeAction(w, r, "record the retirement", func(judge User, id string) (LiveMatch, error) {
		return a.scoring.RetireMatch(r.Context(), id, judge.ID, req.RetiredSide, time.Now())
	})
}

// handleJudgeWalkover gives the judge's match to the side that turned up.
//
//	POST /api/judge/matches/{id}/walkover
func (a *API) handleJudgeWalkover(w http.ResponseWriter, r *http.Request) {
	var req walkoverRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	if !validSide(req.WinnerSide) {
		writeError(w, http.StatusBadRequest, `winnerSide must be "A" or "B"`)
		return
	}
	a.judgeAction(w, r, "record the walkover", func(judge User, id string) (LiveMatch, error) {
		return a.scoring.Walkover(r.Context(), id, judge.ID, req.WinnerSide, time.Now())
	})
}

// handleAdminWalkover records a walkover on any match.
//
//	POST /api/admin/matches/{id}/walkover
func (a *API) handleAdminWalkover(w http.ResponseWriter, r *http.Request, _ User) {
	var req walkoverRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	if !validSide(req.WinnerSide) {
		writeError(w, http.StatusBadRequest, `winnerSide must be "A" or "B"`)
		return
	}
	a.adminScoringAction(w, r, "record the walkover", func(id string) (LiveMatch, error) {
		return a.scoring.Walkover(r.Context(), id, "", req.WinnerSide, time.Now())
	})
}

// handleAdminVoid strikes off a match that started or ended.
//
//	POST /api/admin/matches/{id}/void
func (a *API) handleAdminVoid(w http.ResponseWriter, r *http.Request, admin User) {
	var req voidRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > maxVoidReason {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("give a reason, up to %d characters", maxVoidReason))
		return
	}
	a.adminScoringAction(w, r, "void the match", func(id string) (LiveMatch, error) {
		return a.scoring.VoidMatch(r.Context(), id, admin.ID, reason, time.Now())
	})
}

func (a *API) adminScoringAction(w http.ResponseWriter, r *http.Request, action string,
	do func(matchID string) (LiveMatch, error)) {
	id := r.PathValue("id")
	m, err := do(id)
	if err != nil {
		writeScoringError(w, err, action)
		return
	}
	a.live.publish(id)
	writeJSON(w, http.StatusOK, liveMatchResponse{Match: m})
}

// handlePlayerRatings is every rating system's view of a player: current
// ratings and recent history, for comparing the systems.
//
//	GET /api/players/{id}/ratings
func (a *API) handlePlayerRatings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireUser(w, r); !ok {
		return
	}
	out, err := a.scoring.PlayerRatings(r.Context(), r.PathValue("id"), time.Now())
	switch {
	case errors.Is(err, ErrNoSuchUser):
		writeError(w, http.StatusNotFound, "player not found")
	case err != nil:
		log.Printf("player ratings: %v", err)
		writeError(w, http.StatusInternalServerError, "couldn't load the ratings")
	default:
		writeJSON(w, http.StatusOK, out)
	}
}
