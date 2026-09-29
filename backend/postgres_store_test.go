package main

import (
	"context"
	"os"
	"testing"
)

// TestPostgresStoreContract runs the shared contract against a real database.
// It is skipped unless TEST_DATABASE_URL is set, and it TRUNCATES every table,
// so point it at a throwaway database, never at one holding real data:
//
//	docker exec badmintonpro-db createdb -U badminton badmintonpro_test
//	TEST_DATABASE_URL=postgres://badminton:badminton@localhost:5432/badmintonpro_test?sslmode=disable go test ./...
func TestPostgresStoreContract(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if err := runMigrations(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := NewPostgresStore(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	runStoreContract(t, func(t *testing.T) Store {
		if _, err := store.pool.Exec(context.Background(),
			`TRUNCATE users, sessions, user_identities CASCADE`,
		); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		return store
	})
}
