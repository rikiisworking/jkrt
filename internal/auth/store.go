package auth

import (
	"database/sql"
	"fmt"
	"time"

	appdb "github.com/rikiisworking/jkrt/internal/db"
)

// Store persists the single Learner (user id = 1) password hash.
type Store struct {
	db    *sql.DB
	owned bool // when true, Close() closes the underlying *sql.DB
}

// NewStore wraps an existing database (migrations already applied). Caller owns Close.
func NewStore(sqlDB *sql.DB) *Store {
	return &Store{db: sqlDB, owned: false}
}

// OpenStore opens SQLite, applies Phase 1 migrations (including users), and returns a Store
// that owns the connection (Close closes the DB). Used by tests and simple callers.
func OpenStore(dbPath string) (*Store, error) {
	d, err := appdb.Open(dbPath, "")
	if err != nil {
		return nil, err
	}
	return &Store{db: d.SQL(), owned: true}, nil
}

// Close closes the underlying database when this Store owns it.
func (s *Store) Close() error {
	if s == nil || s.db == nil || !s.owned {
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
