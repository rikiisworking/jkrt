package auth

import (
	"encoding/base64"
	"path/filepath"
	"strings"
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

func TestHashPasswordEmpty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	m := NewManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if m.TTL() != time.Hour {
		t.Fatalf("TTL: got %v", m.TTL())
	}
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
	// Exactly at expiry is expired (Before is strict)
	if _, err := m.Parse(val, exp); err == nil {
		t.Fatal("expected expired at exact expiry")
	}
	// Tampered
	if _, err := m.Parse(val+"x", now); err == nil {
		t.Fatal("expected signature error")
	}
}

func TestIssueEmptySecret(t *testing.T) {
	m := NewManager(nil, time.Hour)
	if _, _, err := m.Issue(UserID, time.Now()); err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestIssueRejectsNonV1User(t *testing.T) {
	m := NewManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if _, _, err := m.Issue(2, time.Now()); err == nil {
		t.Fatal("expected error for user id != 1")
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	m := NewManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	// Valid signature but wrong user (legacy/mistyped token).
	wrongUser := mustIssueWithPayload(t, m, "2|9999999999")

	cases := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"not base64", "!!!not-valid-base64!!!"},
		{"malformed parts", base64.RawURLEncoding.EncodeToString([]byte("only-one-part"))},
		{"bad user id", mustIssueWithPayload(t, m, "x|9999999999")},
		{"non v1 user", wrongUser},
		{"bad expiry", mustIssueWithPayload(t, m, "1|not-a-unix")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := m.Parse(tc.value, now); err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
}

func TestUsersTableDDLHasExpectedColumns(t *testing.T) {
	// Guard against drift before Phase 1 migrations copy this definition.
	for _, needle := range []string{
		"CREATE TABLE IF NOT EXISTS users",
		"id INTEGER PRIMARY KEY",
		"password_hash TEXT NOT NULL",
		"created_at TEXT NOT NULL",
	} {
		if !strings.Contains(UsersTableDDL, needle) {
			t.Fatalf("UsersTableDDL missing %q", needle)
		}
	}
}

// mustIssueWithPayload builds a cookie-shaped value with a valid signature over payload
// (used to reach Atoi/ParseInt error branches after signature check).
func mustIssueWithPayload(t *testing.T, m *Manager, payload string) string {
	t.Helper()
	sig := m.sign(payload)
	raw := payload + "|" + sig
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
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

func TestBootstrapNilStoreWhenAuthOn(t *testing.T) {
	if err := Bootstrap(nil, true, "x"); err == nil {
		t.Fatal("expected error for nil store with auth on")
	}
}

func TestBootstrapSkippedWhenAuthOff(t *testing.T) {
	if err := Bootstrap(nil, false, ""); err != nil {
		t.Fatalf("Bootstrap with auth off: %v", err)
	}
}

func TestEnsureLearnerRow(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := EnsureLearnerRow(store); err != nil {
		t.Fatalf("EnsureLearnerRow: %v", err)
	}
	has, err := store.HasUser()
	if err != nil || !has {
		t.Fatalf("HasUser after ensure: has=%v err=%v", has, err)
	}
	// Idempotent
	if err := EnsureLearnerRow(store); err != nil {
		t.Fatalf("second EnsureLearnerRow: %v", err)
	}
}

func TestEnsureLearnerRowNilStore(t *testing.T) {
	if err := EnsureLearnerRow(nil); err == nil {
		t.Fatal("expected error for nil store")
	}
}

func TestPasswordHashMissingUser(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.PasswordHash(); err == nil {
		t.Fatal("expected error when user row missing")
	}
	has, err := store.HasUser()
	if err != nil {
		t.Fatalf("HasUser: %v", err)
	}
	if has {
		t.Fatal("expected no user")
	}
}
