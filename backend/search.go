package main

import (
	"context"
	"log"
	"net/http"
	"time"
	"unicode/utf8"
)

// Username search, for picking a partner, a judge or (later) an opponent.
// See docs/design/events-and-matches.md, section 13.

const (
	searchResultsLimit = 20
	// Fuzzy matches are added only when the exact, prefix and contains tiers
	// find fewer than fuzzyBelow, and only for queries of fuzzyMinLength or
	// more: fuzzy matching on one or two letters is noise.
	fuzzyBelow     = 5
	fuzzyMinLength = 3
	fuzzyThreshold = 0.3
	// Usernames are at most 20 characters, so a longer query can't match
	// anything but fuzzily; cap it well above that.
	maxSearchQueryLength = 40
	// searchesPerMinute is per signed-in user. Typing with a 250 ms debounce
	// stays far below it; listing every user doesn't.
	searchesPerMinute = 30
)

// PublicUser is what one player may see of another: never phone or email.
type PublicUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// UserSearchStore finds players by username. Only PostgresStore implements
// it: ranking needs pg_trgm.
type UserSearchStore interface {
	// SearchUsers returns up to limit users matching q (already normalised and
	// non-empty), best first: exact, prefix, contains, then fuzzy. It leaves
	// out excludeID (the searcher) and users without a complete profile.
	SearchUsers(ctx context.Context, q, excludeID string, limit int) ([]PublicUser, error)
}

type usersResponse struct {
	Users []PublicUser `json:"users"`
}

// handleSearchUsers needs a session and is rate limited per user, so it
// can't be used to list every account.
//
//	GET /api/users/search?q=@ash
func (a *API) handleSearchUsers(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	q := normaliseUsername(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, usersResponse{Users: []PublicUser{}})
		return
	}
	if utf8.RuneCountInString(q) > maxSearchQueryLength {
		writeError(w, http.StatusBadRequest, "q is too long")
		return
	}
	if ok, wait := allowAll(rule{a.searchLimit, user.ID}); !ok {
		writeRateLimited(w, wait, "too many searches")
		return
	}

	users, err := a.users.SearchUsers(r.Context(), q, user.ID, searchResultsLimit)
	if err != nil {
		log.Printf("search users: %v", err)
		writeError(w, http.StatusInternalServerError, "couldn't search right now")
		return
	}
	writeJSON(w, http.StatusOK, usersResponse{Users: users})
}

// newSearchLimiter is the per-user search limit.
func newSearchLimiter() *limiter { return newLimiter(searchesPerMinute, time.Minute) }
