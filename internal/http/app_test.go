package http_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/auth"
	"github.com/rikiisworking/jkrt/internal/config"
	jkrthttp "github.com/rikiisworking/jkrt/internal/http"
)

func newTestApp(t *testing.T, authOn bool) *jkrthttp.App {
	t.Helper()
	cfg := config.Config{
		Addr:          ":0",
		DBPath:        filepath.Join(t.TempDir(), "test.db"),
		AuthEnabled:   authOn,
		Password:      "test-password-change-me",
		SessionSecret: []byte("0123456789abcdef0123456789abcdef"),
		SessionTTL:    time.Hour,
	}

	var store *auth.Store
	var sessions *auth.Manager
	if authOn {
		var err error
		store, err = auth.OpenStore(cfg.DBPath)
		if err != nil {
			t.Fatalf("OpenStore: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		if err := auth.Bootstrap(store, true, cfg.Password); err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		sessions = auth.NewManager(cfg.SessionSecret, cfg.SessionTTL)
	}

	// Prefer repo static if present; tests do not require it.
	static := filepath.Join("..", "..", "web", "static")
	return jkrthttp.New(jkrthttp.Options{
		Config:    cfg,
		Store:     store,
		Sessions:  sessions,
		StaticDir: static,
	})
}

func TestHealth(t *testing.T) {
	app := newTestApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("body: %s", body)
	}
}

func TestAuthOffIndexOpen(t *testing.T) {
	app := newTestApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

func TestAuthOnIndexRedirectsWithoutCookie(t *testing.T) {
	app := newTestApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: got %d want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/login" {
		t.Fatalf("Location: %q", loc)
	}
}

func TestAuthOnLoginSuccessSetsCookie(t *testing.T) {
	app := newTestApp(t, true)

	form := strings.NewReader("password=test-password-change-me")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: got %d want 302", resp.StatusCode)
	}
	cookies := resp.Cookies()
	var session string
	for _, c := range cookies {
		if c.Name == auth.CookieName {
			session = c.Value
			if !c.HttpOnly {
				t.Fatal("cookie should be HttpOnly")
			}
		}
	}
	if session == "" {
		t.Fatal("expected session cookie")
	}

	// Authenticated index
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session})
	resp2, err := app.Fiber.Test(req2)
	if err != nil {
		t.Fatalf("Test index: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("authenticated / status: %d", resp2.StatusCode)
	}
}

func TestAuthOnBadPassword(t *testing.T) {
	app := newTestApp(t, true)
	form := strings.NewReader("password=wrong")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", resp.StatusCode)
	}
}

func TestLogoutClearsAndRedirects(t *testing.T) {
	app := newTestApp(t, true)
	// Login first
	form := strings.NewReader("password=test-password-change-me")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	var session string
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			session = c.Value
		}
	}
	if session == "" {
		t.Fatal("no session")
	}

	req2 := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req2.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session})
	resp2, err := app.Fiber.Test(req2)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusFound {
		t.Fatalf("logout status: %d", resp2.StatusCode)
	}
	if resp2.Header.Get("Location") != "/login" {
		t.Fatalf("logout Location: %q", resp2.Header.Get("Location"))
	}
}
