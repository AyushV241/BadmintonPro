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
	"github.com/AyushV241/BadmintonPro/backend/internal/payments"
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
	// Demo data is written only when asked for, never on startup, for the same
	// reason; and only to a local database unless -allow-remote says otherwise,
	// because the repo-root .env may point DATABASE_URL at a shared one.
	seedOnly := flag.Bool("seed-demo", false, "write demo venues, events, players and entries to DATABASE_URL and exit")
	allowRemote := flag.Bool("allow-remote", false, "let -seed-demo write to a database that isn't on this machine")
	// Stands in for Razorpay's webhook while it can't reach this machine.
	mockWebhook := flag.String("mock-webhook", "", "send a signed payment.captured webhook for this Razorpay test order (order_…) to -webhook-url and exit")
	webhookURL := flag.String("webhook-url", "http://localhost:8080/api/payments/webhook", "where -mock-webhook sends it")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := loadDotEnv(); err != nil {
		return err
	}

	if *mockWebhook != "" {
		return sendMockWebhook(ctx, *mockWebhook, *webhookURL)
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

	if *seedOnly {
		return runSeedDemo(ctx, databaseURL, *allowRemote)
	}

	store, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	// Which rating players see; every system rates every match regardless.
	if err := store.UseRatingSystem(os.Getenv("RATING_SYSTEM")); err != nil {
		return err
	}
	log.Printf("ratings: %s shown to players", store.ratings.primary.Name())

	providers, err := configureProviders()
	if err != nil {
		return err
	}
	cookieSecure := os.Getenv("COOKIE_SECURE") == "true"
	phone, err := configurePhoneLogin(cookieSecure)
	if err != nil {
		return err
	}

	paymentProvider, err := newPaymentProvider(os.Getenv("PAYMENT_PROVIDER"), cookieSecure)
	if err != nil {
		return err
	}

	go pruneSessions(ctx, store)

	api := NewAPI(store, providers, phone, cookieSecure).
		WithEvents(store).WithAdmin(store).WithEntries(store).WithMatches(store).WithUserSearch(store).
		WithPayments(store, paymentProvider).WithChallenges(store).WithJudges(store).WithScoring(store)

	// Expires challenges past their deadline and refunds whoever paid,
	// until shutdown. Reads and actions expire them lazily too.
	sweeperDone := make(chan struct{})
	go func() {
		defer close(sweeperDone)
		api.sweepChallenges(ctx)
	}()
	// Live score changes made through any server reach this one's viewers.
	// Deferred after store.Close, so it runs first: the listener must hand
	// its connection back before the pool can close.
	defer startLiveListener(ctx, store.pool, api.live)()

	srv := &http.Server{
		Addr:              ":" + envOr("PORT", "8080"),
		Handler:           api.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	// Live-score streams never finish on their own; end them on shutdown.
	srv.RegisterOnShutdown(api.live.shutdown)

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
	// ctx is done once the server has shut down, so the sweeper is stopping;
	// let a sweep in progress finish before the store closes.
	<-sweeperDone
	log.Print("shut down")
	return nil
}

func runSeedDemo(ctx context.Context, databaseURL string, allowRemote bool) error {
	local, err := isLocalDatabase(databaseURL)
	if err != nil {
		return err
	}
	if !local && !allowRemote {
		return errors.New("DATABASE_URL isn't on this machine; refusing to write demo data (add -allow-remote if you mean it)")
	}

	store, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := seedDemo(ctx, store.pool, time.Now()); err != nil {
		return fmt.Errorf("seed demo data: %w", err)
	}
	log.Printf("demo data written: %d venues, %d events, %d players with %d entries in %s",
		len(demoVenues), len(demoEvents), len(demoPlayers), len(demoEntries), demoEntryEvent)
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

// newPaymentProvider is the one place that knows which vendor takes
// payments, like newOTPProvider. nil means payments are disabled. It logs
// the choice, never a secret.
func newPaymentProvider(name string, cookieSecure bool) (payments.Provider, error) {
	switch name {
	case "":
		log.Print("payments disabled (set PAYMENT_PROVIDER to fake or razorpay)")
		return nil, nil
	case "razorpay":
		keyID := os.Getenv("RAZORPAY_KEY_ID")
		p, err := payments.NewRazorpay(keyID, os.Getenv("RAZORPAY_KEY_SECRET"), os.Getenv("RAZORPAY_WEBHOOK_SECRET"))
		if err != nil {
			return nil, err
		}
		mode := "live"
		if strings.HasPrefix(keyID, "rzp_test_") {
			mode = "test"
		}
		webhook := "webhook enabled"
		if os.Getenv("RAZORPAY_WEBHOOK_SECRET") == "" {
			webhook = "webhook disabled (set RAZORPAY_WEBHOOK_SECRET)"
		}
		log.Printf("payments: razorpay, %s mode, %s", mode, webhook)
		return p, nil
	case "fake":
		// Accepts any payment the frontend's fake checkout makes up. Anywhere
		// served over HTTPS is a real deployment, where that would be free
		// entry for everyone.
		if cookieSecure {
			return nil, errors.New("PAYMENT_PROVIDER=fake is for local development and can't be used with COOKIE_SECURE=true")
		}
		log.Print("payments: fake provider (local development: no money moves)")
		return payments.NewFake(), nil
	default:
		return nil, fmt.Errorf("unknown PAYMENT_PROVIDER %q (want fake or razorpay)", name)
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
