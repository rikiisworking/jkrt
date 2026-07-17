package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rikiisworking/jkrt/internal/auth"
	"github.com/rikiisworking/jkrt/internal/db"
)

func TestRunRotatesPassword(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "jkrt.db")
	migrations := filepath.Join("..", "..", "migrations")

	database, err := db.Open(dbPath, migrations)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := auth.NewStore(database.SQL())
	if err := auth.Bootstrap(store, true, "old-secret"); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()

	var stdout, stderr bytes.Buffer
	err = run(
		[]string{"-db", dbPath, "new-secret"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run: %v (stderr=%s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "password updated") {
		t.Fatalf("stdout: %s", stdout.String())
	}

	database, err = db.Open(dbPath, migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store = auth.NewStore(database.SQL())
	hash, err := store.PasswordHash()
	if err != nil {
		t.Fatal(err)
	}
	if auth.CheckPassword(hash, "old-secret") {
		t.Fatal("old password still works")
	}
	if !auth.CheckPassword(hash, "new-secret") {
		t.Fatal("new password does not work")
	}
}

func TestRunFromStdin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "jkrt.db")
	migrations := filepath.Join("..", "..", "migrations")
	database, err := db.Open(dbPath, migrations)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Bootstrap(auth.NewStore(database.SQL()), true, "old"); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()

	var stdout, stderr bytes.Buffer
	err = run(
		[]string{"-db", dbPath},
		strings.NewReader("piped-pass\n"),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	database, err = db.Open(dbPath, migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	hash, err := auth.NewStore(database.SQL()).PasswordHash()
	if err != nil {
		t.Fatal(err)
	}
	if !auth.CheckPassword(hash, "piped-pass") {
		t.Fatal("piped password not applied")
	}
}

func TestRunNoUser(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	migrations := filepath.Join("..", "..", "migrations")
	// Open applies migrations but no user row.
	database, err := db.Open(dbPath, migrations)
	if err != nil {
		t.Fatal(err)
	}
	_ = database.Close()

	var stderr bytes.Buffer
	err = run([]string{"-db", dbPath, "x"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	if err == nil {
		t.Fatal("expected error when no user")
	}
	if !strings.Contains(err.Error(), "no user row") {
		t.Fatalf("err: %v", err)
	}
}

func TestRunEmptyPasswordArg(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "jkrt.db")
	migrations := filepath.Join("..", "..", "migrations")
	database, err := db.Open(dbPath, migrations)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Bootstrap(auth.NewStore(database.SQL()), true, "old"); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()

	err = run([]string{"-db", dbPath, ""}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected empty password error")
	}
}

func TestReadPasswordEmptyStdin(t *testing.T) {
	_, err := readPassword(nil, strings.NewReader(""), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("JKRT_SETPASSWORD_TEST_X", "from-env")
	if got := envOr("JKRT_SETPASSWORD_TEST_X", "def"); got != "from-env" {
		t.Fatalf("got %q", got)
	}
	if got := envOr("JKRT_SETPASSWORD_TEST_MISSING", "def"); got != "def" {
		t.Fatalf("got %q", got)
	}
}