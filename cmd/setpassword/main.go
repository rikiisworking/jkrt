// Command setpassword rotates the Learner password (user id=1) in SQLite.
// Does not start the HTTP server. See docs/auth-and-tunnel.md.
//
//	go run ./cmd/setpassword -db ./jkrt.db
//	# or: JKRT_DB_PATH=./jkrt.db go run ./cmd/setpassword
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/rikiisworking/jkrt/internal/auth"
	"github.com/rikiisworking/jkrt/internal/db"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "setpassword: %v\n", err)
		os.Exit(1)
	}
}

// run is the CLI entry (args without program name).
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("setpassword", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", envOr("JKRT_DB_PATH", "./jkrt.db"), "SQLite database path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	migrationsDir, err := findMigrations()
	if err != nil {
		return err
	}

	database, err := db.Open(*dbPath, migrationsDir)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = database.Close() }()

	store := auth.NewStore(database.SQL())
	has, err := store.HasUser()
	if err != nil {
		return err
	}
	if !has {
		return fmt.Errorf("no user row yet; start the server once with JKRT_AUTH=on and JKRT_PASSWORD to bootstrap")
	}

	pass, err := readPassword(fs.Args(), stdin, stderr)
	if err != nil {
		return err
	}
	if err := auth.SetPassword(store, pass); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "password updated for user id=1")
	fmt.Fprintln(stdout, "existing sessions remain valid until TTL expiry or logout")
	fmt.Fprintln(stdout, "to invalidate all sessions: change JKRT_SESSION_SECRET and restart the server")
	return nil
}

func readPassword(posArgs []string, stdin io.Reader, stderr io.Writer) (string, error) {
	// Non-interactive: first positional arg (prefer interactive for real use).
	if len(posArgs) >= 1 {
		p := posArgs[0]
		if p == "" {
			return "", fmt.Errorf("password must not be empty")
		}
		return p, nil
	}

	// Interactive terminal: confirm twice (not used in automated tests).
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(stderr, "New password: ")
		b1, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", err
		}
		fmt.Fprint(stderr, "Confirm password: ")
		b2, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", err
		}
		if string(b1) != string(b2) {
			return "", fmt.Errorf("passwords do not match")
		}
		if len(b1) == 0 {
			return "", fmt.Errorf("password must not be empty")
		}
		return string(b1), nil
	}

	// Piped password (one line; trailing newline optional).
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read password: %w", err)
	}
	p := strings.TrimSpace(line)
	if p == "" {
		return "", fmt.Errorf("password must not be empty")
	}
	return p, nil
}

func findMigrations() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(wd, "migrations")
	if _, err := os.Stat(filepath.Join(candidate, "001_init.sql")); err == nil {
		return candidate, nil
	}
	return db.FindMigrationsDir()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
