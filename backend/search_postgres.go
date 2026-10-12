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

func (s *PostgresStore) SearchUsers(ctx context.Context, q, excludeID string, limit int) ([]PublicUser, error) {
	pattern := likeEscaper.Replace(q)

	// Tiers 1–3. Every exact or prefix match also contains q, so the one
	// "contains" condition finds them all, and the trigram index serves it.
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.username, u.name
		FROM users u
		WHERE `+searchablePlayer+`
		  AND lower(u.username) LIKE '%' || $3 || '%' ESCAPE '\'
		ORDER BY
			CASE WHEN lower(u.username) = $1 THEN 1
			     WHEN lower(u.username) LIKE $3 || '%' ESCAPE '\' THEN 2
			     ELSE 3 END,
			similarity(lower(u.username), $1) DESC, length(u.username), u.username
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

	// Tier 4, fuzzy, after the others: similar spellings that don't contain
	// q. The % operator lets the trigram index find candidates, using
	// pg_trgm.similarity_threshold (0.3 unless changed); the similarity()
	// check then applies fuzzyThreshold itself.
	rows, err = s.pool.Query(ctx, `
		SELECT u.id, u.username, u.name
		FROM users u
		WHERE `+searchablePlayer+`
		  AND lower(u.username) % $1
		  AND similarity(lower(u.username), $1) >= $5
		  AND lower(u.username) NOT LIKE '%' || $3 || '%' ESCAPE '\'
		ORDER BY similarity(lower(u.username), $1) DESC, length(u.username), u.username
		LIMIT $4`, q, excludeID, pattern, limit-len(users), fuzzyThreshold)
	if err != nil {
		return nil, fmt.Errorf("fuzzy search users: %w", err)
	}
	fuzzy, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PublicUser])
	if err != nil {
		return nil, fmt.Errorf("fuzzy search users: %w", err)
	}
	return append(users, fuzzy...), nil
}
