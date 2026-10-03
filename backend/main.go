package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
	"github.com/AyushV241/BadmintonPro/backend/internal/otp"
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

	providers, err := configureProviders()
	if err != nil {
		return err
	}
	cookieSecure := os.Getenv("COOKIE_SECURE") == "true"
	phone, err := configurePhoneLogin(cookieSecure)
	if err != nil {
		return err
	}

	go pruneSessions(ctx, store)

	srv := &http.Server{
		Addr:              ":" + envOr("PORT", "8080"),
		Handler:           NewAPI(store, providers, phone, cookieSecure).Routes(),
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

	// Phone identities are stored under this provider name; an OAuth provider
	// using it could mint them.
	if _, clash := registry.Get(phoneIdentityProvider); clash {
		return nil, fmt.Errorf("no OAuth provider may be named %q", phoneIdentityProvider)
	}

	if names := registry.Names(); len(names) > 0 {
		log.Printf("external login providers: %v", names)
	} else {
		log.Print("no external login providers configured (set GOOGLE_CLIENT_ID to enable Google)")
	}
	return registry, nil
}

// configurePhoneLogin enables phone login when OTP_PROVIDER is set, and
// returns nil (disabled) when it isn't.
func configurePhoneLogin(cookieSecure bool) (*PhoneLogin, error) {
	provider, err := newOTPProvider(os.Getenv("OTP_PROVIDER"), cookieSecure)
	if err != nil || provider == nil {
		return nil, err
	}

	regions := strings.Split(envOr("PHONE_REGIONS", "IN"), ",")
	policy, err := newPhonePolicy(envOr("PHONE_DEFAULT_REGION", strings.TrimSpace(regions[0])), regions)
	if err != nil {
		return nil, err
	}

	log.Printf("phone login: enabled via %s for %v", provider.Name(), regions)
	return NewPhoneLogin(provider, policy), nil
}

// newOTPProvider is the one place that knows which vendor sends codes.
// Switching vendor means adding an adapter in internal/otp and a case here.
func newOTPProvider(name string, cookieSecure bool) (otp.Provider, error) {
	switch name {
	case "":
		log.Print("phone login disabled (set OTP_PROVIDER to console or twilio)")
		return nil, nil
	case "twilio":
		return otp.NewTwilio(
			os.Getenv("TWILIO_ACCOUNT_SID"),
			os.Getenv("TWILIO_AUTH_TOKEN"),
			os.Getenv("TWILIO_VERIFY_SERVICE_SID"),
		)
	case "console":
		// Prints codes to the log instead of sending them. Anywhere served
		// over HTTPS is a real deployment, where that would be a mistake.
		if cookieSecure {
			return nil, errors.New("OTP_PROVIDER=console is for local development and can't be used with COOKIE_SECURE=true")
		}
		return otp.NewMemory("console", func(phone, code string) {
			log.Printf("phone login code for %s: %s (console provider: not sent)", phone, code)
		}), nil
	default:
		return nil, fmt.Errorf("unknown OTP_PROVIDER %q (want console or twilio)", name)
	}
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
