package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds process configuration from environment variables.
type Config struct {
	Addr          string
	DBPath        string
	AuthEnabled   bool
	Password      string // bootstrap only; empty after startup is fine if user exists
	SessionSecret []byte
	SessionTTL    time.Duration
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	cfg := Config{
		Addr:        envOr("JKRT_ADDR", ":8080"),
		DBPath:      envOr("JKRT_DB_PATH", "./jkrt.db"),
		Password:    os.Getenv("JKRT_PASSWORD"),
		SessionTTL:  168 * time.Hour,
		AuthEnabled: true,
	}

	authRaw := strings.ToLower(strings.TrimSpace(envOr("JKRT_AUTH", "on")))
	switch authRaw {
	case "on", "1", "true", "yes":
		cfg.AuthEnabled = true
	case "off", "0", "false", "no":
		cfg.AuthEnabled = false
	default:
		return Config{}, fmt.Errorf("JKRT_AUTH must be on or off, got %q", authRaw)
	}

	if ttlRaw := os.Getenv("JKRT_SESSION_TTL"); ttlRaw != "" {
		ttl, err := time.ParseDuration(ttlRaw)
		if err != nil {
			return Config{}, fmt.Errorf("JKRT_SESSION_TTL: %w", err)
		}
		if ttl <= 0 {
			return Config{}, fmt.Errorf("JKRT_SESSION_TTL must be positive")
		}
		cfg.SessionTTL = ttl
	}

	if cfg.AuthEnabled {
		secret := os.Getenv("JKRT_SESSION_SECRET")
		if len(secret) < 32 {
			return Config{}, fmt.Errorf("JKRT_SESSION_SECRET is required when auth is on (at least 32 bytes)")
		}
		cfg.SessionSecret = []byte(secret)
	}

	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
