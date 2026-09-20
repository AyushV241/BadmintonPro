package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const defaultDatabaseURL = "postgres://badminton:badminton@localhost:5432/badmintonpro?sslmode=disable"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL := envOr("DATABASE_URL", defaultDatabaseURL)

	log.Print("applying migrations…")
	if err := runMigrations(databaseURL); err != nil {
		return err
	}

	store, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := seedDemoUser(ctx, store); err != nil {
		return err
	}

	go pruneSessions(ctx, store)

	srv := &http.Server{
		Addr:              ":" + envOr("PORT", "8080"),
		Handler:           withCORS(envOr("CORS_ORIGIN", "http://localhost:3000"), NewAPI(store).Routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Shut down cleanly on Ctrl-C so in-flight requests finish.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("BadmintonPro API listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Print("shut down")
	return nil
}

// seedDemoUser creates a login for local development. It is idempotent, so
// restarting against an existing database is not an error.
func seedDemoUser(ctx context.Context, store Store) error {
	email := envOr("DEMO_EMAIL", "player@badmintonpro.local")
	password := envOr("DEMO_PASSWORD", "smash123")

	err := store.CreateUser(ctx, "usr_1", "Demo Player", email, password)
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
