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

// querier is satisfied by both the pool and a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const selectUser = `SELECT id, name, COALESCE(email, ''), email_verified FROM users`

func loadUser(ctx context.Context, q querier, id string) (User, error) {
	var u User
	err := q.QueryRow(ctx, selectUser+` WHERE id = $1`, id).
		Scan(&u.ID, &u.Name, &u.Email, &u.EmailVerified)
	return u, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

func (s *PostgresStore) CreateUser(ctx context.Context, nu NewUser) (User, error) {
	hash, err := hashPassword(nu.Password)
	if err != nil {
		return User{}, err
	}
	id := nu.ID
	if id == "" {
		if id, err = newUserID(); err != nil {
			return User{}, err
		}
	}
	user := User{ID: id, Name: nu.Name, Email: normaliseEmail(nu.Email), EmailVerified: nu.EmailVerified}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, name, email, email_verified) VALUES ($1, $2, $3, $4)`,
			user.ID, user.Name, user.Email, user.EmailVerified,
		); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO password_credentials (user_id, password_hash) VALUES ($1, $2)`,
			user.ID, hash,
		)
		return err
	})
	if isUniqueViolation(err) {
		return User{}, ErrEmailTaken
	}
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *PostgresStore) VerifyPassword(ctx context.Context, email, password string) (User, error) {
	var (
		user User
		hash []byte // NULL when the account has no password
	)
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.name, COALESCE(u.email, ''), u.email_verified, pc.password_hash
		FROM users u
		LEFT JOIN password_credentials pc ON pc.user_id = u.id
		WHERE lower(u.email) = $1`,
		normaliseEmail(email),
	).Scan(&user.ID, &user.Name, &user.Email, &user.EmailVerified, &hash)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		hash = nil
	case err != nil:
		return User{}, fmt.Errorf("look up user: %w", err)
	}

	if hash == nil {
		equaliseTiming(password)
		return User{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (s *PostgresStore) ResolveExternalLogin(ctx context.Context, ident oauth.Identity) (User, error) {
	if ident.Provider == "" || ident.Subject == "" {
		return User{}, errors.New("identity is missing provider or subject")
	}
	email := normaliseEmail(ident.Email)

	var user User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var linkedID string
		err := tx.QueryRow(ctx,
			`SELECT user_id FROM user_identities WHERE provider = $1 AND subject = $2`,
			ident.Provider, ident.Subject,
		).Scan(&linkedID)
		linked := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("look up identity: %w", err)
		}

		var owner *emailOwner
		if !linked && email != "" {
			// FOR UPDATE stops a concurrent password change or login from
			// racing the takeover below.
			var o emailOwner
			err := tx.QueryRow(ctx,
				`SELECT id, email_verified FROM users WHERE lower(email) = $1 FOR UPDATE`,
				email,
			).Scan(&o.userID, &o.emailVerified)
			switch {
			case err == nil:
				owner = &o
			case !errors.Is(err, pgx.ErrNoRows):
				return fmt.Errorf("look up email owner: %w", err)
			}
		}

		linkIdentity := func(userID string) error {
			_, err := tx.Exec(ctx,
				`INSERT INTO user_identities (provider, subject, user_id, email) VALUES ($1, $2, $3, NULLIF($4, ''))`,
				ident.Provider, ident.Subject, userID, email,
			)
			return err
		}

		switch decideLink(linked, owner, ident.EmailVerified) {
		case linkLogin:
			user, err = loadUser(ctx, tx, linkedID)
			return err

		case linkCreate:
			id, err := newUserID()
			if err != nil {
				return err
			}
			user = User{ID: id, Name: displayName(ident)}
			// Only an email the provider vouches for may claim the address.
			if ident.EmailVerified && email != "" {
				user.Email, user.EmailVerified = email, true
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO users (id, name, email, email_verified) VALUES ($1, $2, NULLIF($3, ''), $4)`,
				user.ID, user.Name, user.Email, user.EmailVerified,
			); err != nil {
				return err
			}
			return linkIdentity(user.ID)

		case linkAttach:
			if err := linkIdentity(owner.userID); err != nil {
				return err
			}
			user, err = loadUser(ctx, tx, owner.userID)
			return err

		case linkTakeover:
			for _, stmt := range []string{
				`DELETE FROM password_credentials WHERE user_id = $1`,
				`DELETE FROM sessions WHERE user_id = $1`,
				`UPDATE users SET email_verified = true WHERE id = $1`,
			} {
				if _, err := tx.Exec(ctx, stmt, owner.userID); err != nil {
					return err
				}
			}
			if err := linkIdentity(owner.userID); err != nil {
				return err
			}
			user, err = loadUser(ctx, tx, owner.userID)
			return err

		default: // linkRefuse
			return ErrAccountConflict
		}
	})
	if err != nil {
		return User{}, err
	}
	return user, nil
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
	var u User
	// Expiry is filtered in SQL so an expired token is indistinguishable from
	// one that never existed.
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.name, COALESCE(u.email, ''), u.email_verified
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = $1 AND s.expires_at > now()`,
		token,
	).Scan(&u.ID, &u.Name, &u.Email, &u.EmailVerified)

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
