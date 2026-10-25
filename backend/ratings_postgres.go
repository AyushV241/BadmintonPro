package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/rating"
	"github.com/jackc/pgx/v5"
)

// Rating writes in a discipline are serialised by a transaction-scoped
// advisory lock, so a void's replay sees a fixed history, and the history's
// IDs follow the order matches were rated in. It's taken after the match
// row's lock, and nothing takes them the other way round.
func lockRatings(ctx context.Context, tx pgx.Tx, discipline string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "ratings:"+discipline); err != nil {
		return fmt.Errorf("lock ratings: %w", err)
	}
	return nil
}

// ratedResult is what a finished match gives the rating systems.
type ratedResult struct {
	format string
	// rated is false for a match that doesn't change ratings: a walkover,
	// a retirement before any point, anything not finished.
	rated  bool
	scoreA float64
	games  []rating.Game
	at     time.Time
	// ids are the players, in user ID order; sideOf says which side.
	ids    []string
	sideOf map[string]string
}

// loadRatedResult works a match's result out from its row and point log, the
// same way when it ends and when a void replays it.
func loadRatedResult(ctx context.Context, tx pgx.Tx, matchID string) (ratedResult, error) {
	var (
		res         ratedResult
		status      string
		firstServer string
		winner      *string
		completedAt *time.Time
	)
	if err := tx.QueryRow(ctx, `
		SELECT status, format, COALESCE(first_server, ''), winner_side, completed_at
		FROM matches WHERE id = $1`, matchID,
	).Scan(&status, &res.format, &firstServer, &winner, &completedAt); err != nil {
		return ratedResult{}, fmt.Errorf("match result: %w", err)
	}
	if completedAt == nil || (status != MatchCompleted && status != MatchRetired) {
		return res, nil
	}
	events, err := loadPoints(ctx, tx, matchID)
	if err != nil {
		return ratedResult{}, err
	}
	score := computeScore(firstServer, events)
	switch status {
	case MatchCompleted:
		if winner == nil {
			return res, nil
		}
		if *winner == "A" {
			res.scoreA = 1
		}
	case MatchRetired:
		share, ok := retirementScore(score)
		if !ok {
			return res, nil // retired before a point was played
		}
		res.scoreA = share
	}
	res.games = gamesOf(score)
	res.at = completedAt.UTC()

	rows, err := tx.Query(ctx, `SELECT user_id, side FROM match_sides WHERE match_id = $1 ORDER BY user_id`, matchID)
	if err != nil {
		return ratedResult{}, fmt.Errorf("match sides: %w", err)
	}
	res.sideOf = map[string]string{}
	for rows.Next() {
		var id, side string
		if err := rows.Scan(&id, &side); err != nil {
			rows.Close()
			return ratedResult{}, fmt.Errorf("match sides: %w", err)
		}
		res.ids = append(res.ids, id)
		res.sideOf[id] = side
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ratedResult{}, fmt.Errorf("match sides: %w", err)
	}
	// A side with nobody on it can't be rated.
	res.rated = slices.ContainsFunc(res.ids, func(id string) bool { return res.sideOf[id] == "A" }) &&
		slices.ContainsFunc(res.ids, func(id string) bool { return res.sideOf[id] == "B" })
	return res, nil
}

// match builds the rating package's match from players' states.
func (res ratedResult) match(states map[string]rating.Player) rating.Match {
	m := rating.Match{ScoreA: res.scoreA, Games: res.games, At: res.at}
	for _, id := range res.ids {
		p := states[id]
		p.ID = id
		if res.sideOf[id] == "A" {
			m.A = append(m.A, p)
		} else {
			m.B = append(m.B, p)
		}
	}
	return m
}

// seed is a player's displayed rating in a discipline before a system first
// rates them there.
type seed struct {
	rating  float64
	matches int
}

// seeds are players' displayed ratings, by user ID, then discipline.
type seeds map[string]map[string]seed

func loadSeeds(ctx context.Context, tx pgx.Tx, ids []string) (seeds, error) {
	rows, err := tx.Query(ctx, `
		SELECT user_id, discipline, rating, matches_played FROM player_ratings WHERE user_id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("load ratings: %w", err)
	}
	out := seeds{}
	for rows.Next() {
		var id, discipline string
		var r, n int
		if err := rows.Scan(&id, &discipline, &r, &n); err != nil {
			rows.Close()
			return nil, fmt.Errorf("load ratings: %w", err)
		}
		if out[id] == nil {
			out[id] = map[string]seed{}
		}
		out[id][discipline] = seed{float64(r), n}
	}
	rows.Close()
	return out, rows.Err()
}

// loadStates is each player's state in a system and discipline, as of a
// match at at. A player the system hasn't rated there yet starts from:
//
//  1. a displayed rating from matches before the system (seeded data);
//  2. in doubles or mixed, their singles state, carried over (design doc,
//     section 9): same rating, more uncertainty;
//  3. otherwise a new player's state.
func loadStates(ctx context.Context, tx pgx.Tx, sys rating.System, discipline string, ids []string, seeded seeds, at time.Time) (map[string]rating.Player, error) {
	states, err := storedStates(ctx, tx, sys, discipline, ids)
	if err != nil {
		return nil, err
	}
	var singles map[string]rating.Player
	for _, id := range ids {
		if _, ok := states[id]; ok {
			continue
		}
		var p rating.Player
		switch s, ok := seeded[id][discipline]; {
		case ok && s.matches > 0:
			p = sys.Carry(s.rating, s.matches)
		case discipline != FormatSingles:
			if singles == nil {
				if singles, err = storedStates(ctx, tx, sys, FormatSingles, ids); err != nil {
					return nil, err
				}
			}
			from, ok := singles[id]
			if !ok {
				from = sys.Initial()
				if s, ok := seeded[id][FormatSingles]; ok && s.matches > 0 {
					from = sys.Carry(s.rating, s.matches)
				}
			}
			p = sys.Transfer(from, at)
		default:
			p = sys.Initial()
		}
		p.ID = id
		states[id] = p
	}
	return states, nil
}

// storedStates are the players' stored states in a system and discipline;
// players it hasn't rated there are missing.
func storedStates(ctx context.Context, tx pgx.Tx, sys rating.System, discipline string, ids []string) (map[string]rating.Player, error) {
	rows, err := tx.Query(ctx, `
		SELECT user_id, rating, rd, matches_played, last_rated_at FROM rating_states
		WHERE user_id = ANY($1) AND discipline = $2 AND system = $3`, ids, discipline, sys.Name())
	if err != nil {
		return nil, fmt.Errorf("load rating states: %w", err)
	}
	states := map[string]rating.Player{}
	for rows.Next() {
		var p rating.Player
		var rd *float64
		var last *time.Time
		if err := rows.Scan(&p.ID, &p.Rating, &rd, &p.Matches, &last); err != nil {
			rows.Close()
			return nil, fmt.Errorf("load rating states: %w", err)
		}
		if rd != nil {
			p.RD = *rd
		}
		if last != nil {
			p.LastRated = last.UTC()
		}
		states[p.ID] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load rating states: %w", err)
	}
	return states, nil
}

func nullableRD(rd float64) *float64 {
	if rd <= 0 {
		return nil
	}
	return &rd
}

func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// recordChange stores a player's new state in a system and appends it to
// the history. For the default system it also updates the rating players
// see.
func (s *PostgresStore) recordChange(ctx context.Context, tx pgx.Tx, sys rating.System, matchID, discipline, kind string,
	before, after rating.Player, details map[string]float64) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO rating_changes (match_id, user_id, discipline, system, kind,
			rating_before, rating_after, rd_before, rd_after, matches_before, matches_after,
			rated_at_before, rated_at_after, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		matchID, after.ID, discipline, sys.Name(), kind,
		before.Rating, after.Rating, nullableRD(before.RD), nullableRD(after.RD), before.Matches, after.Matches,
		nullableTime(before.LastRated), nullableTime(after.LastRated), raw); err != nil {
		return fmt.Errorf("record rating change: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO rating_states (user_id, discipline, system, rating, rd, matches_played, last_rated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, discipline, system) DO UPDATE
		SET rating = EXCLUDED.rating, rd = EXCLUDED.rd, matches_played = EXCLUDED.matches_played,
		    last_rated_at = EXCLUDED.last_rated_at, updated_at = now()`,
		after.ID, discipline, sys.Name(), after.Rating, nullableRD(after.RD), after.Matches, nullableTime(after.LastRated)); err != nil {
		return fmt.Errorf("save rating state: %w", err)
	}
	if sys.Name() != s.ratings.primary.Name() {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO player_ratings (user_id, discipline, rating, matches_played, provisional, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (user_id, discipline) DO UPDATE
		SET rating = EXCLUDED.rating, matches_played = EXCLUDED.matches_played,
		    provisional = EXCLUDED.provisional, updated_at = now()`,
		after.ID, discipline, shown(after.Rating), after.Matches, sys.Provisional(after)); err != nil {
		return fmt.Errorf("update rating: %w", err)
	}
	if discipline != FormatSingles {
		return nil
	}
	// Until a player has played doubles or mixed, their rating there is
	// their singles one, provisional, as that's where it will start: so
	// the matcher and events' rating bands judge them by it.
	if _, err := tx.Exec(ctx, `
		INSERT INTO player_ratings (user_id, discipline, rating, matches_played, provisional, updated_at)
		SELECT $1, d, $2, 0, true, now() FROM unnest($3::text[]) AS d
		WHERE NOT EXISTS (SELECT 1 FROM rating_states rs WHERE rs.user_id = $1 AND rs.discipline = d AND rs.system = $4)
		ON CONFLICT (user_id, discipline) DO UPDATE SET rating = EXCLUDED.rating, updated_at = now()
		WHERE player_ratings.matches_played = 0`,
		after.ID, shown(after.Rating), []string{FormatDoubles, FormatMixed}, sys.Name()); err != nil {
		return fmt.Errorf("update doubles ratings from singles: %w", err)
	}
	return nil
}

// applyRatings rates a match in every system, inside the transaction that
// ends it. A match is rated once: if it already has rating rows, nothing
// happens (and a unique key backs that up).
func (s *PostgresStore) applyRatings(ctx context.Context, tx pgx.Tx, matchID string) error {
	res, err := loadRatedResult(ctx, tx, matchID)
	if err != nil || !res.rated {
		return err
	}
	if err := lockRatings(ctx, tx, res.format); err != nil {
		return err
	}
	var done bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM rating_changes WHERE match_id = $1 AND kind = $2)`,
		matchID, ratingKindMatch).Scan(&done); err != nil {
		return fmt.Errorf("check ratings: %w", err)
	}
	if done {
		return nil
	}
	// Read before any system writes: a system that hasn't rated a player
	// starts from what they had before this match.
	seeded, err := loadSeeds(ctx, tx, res.ids)
	if err != nil {
		return err
	}
	for _, sys := range s.ratings.systems {
		states, err := loadStates(ctx, tx, sys, res.format, res.ids, seeded, res.at)
		if err != nil {
			return err
		}
		m := res.match(states)
		for _, c := range sys.Rate(m) {
			if err := s.recordChange(ctx, tx, sys, matchID, res.format, ratingKindMatch, c.Before, c.After, c.Details); err != nil {
				return err
			}
		}
	}
	return nil
}

// historyRow is one rating_changes row, as replay needs it.
type historyRow struct {
	matchID, userID, kind string
	before, after         rating.Player
}

func playerState(id string, r float64, rd *float64, n int, at *time.Time) rating.Player {
	p := rating.Player{ID: id, Rating: r, Matches: n}
	if rd != nil {
		p.RD = *rd
	}
	if at != nil {
		p.LastRated = at.UTC()
	}
	return p
}

// voidRatings undoes a voided match in every system (design doc, section 9).
// Each system's history from the match on is replayed without it: every
// player starts from their state just before it, and the matches rated
// since are rated again, in their original order and at their original
// times. Nobody has played since in the common case, so the match's players
// get their exact earlier state back. When someone has, everyone whose
// rating comes out different gets an "adjust" row; the voided match's own
// players get a "void" row either way. Original rows are never edited.
func (s *PostgresStore) voidRatings(ctx context.Context, tx pgx.Tx, matchID string) error {
	var discipline string
	if err := tx.QueryRow(ctx, `SELECT format FROM matches WHERE id = $1`, matchID).Scan(&discipline); err != nil {
		return fmt.Errorf("match format: %w", err)
	}
	if err := lockRatings(ctx, tx, discipline); err != nil {
		return err
	}
	for _, sys := range s.ratings.systems {
		if err := s.replayWithout(ctx, tx, sys, discipline, matchID); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) replayWithout(ctx context.Context, tx pgx.Tx, sys rating.System, discipline, voided string) error {
	var first *int64
	if err := tx.QueryRow(ctx, `
		SELECT min(id) FROM rating_changes WHERE match_id = $1 AND system = $2 AND kind = $3`,
		voided, sys.Name(), ratingKindMatch).Scan(&first); err != nil {
		return fmt.Errorf("find rating changes: %w", err)
	}
	if first == nil {
		return nil // never rated (a walkover, say): nothing to undo
	}

	rows, err := tx.Query(ctx, `
		SELECT match_id, user_id, kind,
			rating_before, rd_before, matches_before, rated_at_before,
			rating_after, rd_after, matches_after, rated_at_after
		FROM rating_changes
		WHERE discipline = $1 AND system = $2 AND id >= $3
		ORDER BY id`, discipline, sys.Name(), *first)
	if err != nil {
		return fmt.Errorf("load rating history: %w", err)
	}
	history, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (historyRow, error) {
		var (
			h                 historyRow
			rb, ra            float64
			rdb, rda          *float64
			nb, na            int
			atBefore, atAfter *time.Time
		)
		err := row.Scan(&h.matchID, &h.userID, &h.kind, &rb, &rdb, &nb, &atBefore, &ra, &rda, &na, &atAfter)
		h.before = playerState(h.userID, rb, rdb, nb, atBefore)
		h.after = playerState(h.userID, ra, rda, na, atAfter)
		return h, err
	})
	if err != nil {
		return fmt.Errorf("load rating history: %w", err)
	}

	// Each player's state just before the voided match (the before of their
	// first row from there) and now (the after of their last).
	start := map[string]rating.Player{}
	current := map[string]rating.Player{}
	var users, replay []string
	inVoided := map[string]bool{}
	for _, h := range history {
		if _, seen := start[h.userID]; !seen {
			start[h.userID] = h.before
			users = append(users, h.userID)
		}
		current[h.userID] = h.after
		if h.kind != ratingKindMatch {
			continue
		}
		if h.matchID == voided {
			inVoided[h.userID] = true
		} else if !slices.Contains(replay, h.matchID) {
			replay = append(replay, h.matchID)
		}
	}
	// Matches voided before this one stay out.
	if len(replay) > 0 {
		var stillVoid []string
		rows, err := tx.Query(ctx, `SELECT id FROM matches WHERE id = ANY($1) AND status = $2`, replay, MatchVoid)
		if err != nil {
			return fmt.Errorf("voided matches: %w", err)
		}
		if stillVoid, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return fmt.Errorf("voided matches: %w", err)
		}
		replay = slices.DeleteFunc(replay, func(id string) bool { return slices.Contains(stillVoid, id) })
	}

	states := map[string]rating.Player{}
	for id, p := range start {
		states[id] = p
	}
	for _, id := range replay {
		res, err := loadRatedResult(ctx, tx, id)
		if err != nil {
			return err
		}
		if !res.rated {
			continue
		}
		for _, c := range sys.Rate(res.match(states)) {
			states[c.PlayerID] = c.After
		}
	}

	slices.Sort(users)
	for _, id := range users {
		now, then := states[id], current[id]
		kind := ratingKindAdjust
		if inVoided[id] {
			kind = ratingKindVoid
		} else if sameState(now, then) {
			continue
		}
		if err := s.recordChange(ctx, tx, sys, voided, discipline, kind, then, now,
			map[string]float64{"replayed": float64(len(replay))}); err != nil {
			return err
		}
	}
	return nil
}

func sameState(a, b rating.Player) bool {
	return a.Rating == b.Rating && a.RD == b.RD && a.Matches == b.Matches && a.LastRated.Equal(b.LastRated)
}

// matchRatingChanges are a match's own rating changes in every system:
// rated, then voided. Adjustments to other players aren't the match's.
func (s *PostgresStore) matchRatingChanges(ctx context.Context, matchID string) ([]LiveRatingChange, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, system, kind, rating_before, rating_after, rd_before, rd_after,
			(details->>'winProbability')::float8
		FROM rating_changes WHERE match_id = $1 AND kind IN ($2, $3)
		ORDER BY id`, matchID, ratingKindMatch, ratingKindVoid)
	if err != nil {
		return nil, fmt.Errorf("rating changes: %w", err)
	}
	changes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (LiveRatingChange, error) {
		var (
			c        LiveRatingChange
			rb, ra   float64
			rdb, rda *float64
			win      *float64
		)
		err := row.Scan(&c.UserID, &c.System, &c.Kind, &rb, &ra, &rdb, &rda, &win)
		c.Before, c.After = shown(rb), shown(ra)
		c.Change = c.After - c.Before
		c.RDBefore, c.RDAfter = shownRD(rdb), shownRD(rda)
		c.WinProbability = roundedProbability(win)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("rating changes: %w", err)
	}
	if changes == nil {
		changes = []LiveRatingChange{}
	}
	return changes, nil
}

func roundedProbability(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := math.Round(*p*1000) / 1000
	return &v
}

// PlayerRatings is what every rating system says about a player, now and
// over their recent matches.
func (s *PostgresStore) PlayerRatings(ctx context.Context, userID string, now time.Time) (PlayerRatings, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
		return PlayerRatings{}, fmt.Errorf("find player: %w", err)
	}
	if !exists {
		return PlayerRatings{}, ErrNoSuchUser
	}
	out := PlayerRatings{UserID: userID, Default: s.ratings.primary.Name(), Ratings: []PlayerRating{}, History: []RatingHistoryEntry{}}

	rows, err := s.pool.Query(ctx, `
		SELECT system, discipline, rating, rd, matches_played, last_rated_at
		FROM rating_states WHERE user_id = $1 ORDER BY discipline, system`, userID)
	if err != nil {
		return PlayerRatings{}, fmt.Errorf("rating states: %w", err)
	}
	out.Ratings, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (PlayerRating, error) {
		var (
			pr   PlayerRating
			r    float64
			rd   *float64
			last *time.Time
		)
		if err := row.Scan(&pr.System, &pr.Discipline, &r, &rd, &pr.MatchesPlayed, &last); err != nil {
			return pr, err
		}
		pr.Rating, pr.RD, pr.LastRatedAt = shown(r), shownRD(rd), last
		if last != nil {
			t := last.UTC()
			pr.LastRatedAt = &t
		}
		if sys, ok := s.ratings.system(pr.System); ok {
			p := playerState(userID, r, rd, pr.MatchesPlayed, last)
			pr.Provisional = sys.Provisional(p)
			if gr, ok := sys.(interface {
				Current(rating.Player, time.Time) float64
			}); ok && rd != nil {
				cur := gr.Current(p, now)
				pr.RDNow = shownRD(&cur)
			}
		}
		return pr, nil
	})
	if err != nil {
		return PlayerRatings{}, fmt.Errorf("rating states: %w", err)
	}

	rows, err = s.pool.Query(ctx, `
		SELECT match_id, system, discipline, kind, rating_before, rating_after, rd_before, rd_after,
			(details->>'winProbability')::float8, created_at
		FROM rating_changes WHERE user_id = $1
		ORDER BY id DESC LIMIT $2`, userID, ratingHistoryLimit)
	if err != nil {
		return PlayerRatings{}, fmt.Errorf("rating history: %w", err)
	}
	out.History, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (RatingHistoryEntry, error) {
		var (
			h        RatingHistoryEntry
			rb, ra   float64
			rdb, rda *float64
			win      *float64
		)
		err := row.Scan(&h.MatchID, &h.System, &h.Discipline, &h.Kind, &rb, &ra, &rdb, &rda, &win, &h.At)
		h.Before, h.After = shown(rb), shown(ra)
		h.Change = h.After - h.Before
		h.RDBefore, h.RDAfter = shownRD(rdb), shownRD(rda)
		h.WinProbability = roundedProbability(win)
		h.At = h.At.UTC()
		return h, err
	})
	if err != nil {
		return PlayerRatings{}, fmt.Errorf("rating history: %w", err)
	}
	return out, nil
}
