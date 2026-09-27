package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"

	"github.com/joho/godotenv"
)

// dotEnvPaths are read in order. godotenv never overrides a variable that is
// already set, so earlier files win over later ones, and the real environment
// wins over both:
//
//   - backend/.env: backend-only settings, e.g. a hosted DATABASE_URL
//   - ../.env (repo root): shared with docker-compose, e.g. GOOGLE_* credentials
var dotEnvPaths = []string{".env", "../.env"}

// loadDotEnv loads every .env file that exists. It is called explicitly from
// run() rather than from init(), so `go test` never picks up real credentials.
// A missing file is fine; a malformed one is an error rather than silently
// skipped.
func loadDotEnv() error {
	return loadDotEnvFiles(dotEnvPaths...)
}

func loadDotEnvFiles(paths ...string) error {
	for _, path := range paths {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := godotenv.Load(path); err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		log.Printf("loaded environment from %s", path)
	}
	return nil
}
