package auth

import (
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
