package config

import (
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
}
