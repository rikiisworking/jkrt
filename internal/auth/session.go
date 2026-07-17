package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// CookieName is the session cookie name (development plan).
	CookieName = "jkrt_session"
	// UserID is the single Learner id for v1.
	UserID = 1
)

// Session holds a validated session payload.
type Session struct {
	UserID    int
	ExpiresAt time.Time
}

// Manager signs and verifies session cookies with HMAC-SHA256.
type Manager struct {
	secret []byte
	ttl    time.Duration
}

// NewManager creates a session manager. secret must be non-empty when auth is on.
func NewManager(secret []byte, ttl time.Duration) *Manager {
	return &Manager{secret: secret, ttl: ttl}
}

// TTL returns the configured session lifetime.
func (m *Manager) TTL() time.Duration {
	return m.ttl
}

// Issue creates a signed cookie value for the given user, valid for TTL from now.
// v1 only allows UserID (the single Learner).
func (m *Manager) Issue(userID int, now time.Time) (string, time.Time, error) {
	if len(m.secret) == 0 {
		return "", time.Time{}, fmt.Errorf("session secret is empty")
	}
	if userID != UserID {
		return "", time.Time{}, fmt.Errorf("invalid user id for v1: %d", userID)
	}
	exp := now.Add(m.ttl)
	payload := fmt.Sprintf("%d|%d", userID, exp.Unix())
	sig := m.sign(payload)
	raw := payload + "|" + sig
	return base64.RawURLEncoding.EncodeToString([]byte(raw)), exp, nil
}

// Parse validates a cookie value and returns the session if valid, not expired,
// and pinned to UserID (v1 single Learner).
func (m *Manager) Parse(value string, now time.Time) (Session, error) {
	if value == "" {
		return Session{}, fmt.Errorf("empty session")
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Session{}, fmt.Errorf("decode session: %w", err)
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return Session{}, fmt.Errorf("malformed session")
	}
	payload := parts[0] + "|" + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(m.sign(payload))) {
		return Session{}, fmt.Errorf("invalid session signature")
	}
	userID, err := strconv.Atoi(parts[0])
	if err != nil {
		return Session{}, fmt.Errorf("invalid user id")
	}
	if userID != UserID {
		return Session{}, fmt.Errorf("invalid user id for v1: %d", userID)
	}
	expUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return Session{}, fmt.Errorf("invalid expiry")
	}
	exp := time.Unix(expUnix, 0).UTC()
	if !now.UTC().Before(exp) {
		return Session{}, fmt.Errorf("session expired")
	}
	return Session{UserID: userID, ExpiresAt: exp}, nil
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
