// Package db opens SQLite, applies migrations, and persists learning data.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// DB is the application database handle.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) SQLite at dbPath and applies *.sql files from migrationsDir
// in lexicographic order. migrationsDir must contain 001_init.sql (or equivalent).
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
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}
		if _, err := sqlDB.Exec(string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", path, err)
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
