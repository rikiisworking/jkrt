package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// DefaultNHKMainRSSURL is the verified NHK main cat0 feed (DEVELOPMENT_PLAN.md).
const DefaultNHKMainRSSURL = "https://news.web.nhk/n-data/conf/na/rss/cat0.xml"

// Config holds process configuration from environment variables.
type Config struct {
	Addr          string
	DBPath        string
	AuthEnabled   bool
	Password      string // bootstrap only; empty after startup is fine if user exists
	SessionSecret []byte
	SessionTTL    time.Duration
	NHKMainRSSURL string // JKRT_NHK_MAIN_RSS_URL

	// JLPT unknown-word classifier (Grok Build headless).
	// JLPTClassify: "on" | "off" | "auto" (default auto = on when grok on PATH).
	JLPTClassify          string
	JLPTClassifyModel     string
	JLPTClassifyTimeout   time.Duration
	JLPTClassifyMaxPerExt int
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	cfg := Config{
		Addr:                  envOr("JKRT_ADDR", ":8080"),
		DBPath:                envOr("JKRT_DB_PATH", "./jkrt.db"),
		Password:              os.Getenv("JKRT_PASSWORD"),
		SessionTTL:            168 * time.Hour,
		AuthEnabled:           true,
		NHKMainRSSURL:         envOr("JKRT_NHK_MAIN_RSS_URL", DefaultNHKMainRSSURL),
		JLPTClassify:          strings.ToLower(strings.TrimSpace(envOr("JKRT_JLPT_CLASSIFY", "auto"))),
		JLPTClassifyModel:     envOr("JKRT_JLPT_CLASSIFY_MODEL", "composer-2.5"),
		JLPTClassifyTimeout:   12 * time.Second,
		JLPTClassifyMaxPerExt: 10,
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

	switch cfg.JLPTClassify {
	case "on", "off", "auto", "1", "0", "true", "false", "yes", "no":
		// normalize aliases
		switch cfg.JLPTClassify {
		case "1", "true", "yes":
			cfg.JLPTClassify = "on"
		case "0", "false", "no":
			cfg.JLPTClassify = "off"
		}
	default:
		return Config{}, fmt.Errorf("JKRT_JLPT_CLASSIFY must be on, off, or auto, got %q", cfg.JLPTClassify)
	}

	if raw := os.Getenv("JKRT_JLPT_CLASSIFY_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("JKRT_JLPT_CLASSIFY_TIMEOUT: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("JKRT_JLPT_CLASSIFY_TIMEOUT must be positive")
		}
		cfg.JLPTClassifyTimeout = d
	}
	if raw := os.Getenv("JKRT_JLPT_CLASSIFY_MAX_PER_EXTRACT"); raw != "" {
		var n int
		if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n < 0 {
			return Config{}, fmt.Errorf("JKRT_JLPT_CLASSIFY_MAX_PER_EXTRACT must be non-negative int")
		}
		cfg.JLPTClassifyMaxPerExt = n
	}

	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
