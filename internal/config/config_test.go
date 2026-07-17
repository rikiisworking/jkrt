package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAuthOff(t *testing.T) {
	t.Setenv("JKRT_AUTH", "off")
	t.Setenv("JKRT_ADDR", ":9090")
	t.Setenv("JKRT_DB_PATH", "/tmp/test.db")
	// No session secret required when auth is off.
	t.Setenv("JKRT_SESSION_SECRET", "")
	t.Setenv("JKRT_PASSWORD", "")
	t.Setenv("JKRT_SESSION_TTL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AuthEnabled {
		t.Fatal("expected auth disabled")
	}
	if cfg.Addr != ":9090" {
		t.Fatalf("addr: got %q", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Fatalf("db path: got %q", cfg.DBPath)
	}
	if cfg.SessionTTL != 168*time.Hour {
		t.Fatalf("default TTL: got %v", cfg.SessionTTL)
	}
}

func TestLoadAuthOnRequiresSecret(t *testing.T) {
	t.Setenv("JKRT_AUTH", "on")
	t.Setenv("JKRT_SESSION_SECRET", "too-short")
	t.Setenv("JKRT_PASSWORD", "x")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for short session secret")
	}
}

func TestLoadAuthOnMissingSecret(t *testing.T) {
	t.Setenv("JKRT_AUTH", "on")
	t.Setenv("JKRT_SESSION_SECRET", "")
	t.Setenv("JKRT_PASSWORD", "x")
	t.Setenv("JKRT_SESSION_TTL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing session secret")
	}
}

func TestLoadAuthOnOK(t *testing.T) {
	t.Setenv("JKRT_AUTH", "on")
	t.Setenv("JKRT_SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("JKRT_PASSWORD", "secret")
	t.Setenv("JKRT_SESSION_TTL", "24h")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AuthEnabled {
		t.Fatal("expected auth enabled")
	}
	if cfg.SessionTTL != 24*time.Hour {
		t.Fatalf("TTL: got %v", cfg.SessionTTL)
	}
	if string(cfg.SessionSecret) != "0123456789abcdef0123456789abcdef" {
		t.Fatal("session secret mismatch")
	}
	if cfg.Password != "secret" {
		t.Fatalf("password: got %q", cfg.Password)
	}
}

func TestLoadDefaultsAuthOn(t *testing.T) {
	// Clear overrides so defaults apply (except required secret).
	t.Setenv("JKRT_AUTH", "")
	t.Setenv("JKRT_ADDR", "")
	t.Setenv("JKRT_DB_PATH", "")
	t.Setenv("JKRT_PASSWORD", "")
	t.Setenv("JKRT_SESSION_TTL", "")
	t.Setenv("JKRT_SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("JKRT_NHK_MAIN_RSS_URL", "")
	t.Setenv("JKRT_NHK_EASY_RSS_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AuthEnabled {
		t.Fatal("default auth should be on")
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("default addr: %q", cfg.Addr)
	}
	if cfg.DBPath != "./jkrt.db" {
		t.Fatalf("default db: %q", cfg.DBPath)
	}
	if cfg.NHKMainRSSURL != DefaultNHKMainRSSURL {
		t.Fatalf("default main RSS: %q", cfg.NHKMainRSSURL)
	}
	if cfg.NHKEasyRSSURL != "" {
		t.Fatalf("default easy RSS should be empty, got %q", cfg.NHKEasyRSSURL)
	}
}

func TestLoadRSSURLOverrides(t *testing.T) {
	t.Setenv("JKRT_AUTH", "off")
	t.Setenv("JKRT_SESSION_TTL", "")
	t.Setenv("JKRT_NHK_MAIN_RSS_URL", "https://example.test/main.xml")
	t.Setenv("JKRT_NHK_EASY_RSS_URL", "https://example.test/easy.xml")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.NHKMainRSSURL != "https://example.test/main.xml" {
		t.Fatalf("main: %q", cfg.NHKMainRSSURL)
	}
	if cfg.NHKEasyRSSURL != "https://example.test/easy.xml" {
		t.Fatalf("easy: %q", cfg.NHKEasyRSSURL)
	}
}

func TestLoadAuthAliases(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	for _, v := range []string{"ON", "1", "true", "yes"} {
		t.Setenv("JKRT_AUTH", v)
		t.Setenv("JKRT_SESSION_SECRET", secret)
		t.Setenv("JKRT_SESSION_TTL", "")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("auth %q: %v", v, err)
		}
		if !cfg.AuthEnabled {
			t.Fatalf("auth %q: expected enabled", v)
		}
	}
	for _, v := range []string{"OFF", "0", "false", "no"} {
		t.Setenv("JKRT_AUTH", v)
		t.Setenv("JKRT_SESSION_SECRET", "")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("auth %q: %v", v, err)
		}
		if cfg.AuthEnabled {
			t.Fatalf("auth %q: expected disabled", v)
		}
	}
}

func TestLoadInvalidAuth(t *testing.T) {
	t.Setenv("JKRT_AUTH", "maybe")
	t.Setenv("JKRT_SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid JKRT_AUTH")
	}
	if !strings.Contains(err.Error(), "JKRT_AUTH") {
		t.Fatalf("error: %v", err)
	}
}

func TestLoadInvalidTTL(t *testing.T) {
	t.Setenv("JKRT_AUTH", "off")
	t.Setenv("JKRT_SESSION_TTL", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid TTL")
	}
}

func TestLoadNonPositiveTTL(t *testing.T) {
	t.Setenv("JKRT_AUTH", "off")
	t.Setenv("JKRT_SESSION_TTL", "0s")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for zero TTL")
	}
}
