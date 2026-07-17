package auth

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store persists the single Learner (user id = 1) password hash.
type Store struct {
	db *sql.DB
}

// OpenStore opens (or creates) the SQLite database and ensures the users table exists.
// Phase 0 only needs users; full schema arrives in Phase 1 migrations.
func OpenStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// One connection is enough for the single-user app and avoids lock surprises in tests.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create users table: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// HasUser reports whether user id=1 exists.
func (s *Store) HasUser() (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE id = 1`).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateUser1 inserts the bootstrap Learner row.
func (s *Store) CreateUser1(passwordHash string) error {
	_, err := s.db.Exec(
		`INSERT INTO users (id, password_hash, created_at) VALUES (1, ?, ?)`,
		passwordHash,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert user 1: %w", err)
	}
	return nil
}

// PasswordHash returns the bcrypt hash for user id=1.
func (s *Store) PasswordHash() (string, error) {
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = 1`).Scan(&hash)
	if err != nil {
		return "", err
	}
	return hash, nil
}
