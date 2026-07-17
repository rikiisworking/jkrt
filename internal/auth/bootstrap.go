package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// Bootstrap ensures user id=1 exists when auth is enabled.
// If no user row and password is set, creates the row with a bcrypt hash.
// If auth is on and neither user nor password is available, returns an error.
func Bootstrap(store *Store, authEnabled bool, password string) error {
	if !authEnabled {
		return nil
	}
	if store == nil {
		return fmt.Errorf("auth store is required when auth is on")
	}

	has, err := store.HasUser()
	if err != nil {
		return fmt.Errorf("check user: %w", err)
	}
	if has {
		return nil
	}
	if password == "" {
		return fmt.Errorf("JKRT_AUTH=on but no user exists and JKRT_PASSWORD is not set")
	}

	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := store.CreateUser1(hash); err != nil {
		return err
	}
	return nil
}

// SetPassword bcrypt-hashes password and updates user id=1.
// Use for password rotation without wiping learning data.
// Existing signed cookies stay valid until they expire or the Learner logs out;
// change JKRT_SESSION_SECRET and restart to invalidate all sessions.
func SetPassword(store *Store, password string) error {
	if store == nil {
		return fmt.Errorf("auth store is required")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return store.UpdatePasswordHash(hash)
}

// EnsureLearnerRow ensures users.id=1 exists for FK targets (cards, reviews).
// Used when JKRT_AUTH=off so extract/scrape can create Cards without a login bootstrap.
// The password hash is random and unknown (not for login).
func EnsureLearnerRow(store *Store) error {
	if store == nil {
		return fmt.Errorf("auth store is required")
	}
	has, err := store.HasUser()
	if err != nil {
		return fmt.Errorf("check user: %w", err)
	}
	if has {
		return nil
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return fmt.Errorf("random learner secret: %w", err)
	}
	hash, err := HashPassword(hex.EncodeToString(secret[:]))
	if err != nil {
		return fmt.Errorf("hash placeholder password: %w", err)
	}
	return store.CreateUser1(hash)
}
