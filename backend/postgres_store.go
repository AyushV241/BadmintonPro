package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is the SQLSTATE code Postgres returns for a duplicate key.
const uniqueViolation = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

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

// querier is satisfied by both the pool and a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// userColumns is the users row as a User, for any query that aliases users
// as u. scanUser reads them in this order.
const userColumns = `u.id, u.name, COALESCE(u.username, ''), COALESCE(u.email, ''), u.email_verified,
	COALESCE(u.phone, ''), u.phone_verified, COALESCE(u.sign_in_method, '')`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Username, &u.Email, &u.EmailVerified, &u.Phone, &u.PhoneVerified, &u.SignInMethod)
	return withProfileStatus(u), err
}

func loadUser(ctx context.Context, q querier, id string) (User, error) {
	return scanUser(q.QueryRow(ctx, `SELECT `+userColumns+` FROM users u WHERE u.id = $1`, id))
}

func (s *PostgresStore) ResolveExternalLogin(ctx context.Context, ident oauth.Identity) (User, error) {
	if ident.Provider == "" || ident.Subject == "" {
		return User{}, errors.New("identity is missing provider or subject")
	}

	var user User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var linkedID string
		err := tx.QueryRow(ctx,
			`SELECT user_id FROM user_identities WHERE provider = $1 AND subject = $2`,
			ident.Provider, ident.Subject,
		).Scan(&linkedID)
		switch {
		case err == nil:
			user, err = loadUser(ctx, tx, linkedID)
			return err
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("look up identity: %w", err)
		}

		id, err := newUserID()
		if err != nil {
			return err
		}
		user = newAccount(id, ident)
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (id, name, email, email_verified, phone, phone_verified, sign_in_method)
			VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), $6, $7)`,
			user.ID, user.Name, user.Email, user.EmailVerified, user.Phone, user.PhoneVerified, user.SignInMethod,
		); err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO user_identities (provider, subject, user_id, email) VALUES ($1, $2, $3, NULLIF($4, ''))`,
			ident.Provider, ident.Subject, user.ID, user.Email,
		)
		return err
	})
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *PostgresStore) UpdateProfile(ctx context.Context, userID string, p ProfileUpdate) (User, error) {
	var row pgx.Row
	if p.Email == nil {
		row = s.pool.QueryRow(ctx, `
			UPDATE users AS u SET name = $2, username = $3
			WHERE u.id = $1
			RETURNING `+userColumns,
			userID, p.Name, p.Username)
	} else {
		// A typed email is never verified, whatever the account had before.
		row = s.pool.QueryRow(ctx, `
			UPDATE users AS u SET name = $2, username = $3, email = NULLIF($4, ''), email_verified = false
			WHERE u.id = $1
			RETURNING `+userColumns,
			userID, p.Name, p.Username, *p.Email)
	}
	u, err := scanUser(row)
	switch {
	case isUniqueViolation(err):
		return User{}, ErrUsernameTaken
	case errors.Is(err, pgx.ErrNoRows):
		return User{}, ErrNoSuchUser
	case err != nil:
		return User{}, fmt.Errorf("update profile: %w", err)
	}
	return u, nil
}

func (s *PostgresStore) SetVerifiedPhone(ctx context.Context, userID, phone string) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `
		UPDATE users AS u SET phone = $2, phone_verified = true
		WHERE u.id = $1
		RETURNING `+userColumns,
		userID, phone))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return User{}, ErrNoSuchUser
	case err != nil:
		return User{}, fmt.Errorf("set phone: %w", err)
	}
	return u, nil
}

func (s *PostgresStore) CreateSession(ctx context.Context, userID string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)`,
		token, userID, time.Now().Add(sessionTTL),
	); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

func (s *PostgresStore) UserForToken(ctx context.Context, token string) (User, error) {
	// Expiry is filtered in SQL so an expired token is indistinguishable from
	// one that never existed.
	u, err := scanUser(s.pool.QueryRow(ctx, `
		SELECT `+userColumns+`
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = $1 AND s.expires_at > now()`,
		token,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return User{}, ErrInvalidToken
	case err != nil:
		return User{}, fmt.Errorf("look up session: %w", err)
	}
	return u, nil
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
