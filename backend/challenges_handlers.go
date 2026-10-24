package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type challengeResponse struct {
	Challenge Challenge `json:"challenge"`
}

type challengesResponse struct {
	Challenges []Challenge `json:"challenges"`
}

type playersResponse struct {
	Players []Player `json:"players"`
}

// writeChallengeError maps challenge errors to responses, like
// writeEntryError: a rule that stops a valid request (closed, full, over a
// limit, the wrong state) is a 409. Players read these.
func writeChallengeError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, errValidation):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), errValidation.Error()+": "))
	case errors.Is(err, ErrNoSuchChallenge):
		writeError(w, http.StatusNotFound, "challenge not found")
	case errors.Is(err, ErrNoSuchEvent):
		writeError(w, http.StatusNotFound, "event not found")
	case errors.Is(err, ErrNoSuchUser):
		writeError(w, http.StatusNotFound, "no player with that username")
	case errors.Is(err, ErrNotChallengeOpponent):
		writeError(w, http.StatusForbidden, "only the player who was challenged can answer it")
	case errors.Is(err, ErrChallengeExpired):
		writeError(w, http.StatusConflict, "this challenge has expired")
	case errors.Is(err, ErrChallengeNotPending):
		writeError(w, http.StatusConflict, "this challenge has already been answered")
	case errors.Is(err, ErrChallengeNotWithdrawable):
		writeError(w, http.StatusConflict, "this challenge can't be withdrawn now")
	case errors.Is(err, ErrChallengeExists):
		writeError(w, http.StatusConflict, "you already have an open challenge with this player for this event")
	case errors.Is(err, ErrEventNotOpen):
		writeError(w, http.StatusConflict, "this event isn't open for joining")
	case errors.Is(err, ErrRegistrationClosed):
		writeError(w, http.StatusConflict, "joining has closed for this event")
	case errors.Is(err, ErrOutsideBand):
		writeError(w, http.StatusConflict, "your rating is outside this event's rating band")
	case errors.Is(err, ErrOtherOutsideBand):
		writeError(w, http.StatusConflict, "the other player's rating is outside this event's rating band")
	case errors.Is(err, ErrEntryLimit):
		writeError(w, http.StatusConflict, fmt.Sprintf("you already have %d entries in this event, the most allowed", maxEntriesPerPlayer))
	case errors.Is(err, ErrOtherEntryLimit):
		writeError(w, http.StatusConflict, fmt.Sprintf("the other player already has %d entries in this event, the most allowed", maxEntriesPerPlayer))
	case errors.Is(err, ErrOtherProfileIncomplete):
		writeError(w, http.StatusConflict, "the other player hasn't finished setting up their profile")
	case errors.Is(err, ErrEventFull):
		writeError(w, http.StatusConflict, "this event doesn't have room for two more players")
	case errors.Is(err, ErrNoMatchesLeft):
		writeError(w, http.StatusConflict, "this event has no matches left to give out")
	default:
		log.Printf("challenges %s: %v", action, err)
		writeError(w, http.StatusInternalServerError, "couldn't "+action)
	}
}

// handleCreateChallenge sends a singles challenge: the opponent, an event
// and a preferred start inside its window. From now the opponent has to
// accept and both have to pay within 4 hours (or by the registration cutoff
// if sooner).
//
//	POST /api/challenges  {"eventId","opponentUsername":"@ravi","preferredStart":"2026-10-17T19:30:00+05:30"}
func (a *API) handleCreateChallenge(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if !user.ProfileComplete {
		writeError(w, http.StatusForbidden, "finish setting up your profile before challenging anyone")
		return
	}
	var req challengeRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	eventID, opponent, preferred, err := validateChallenge(req)
	if err != nil {
		writeChallengeError(w, err, "send the challenge")
		return
	}
	ch, err := a.challenges.CreateChallenge(r.Context(), ChallengeInput{
		ChallengerID: user.ID, EventID: eventID, OpponentUsername: opponent,
		PreferredStart: preferred, Now: time.Now(),
	})
	if err != nil {
		writeChallengeError(w, err, "send the challenge")
		return
	}
	// TODO(step 11, notifications): tell the opponent. Until then they see
	// it in GET /api/me/challenges.
	writeJSON(w, http.StatusCreated, challengeResponse{Challenge: ch})
}

// handleListMyChallenges lists the challenges the caller sent and received:
// open ones first, then the rest, newest first. Any past their deadline are
// expired first.
//
//	GET /api/me/challenges
func (a *API) handleListMyChallenges(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	_, refunds, err := a.challenges.ExpireChallenges(ctx, time.Now(), user.ID)
	a.refundAll(ctx, refunds)
	if err != nil {
		writeChallengeError(w, err, "load your challenges")
		return
	}
	list, err := a.challenges.ListUserChallenges(ctx, user.ID)
	if err != nil {
		writeChallengeError(w, err, "load your challenges")
		return
	}
	writeJSON(w, http.StatusOK, challengesResponse{Challenges: list})
}

// handleGetChallenge returns one of the caller's challenges; anyone else's
// is a 404.
//
//	GET /api/challenges/{id}
func (a *API) handleGetChallenge(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	out, err := a.challenges.GetChallenge(ctx, user.ID, r.PathValue("id"), time.Now())
	a.refundAll(ctx, out.Refunds)
	if err != nil {
		writeChallengeError(w, err, "load the challenge")
		return
	}
	writeJSON(w, http.StatusOK, challengeResponse{Challenge: out.Challenge})
}

// challengeAction is one of the store's answers or withdrawals.
type challengeAction func(ctx context.Context, userID, challengeID string, now time.Time) (ChallengeOutcome, error)

// runChallengeAction runs an action on the user's challenge, refunds
// whatever it released after it committed, and returns the challenge.
func (a *API) runChallengeAction(w http.ResponseWriter, r *http.Request, user User, act challengeAction, action string) {
	ctx := r.Context()
	out, err := act(ctx, user.ID, r.PathValue("id"), time.Now())
	a.refundAll(ctx, out.Refunds)
	if err != nil {
		writeChallengeError(w, err, action)
		return
	}
	writeJSON(w, http.StatusOK, challengeResponse{Challenge: out.Challenge})
}

// handleAcceptChallenge accepts a pending challenge as its opponent. The
// rules are checked again, and a locked entry is made for each player,
// held until the challenge's deadline; each pays for theirs with POST
// /api/me/entries/{id}/pay. The match is fixed when the second payment
// lands.
//
//	POST /api/challenges/{id}/accept
func (a *API) handleAcceptChallenge(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if !user.ProfileComplete {
		writeError(w, http.StatusForbidden, "finish setting up your profile before accepting a challenge")
		return
	}
	a.runChallengeAction(w, r, user, a.challenges.AcceptChallenge, "accept the challenge")
}

// handleDeclineChallenge declines a pending challenge as its opponent.
//
//	POST /api/challenges/{id}/decline
func (a *API) handleDeclineChallenge(w http.ResponseWriter, r *http.Request) {
	if user, ok := a.requireUser(w, r); ok {
		a.runChallengeAction(w, r, user, a.challenges.DeclineChallenge, "decline the challenge")
	}
}

// handleWithdrawChallenge pulls out of a challenge before its match is
// fixed: the challenger any time before then, the opponent once they've
// accepted. Unpaid entries are cancelled and paid ones refunded.
//
//	POST /api/challenges/{id}/withdraw
func (a *API) handleWithdrawChallenge(w http.ResponseWriter, r *http.Request) {
	if user, ok := a.requireUser(w, r); ok {
		a.runChallengeAction(w, r, user, a.challenges.WithdrawChallenge, "withdraw the challenge")
	}
}

// handleRecentPlayers lists people the caller has played or challenged,
// most recent first, for the challenge sheet.
//
//	GET /api/me/recent-players
func (a *API) handleRecentPlayers(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	players, err := a.challenges.RecentPlayers(r.Context(), user.ID)
	if err != nil {
		writeChallengeError(w, err, "load your recent players")
		return
	}
	writeJSON(w, http.StatusOK, playersResponse{Players: players})
}

// handleNearbyPlayers suggests players to challenge on Home: people with a
// recent entry at a venue near the caller, closest rating first.
//
//	GET /api/players/nearby?lat=12.93&lng=77.62&radiusKm=10
func (a *API) handleNearbyPlayers(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	near, radius, err := parseNear(r.URL.Query())
	if err == nil && near == nil {
		err = errors.New("lat and lng are required")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	players, err := a.challenges.NearbyPlayers(r.Context(), user.ID, *near, radius, time.Now())
	if err != nil {
		writeChallengeError(w, err, "load players near you")
		return
	}
	writeJSON(w, http.StatusOK, playersResponse{Players: players})
}

// expireChallenges expires every challenge past its deadline and refunds
// anyone who had paid. The sweeper calls it; reads and actions also expire
// lazily, so nothing depends on its timing.
func (a *API) expireChallenges(ctx context.Context, now time.Time) {
	n, refunds, err := a.challenges.ExpireChallenges(ctx, now, "")
	a.refundAll(ctx, refunds)
	if err != nil {
		log.Printf("challenges: expire: %v", err)
	}
	if n > 0 {
		log.Printf("challenges: expired %d", n)
	}
}

// sweepChallenges runs expireChallenges every challengeSweepEvery until ctx
// is done.
func (a *API) sweepChallenges(ctx context.Context) {
	ticker := time.NewTicker(challengeSweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.expireChallenges(ctx, time.Now())
		}
	}
}
