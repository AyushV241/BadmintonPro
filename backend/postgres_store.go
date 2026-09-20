package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// uniqueViolation is the SQLSTATE code Postgres returns for a duplicate key.
const uniqueViolation = "23505"

// PostgresStore is the durable implementation of Store.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() { s.pool.Close() }

func (s *PostgresStore) CreateUser(ctx context.Context, id, name, email, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO users (id, name, email, password_hash) VALUES ($1, $2, $3, $4)`,
		id, name, normaliseEmail(email), hash,
	)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return ErrEmailTaken
	}
	return err
}

func (s *PostgresStore) Authenticate(ctx context.Context, email, password string) (User, string, error) {
	var (
		user User
		hash []byte
	)

	err := s.pool.QueryRow(ctx,
		`SELECT id, name, email, password_hash FROM users WHERE lower(email) = $1`,
		normaliseEmail(email),
	).Scan(&user.ID, &user.Name, &user.Email, &hash)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		equaliseTiming(password)
		return User{}, "", ErrInvalidCredentials
	case err != nil:
		return User{}, "", fmt.Errorf("look up user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil {
		return User{}, "", ErrInvalidCredentials
	}

	token, err := newToken()
	if err != nil {
		return User{}, "", err
	}

	if _, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)`,
		token, user.ID, time.Now().Add(sessionTTL),
	); err != nil {
		return User{}, "", fmt.Errorf("create session: %w", err)
	}

	return user, token, nil
}

func (s *PostgresStore) UserForToken(ctx context.Context, token string) (User, error) {
	var user User

	// Expiry is filtered in SQL so an expired token is indistinguishable from
	// one that never existed.
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.name, u.email
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = $1 AND s.expires_at > now()`,
		token,
	).Scan(&user.ID, &user.Name, &user.Email)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return User{}, ErrInvalidToken
	case err != nil:
		return User{}, fmt.Errorf("look up session: %w", err)
	}

	return user, nil
}

func (s *PostgresStore) Revoke(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

// DeleteExpiredSessions clears out sessions past their expiry. Expired tokens
// are already rejected on read; this just stops the table growing forever.
func (s *PostgresStore) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
