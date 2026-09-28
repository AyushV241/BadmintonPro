package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// unsetForTest removes a variable for the duration of a test, restoring any
// previous value afterwards.
func unsetForTest(t *testing.T, key string) {
	t.Helper()
	if prev, ok := os.LookupEnv(key); ok {
		t.Cleanup(func() { _ = os.Setenv(key, prev) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv(key) })
	}
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDotEnvReadsEveryFile(t *testing.T) {
	dir := t.TempDir()
	backend := writeFile(t, dir, "backend.env", "BP_TEST_DB=backend-db\n")
	root := writeFile(t, dir, "root.env", "BP_TEST_GOOGLE=root-google\n")
	unsetForTest(t, "BP_TEST_DB")
	unsetForTest(t, "BP_TEST_GOOGLE")

	if err := loadDotEnvFiles(backend, root); err != nil {
		t.Fatal(err)
	}

	// Regression: loading stopped at the first file, so a backend/.env hid the
	// repo-root .env and its Google credentials.
	if got := os.Getenv("BP_TEST_DB"); got != "backend-db" {
		t.Errorf("BP_TEST_DB = %q, want value from the first file", got)
	}
	if got := os.Getenv("BP_TEST_GOOGLE"); got != "root-google" {
		t.Errorf("BP_TEST_GOOGLE = %q, want value from the second file", got)
	}
}

func TestLoadDotEnvEarlierFileWins(t *testing.T) {
	dir := t.TempDir()
	backend := writeFile(t, dir, "backend.env", "BP_TEST_URL=from-backend\n")
	root := writeFile(t, dir, "root.env", "BP_TEST_URL=from-root\n")
	unsetForTest(t, "BP_TEST_URL")

	if err := loadDotEnvFiles(backend, root); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("BP_TEST_URL"); got != "from-backend" {
		t.Errorf("BP_TEST_URL = %q, want backend/.env to take precedence", got)
	}
}

func TestLoadDotEnvRealEnvironmentWins(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, dir, "x.env", "BP_TEST_SET=from-file\n")
	t.Setenv("BP_TEST_SET", "from-environment")

	if err := loadDotEnvFiles(file); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("BP_TEST_SET"); got != "from-environment" {
		t.Errorf("BP_TEST_SET = %q, want the existing environment value kept", got)
	}
}

func TestLoadDotEnvSkipsMissingFiles(t *testing.T) {
	if err := loadDotEnvFiles(filepath.Join(t.TempDir(), "absent.env")); err != nil {
		t.Errorf("missing file should be ignored, got %v", err)
	}
}

func TestLoadDotEnvRejectsMalformedFile(t *testing.T) {
	file := writeFile(t, t.TempDir(), "bad.env", "BP_TEST_BAD='unterminated\n")
	if err := loadDotEnvFiles(file); err == nil {
		t.Error("expected an error for a malformed file")
	}
}
