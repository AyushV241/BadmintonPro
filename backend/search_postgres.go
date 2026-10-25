package main

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// likeEscaper escapes LIKE's wildcards and its escape character, so "a_b"
// matches only "a_b" and not "axb". Queries use ESCAPE '\' explicitly.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// searchablePlayer keeps users another player may find: a complete profile
// (a username and a name), and not the searcher ($2).
const searchablePlayer = `u.username IS NOT NULL AND u.name <> '' AND u.id <> $2`

// Matching ($1 is the query, $3 the same escaped for LIKE). A player matches
// on their username or their name, whichever is closer:
//
//   - containsQuery: either one contains the query. The trigram indexes on
//     lower(username) and lower(name) serve it.
//   - matchTier: 1 exact, 2 starts with (the username, the name, or any word
//     of the name: "ver" finds "Ayush Verma"), 3 contains.
//   - closeness: the better of the username's similarity and the name's word
//     similarity, which scores the query against the best-matching stretch
//     of the name rather than the whole of it.
const (
	containsQuery = `(lower(u.username) LIKE '%' || $3 || '%' ESCAPE '\'
		OR lower(u.name) LIKE '%' || $3 || '%' ESCAPE '\')`
	matchTier = `CASE
		WHEN lower(u.username) = $1 OR lower(u.name) = $1 THEN 1
		WHEN lower(u.username) LIKE $3 || '%' ESCAPE '\'
		  OR lower(u.name) LIKE $3 || '%' ESCAPE '\'
		  OR lower(u.name) LIKE '% ' || $3 || '%' ESCAPE '\' THEN 2
		ELSE 3 END`
	closeness = `greatest(similarity(lower(u.username), $1), word_similarity($1, lower(u.name)))`
)

func (s *PostgresStore) SearchUsers(ctx context.Context, q, excludeID string, limit int) ([]PublicUser, error) {
	pattern := likeEscaper.Replace(q)

	// Tiers 1–3: every exact or prefix match also contains the query, so the
	// one "contains" condition finds them all.
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.username, u.name
		FROM users u
		WHERE `+searchablePlayer+` AND `+containsQuery+`
		ORDER BY `+matchTier+`, `+closeness+` DESC, length(u.username), u.username
		LIMIT $4`, q, excludeID, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	users, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PublicUser])
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	if users == nil {
		users = []PublicUser{}
	}
	if len(users) >= fuzzyBelow || utf8.RuneCountInString(q) < fuzzyMinLength {
		return users, nil
	}

	// Tier 4, fuzzy: similar spellings that don't contain the query ("aysh"
	// finds Ayush). The % and <% operators let the trigram indexes find
	// candidates; they compare against pg_trgm's thresholds, set here for
	// this transaction, and closeness applies fuzzyThreshold itself.
	var fuzzy []PublicUser
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, setting := range []string{"pg_trgm.similarity_threshold", "pg_trgm.word_similarity_threshold"} {
			if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, setting, fmt.Sprint(fuzzyThreshold)); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.username, u.name
			FROM users u
			WHERE `+searchablePlayer+`
			  AND (lower(u.username) % $1 OR $1 <% lower(u.name))
			  AND `+closeness+` >= $5
			  AND NOT `+containsQuery+`
			ORDER BY `+closeness+` DESC, length(u.username), u.username
			LIMIT $4`, q, excludeID, pattern, limit-len(users), fuzzyThreshold)
		if err != nil {
			return err
		}
		fuzzy, err = pgx.CollectRows(rows, pgx.RowToStructByPos[PublicUser])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("fuzzy search users: %w", err)
	}
	return append(users, fuzzy...), nil
}

// SuggestPlayers returns players to challenge before anything is typed:
// those whose singles rating is closest to the searcher's, then the most
// recently joined. Complete profiles only, never the searcher.
func (s *PostgresStore) SuggestPlayers(ctx context.Context, userID string, limit int) ([]Player, error) {
	rows, err := s.pool.Query(ctx, `
		WITH me AS (
			SELECT COALESCE((SELECT rating FROM player_ratings WHERE user_id = $1 AND discipline = 'singles'), $2) AS rating
		)
		SELECT `+playerColumns+`
		FROM users u
		LEFT JOIN player_ratings r ON r.user_id = u.id AND r.discipline = 'singles'
		CROSS JOIN me
		WHERE u.username IS NOT NULL AND u.name <> '' AND u.id <> $1
		ORDER BY abs(COALESCE(r.rating, $2) - me.rating), u.created_at DESC, u.id
		LIMIT $3`, userID, startingRating, limit)
	if err != nil {
		return nil, fmt.Errorf("suggest players: %w", err)
	}
	players, err := collectPlayers(rows)
	if err != nil {
		return nil, fmt.Errorf("suggest players: %w", err)
	}
	return players, nil
}
