package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	store := NewStore()

	// Seeded so there is something to log in with before signup exists.
	demoEmail := envOr("DEMO_EMAIL", "player@badmintonpro.local")
	demoPassword := envOr("DEMO_PASSWORD", "smash123")
	if err := store.CreateUser("usr_1", "Demo Player", demoEmail, demoPassword); err != nil {
		log.Fatalf("seed demo user: %v", err)
	}

	addr := ":" + envOr("PORT", "8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           withCORS(envOr("CORS_ORIGIN", "http://localhost:3000"), NewAPI(store).Routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("BadmintonPro API listening on %s", addr)
	log.Printf("demo login: %s / %s", demoEmail, demoPassword)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
