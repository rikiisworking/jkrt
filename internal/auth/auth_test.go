package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !CheckPassword(hash, "s3cret") {
		t.Fatal("expected password to match")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("expected wrong password to fail")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	m := NewManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	val, exp, err := m.Issue(UserID, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if exp.Sub(now) != time.Hour {
		t.Fatalf("expiry delta: %v", exp.Sub(now))
	}
	sess, err := m.Parse(val, now.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if sess.UserID != UserID {
		t.Fatalf("user id: %d", sess.UserID)
	}
	// Expired
	if _, err := m.Parse(val, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expected expiry error")
	}
	// Tampered
	if _, err := m.Parse(val+"x", now); err == nil {
		t.Fatal("expected signature error")
	}
}

func TestBootstrapCreatesUser(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := Bootstrap(store, true, "bootstrap-pass"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	has, err := store.HasUser()
	if err != nil || !has {
		t.Fatalf("HasUser: has=%v err=%v", has, err)
	}
	hash, err := store.PasswordHash()
	if err != nil {
		t.Fatalf("PasswordHash: %v", err)
	}
	if !CheckPassword(hash, "bootstrap-pass") {
		t.Fatal("stored hash does not match password")
	}
	// Second bootstrap is a no-op even without password.
	if err := Bootstrap(store, true, ""); err != nil {
		t.Fatalf("second Bootstrap: %v", err)
	}
}

func TestBootstrapFailsWithoutPassword(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	err = Bootstrap(store, true, "")
	if err == nil {
		t.Fatal("expected error when auth on and no password/user")
	}
}

func TestBootstrapSkippedWhenAuthOff(t *testing.T) {
	if err := Bootstrap(nil, false, ""); err != nil {
		t.Fatalf("Bootstrap with auth off: %v", err)
	}
}
