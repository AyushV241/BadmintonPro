package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

const (
	// Local Docker credentials, committed on purpose (see README "Database
	// credentials"). Real environments set DATABASE_URL.
	defaultDatabaseURL       = "postgres://badminton:badminton@localhost:5432/badmintonpro?sslmode=disable" //nolint:gosec // G101: local-only dev credentials
	defaultGoogleRedirectURL = "http://localhost:3000/api/auth/google/callback"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Migrations are never applied on startup: the database can be shared, and
	// every developer's server (and every air reload) would otherwise migrate
	// it. Apply them deliberately, once, with `go run . -migrate`.
	migrateOnly := flag.Bool("migrate", false, "apply pending migrations to DATABASE_URL and exit")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := loadDotEnv(); err != nil {
		return err
	}

	databaseURL := envOr("DATABASE_URL", defaultDatabaseURL)

	if *migrateOnly {
		log.Print("applying migrations…")
		if err := runMigrations(databaseURL); err != nil {
			return err
		}
		log.Print("migrations up to date")
		return nil
	}

	store, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := seedDemoUser(ctx, store); err != nil {
		return err
	}

	providers, err := configureProviders()
	if err != nil {
		return err
	}

	go pruneSessions(ctx, store)

	srv := &http.Server{
		Addr:              ":" + envOr("PORT", "8080"),
		Handler:           NewAPI(store, providers, os.Getenv("COOKIE_SECURE") == "true").Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Shut down cleanly on Ctrl-C so in-flight requests finish.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("BadmintonPro API listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Print("shut down")
	return nil
}

// configureProviders enables each external login whose credentials are set.
// A provider that is configured but broken fails startup rather than quietly
// disappearing from the login page.
func configureProviders() (*oauth.Registry, error) {
	registry := oauth.NewRegistry()

	if clientID := os.Getenv("GOOGLE_CLIENT_ID"); clientID != "" {
		google, err := oauth.NewGoogle(
			clientID,
			os.Getenv("GOOGLE_CLIENT_SECRET"),
			envOr("GOOGLE_REDIRECT_URL", defaultGoogleRedirectURL),
		)
		if err != nil {
			return nil, err
		}
		registry.Register(google)
	}

	if names := registry.Names(); len(names) > 0 {
		log.Printf("external login providers: %v", names)
	} else {
		log.Print("no external login providers configured (set GOOGLE_CLIENT_ID to enable Google)")
	}
	return registry, nil
}

// seedDemoUser creates a password login for local development. It is
// idempotent, so restarting against an existing database is not an error.
func seedDemoUser(ctx context.Context, store Store) error {
	email := envOr("DEMO_EMAIL", "player@badmintonpro.local")
	password := envOr("DEMO_PASSWORD", "smash123")

	_, err := store.CreateUser(ctx, NewUser{ID: "usr_1", Name: "Demo Player", Email: email, Password: password})
	switch {
	case errors.Is(err, ErrEmailTaken):
		log.Printf("demo login: %s (already seeded)", email)
		return nil
	case err != nil:
		return err
	}

	log.Printf("demo login: %s / %s", email, password)
	return nil
}

// pruneSessions periodically clears expired sessions so the table does not
// grow without bound.
func pruneSessions(ctx context.Context, store *PostgresStore) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := store.DeleteExpiredSessions(ctx); err != nil {
				log.Printf("prune sessions: %v", err)
			} else if n > 0 {
				log.Printf("pruned %d expired session(s)", n)
			}
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
