// Package db opens SQLite, applies migrations, and persists learning data.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rikiisworking/jkrt/internal/schedule"

	_ "modernc.org/sqlite"
)

// DB is the application database handle.
type DB struct {
	sql *sql.DB
	// cardParams seeds new Cards via schedule.NewCard. Nil → DefaultParams().
	// Set with SetScheduleParams so extract and Review share one config.
	cardParams *schedule.Params
}

// Open opens (or creates) SQLite at dbPath and applies *.sql files from migrationsDir
// in lexicographic order. Already-applied files (schema_migrations) are skipped.
func Open(dbPath, migrationsDir string) (*DB, error) {
	if migrationsDir == "" {
		dir, err := FindMigrationsDir()
		if err != nil {
			return nil, err
		}
		migrationsDir = dir
	}

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single-user app: one connection avoids lock surprises in tests.
	sqlDB.SetMaxOpenConns(1)

	if _, err := sqlDB.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("pragma foreign_keys: %w", err)
	}

	if err := applyMigrations(sqlDB, migrationsDir); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return &DB{sql: sqlDB}, nil
}

// SetScheduleParams sets the SM-2 params used when extract creates new Cards.
// Call once at startup with the same Params passed to review.New.
func (d *DB) SetScheduleParams(p schedule.Params) {
	if d == nil {
		return
	}
	cp := p
	d.cardParams = &cp
}

// scheduleParams returns configured params or schedule.DefaultParams().
func (d *DB) scheduleParams() schedule.Params {
	if d != nil && d.cardParams != nil {
		return *d.cardParams
	}
	return schedule.DefaultParams()
}

// SQL returns the underlying *sql.DB.
func (d *DB) SQL() *sql.DB {
	if d == nil {
		return nil
	}
	return d.sql
}

// Close closes the database.
func (d *DB) Close() error {
	if d == nil || d.sql == nil {
		return nil
	}
	return d.sql.Close()
}

func applyMigrations(sqlDB *sql.DB, dir string) error {
	if _, err := sqlDB.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
	name TEXT PRIMARY KEY,
	applied_at TEXT NOT NULL
);`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %q: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".sql") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("no .sql migrations in %q", dir)
	}
	sort.Strings(files)

	for _, path := range files {
		name := filepath.Base(path)
		var n int
		if err := sqlDB.QueryRow(
			`SELECT COUNT(1) FROM schema_migrations WHERE name = ?`, name,
		).Scan(&n); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if n > 0 {
			continue
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}
		if _, err := sqlDB.Exec(string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", path, err)
		}
		if _, err := sqlDB.Exec(
			`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
			name, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}

// FindMigrationsDir walks up from the working directory looking for
// migrations/001_init.sql next to go.mod (or a migrations folder with sql files).
func FindMigrationsDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		candidate := filepath.Join(dir, "migrations")
		initSQL := filepath.Join(candidate, "001_init.sql")
		if st, err := os.Stat(initSQL); err == nil && !st.IsDir() {
			return candidate, nil
		}
		// Also accept any migrations dir that has at least one .sql
		if entries, err := os.ReadDir(candidate); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
					return candidate, nil
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("migrations directory not found from %s (expected migrations/001_init.sql)", wd)
}
