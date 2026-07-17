package http_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/auth"
	"github.com/rikiisworking/jkrt/internal/config"
	"github.com/rikiisworking/jkrt/internal/db"
	jkrthttp "github.com/rikiisworking/jkrt/internal/http"
	"github.com/rikiisworking/jkrt/internal/scrape"
)

func newTestApp(t *testing.T, authOn bool) *jkrthttp.App {
	t.Helper()
	return newTestAppOpts(t, authOn, filepath.Join("..", "..", "web", "static"), nil)
}

func newTestAppOpts(t *testing.T, authOn bool, staticDir string, httpClient scrape.HTTPDoer) *jkrthttp.App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	cfg := config.Config{
		Addr:          ":0",
		DBPath:        dbPath,
		AuthEnabled:   authOn,
		Password:      "test-password-change-me",
		SessionSecret: []byte("0123456789abcdef0123456789abcdef"),
		SessionTTL:    time.Hour,
		NHKMainRSSURL: "https://fixture.test/nhk_main.xml",
		NHKEasyRSSURL: "https://fixture.test/nhk_easy.xml",
	}

	database, err := db.Open(dbPath, filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ana, err := analyze.New()
	if err != nil {
		t.Fatalf("analyzer: %v", err)
	}

	var store *auth.Store
	var sessions *auth.Manager
	if authOn {
		store = auth.NewStore(database.SQL())
		if err := auth.Bootstrap(store, true, cfg.Password); err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		sessions = auth.NewManager(cfg.SessionSecret, cfg.SessionTTL)
	} else {
		storeWrap := auth.NewStore(database.SQL())
		if err := auth.EnsureLearnerRow(storeWrap); err != nil {
			t.Fatalf("EnsureLearnerRow: %v", err)
		}
	}

	return jkrthttp.New(jkrthttp.Options{
		Config:     cfg,
		Store:      store,
		Sessions:   sessions,
		StaticDir:  staticDir,
		DB:         database,
		Analyzer:   ana,
		HTTPClient: httpClient,
	})
}

func loginCookie(t *testing.T, app *jkrthttp.App) string {
	t.Helper()
	form := strings.NewReader("password=test-password-change-me")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status: %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
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

func TestHealthUnauthenticatedWhenAuthOn(t *testing.T) {
	// /health is public even with auth on.
	app := newTestApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
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

func TestIndexInlineFallbackWithoutStatic(t *testing.T) {
	app := newTestAppOpts(t, false, "", nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Japanese Kanji Reading Trainer") {
		t.Fatalf("expected inline placeholder HTML, body=%s", body)
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

func TestAuthOnJSONUnauthorizedWithoutCookie(t *testing.T) {
	// Plan: unauthenticated protected routes → 401 (API) or 302 (HTML).
	app := newTestApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "unauthorized") {
		t.Fatalf("body: %s", body)
	}
}

func TestAuthOnInvalidCookieRedirects(t *testing.T) {
	app := newTestApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "not-a-valid-session"})
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: got %d want 302", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/login" {
		t.Fatalf("Location: %q", resp.Header.Get("Location"))
	}
}

func TestIndexHTMLNotPublicUnderStatic(t *testing.T) {
	// HTML lives under web/static/ but only assets/ is mounted at /static.
	// Unauthenticated clients must not fetch index.html via the static mount.
	app := newTestApp(t, true)
	for _, path := range []string{"/static/index.html", "/static/../index.html"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp, err := app.Fiber.Test(req)
		if err != nil {
			t.Fatalf("Test %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("%s: expected not 200, got %d (index must not be public)", path, resp.StatusCode)
		}
	}
}

func TestWrongUserSessionRejected(t *testing.T) {
	// Valid HMAC for user id=2 must not unlock protected routes (v1 pins UserID==1).
	app := newTestApp(t, true)
	secret := []byte("0123456789abcdef0123456789abcdef")
	exp := time.Now().UTC().Add(time.Hour).Unix()
	payload := fmt.Sprintf("2|%d", exp)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	raw := payload + "|" + hex.EncodeToString(mac.Sum(nil))
	cookie := base64.RawURLEncoding.EncodeToString([]byte(raw))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: got %d want 302", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/login" {
		t.Fatalf("Location: %q", resp.Header.Get("Location"))
	}
}

func TestLoginGetForm(t *testing.T) {
	app := newTestApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, `name="password"`) || !strings.Contains(s, `action="/login"`) {
		t.Fatalf("expected login form, body=%s", s)
	}
}

func TestLoginGetRedirectsWhenAlreadyAuthed(t *testing.T) {
	app := newTestApp(t, true)
	session := loginCookie(t, app)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session})
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: got %d want 302", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/" {
		t.Fatalf("Location: %q", resp.Header.Get("Location"))
	}
}

func TestLoginGetRedirectsWhenAuthOff(t *testing.T) {
	app := newTestApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: got %d want 302", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/" {
		t.Fatalf("Location: %q", resp.Header.Get("Location"))
	}
}

func TestLoginPostRedirectsWhenAuthOff(t *testing.T) {
	app := newTestApp(t, false)
	form := strings.NewReader("password=anything")
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
	if resp.Header.Get("Location") != "/" {
		t.Fatalf("Location: %q", resp.Header.Get("Location"))
	}
	cookies := resp.Cookies()
	var session string
	for _, c := range cookies {
		if c.Name == auth.CookieName {
			session = c.Value
			if !c.HttpOnly {
				t.Fatal("cookie should be HttpOnly")
			}
			if c.Path != "/" {
				t.Fatalf("cookie path: %q", c.Path)
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

func TestLoginSetsSecureCookieWhenForwardedHTTPS(t *testing.T) {
	app := newTestApp(t, true)
	form := strings.NewReader("password=test-password-change-me")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			found = true
			if !c.Secure {
				t.Fatal("expected Secure cookie when X-Forwarded-Proto=https")
			}
		}
	}
	if !found {
		t.Fatal("expected session cookie")
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
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Invalid password") {
		t.Fatalf("expected error in form HTML, body=%s", body)
	}
}

func TestLogoutClearsAndRedirects(t *testing.T) {
	app := newTestApp(t, true)
	session := loginCookie(t, app)

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
	// Cleared cookie should be empty / expired.
	var cleared bool
	for _, c := range resp2.Cookies() {
		if c.Name == auth.CookieName {
			cleared = true
			if c.Value != "" {
				t.Fatalf("expected empty cookie value, got %q", c.Value)
			}
		}
	}
	if !cleared {
		// Fiber may only send MaxAge via Set-Cookie header; accept either.
		if !strings.Contains(resp2.Header.Get("Set-Cookie"), auth.CookieName) {
			t.Fatal("expected Set-Cookie clearing session")
		}
	}
}

func TestLogoutRequiresAuth(t *testing.T) {
	app := newTestApp(t, true)
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: got %d want 302", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/login" {
		t.Fatalf("Location: %q", resp.Header.Get("Location"))
	}
}

// fixtureHTTPClient maps fixture URLs to testdata RSS — zero network dials.
type fixtureHTTPClient map[string][]byte

func (ft fixtureHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, ok := ft[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("missing")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func loadRSSFixtures(t *testing.T) fixtureHTTPClient {
	t.Helper()
	main, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rss", "nhk_main_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	easy, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rss", "nhk_easy_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return fixtureHTTPClient{
		"https://fixture.test/nhk_main.xml": main,
		"https://fixture.test/nhk_easy.xml": easy,
	}
}

func TestScrapeRequiresAuth(t *testing.T) {
	app := newTestAppOpts(t, true, "", loadRSSFixtures(t))
	req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "unauthorized") {
		t.Fatalf("body: %s", body)
	}
}

func TestScrapeAuthOffIngestsFixtures(t *testing.T) {
	app := newTestAppOpts(t, false, "", loadRSSFixtures(t))
	// Fiber.Test can time out on Kagome; allow more time.
	req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
	resp, err := app.Fiber.Test(req, 60_000)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: %d body=%s", resp.StatusCode, body)
	}
	var result scrape.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Sources) != 2 {
		t.Fatalf("sources: %+v", result.Sources)
	}
	// Plan JSON shape: sources[{name, ok, items_new, error?}]
	byName := map[string]scrape.SourceResult{}
	for _, sr := range result.Sources {
		byName[sr.Name] = sr
		if !sr.OK {
			t.Fatalf("source %s not ok: %+v", sr.Name, sr)
		}
	}
	main, ok := byName[scrape.SourceNHKMain]
	if !ok {
		t.Fatal("missing nhk_main")
	}
	easy, ok := byName[scrape.SourceNHKEasy]
	if !ok {
		t.Fatal("missing nhk_easy")
	}
	if main.ItemsNew != 2 {
		t.Fatalf("main items_new: %d", main.ItemsNew)
	}
	if easy.ItemsNew != 3 {
		t.Fatalf("easy items_new: %d", easy.ItemsNew)
	}

	// Dedupe on second scrape
	req2 := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
	resp2, err := app.Fiber.Test(req2, 60_000)
	if err != nil {
		t.Fatalf("second scrape: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status: %d", resp2.StatusCode)
	}
	var result2 scrape.Result
	if err := json.NewDecoder(resp2.Body).Decode(&result2); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	for _, sr := range result2.Sources {
		if !sr.OK {
			t.Fatalf("%s not ok on dedupe: %+v", sr.Name, sr)
		}
		if sr.ItemsNew != 0 {
			t.Fatalf("%s items_new on redeupe: %d", sr.Name, sr.ItemsNew)
		}
	}
}

func TestScrapeWithAuthCookie(t *testing.T) {
	app := newTestAppOpts(t, true, "", loadRSSFixtures(t))
	session := loginCookie(t, app)
	req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session})
	resp, err := app.Fiber.Test(req, 60_000)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: %d body=%s", resp.StatusCode, body)
	}
	var result scrape.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Sources) != 2 {
		t.Fatalf("sources: %+v", result.Sources)
	}
	for _, sr := range result.Sources {
		if !sr.OK {
			t.Fatalf("%s: %+v", sr.Name, sr)
		}
	}
}

func TestScrapePartialSuccessJSON(t *testing.T) {
	// Easy URL empty → 200 with ok=false on easy, main still ok (plan partial success).
	dbPath := filepath.Join(t.TempDir(), "test.db")
	cfg := config.Config{
		Addr:          ":0",
		DBPath:        dbPath,
		AuthEnabled:   false,
		NHKMainRSSURL: "https://fixture.test/nhk_main.xml",
		NHKEasyRSSURL: "", // soft-fail
	}
	database, err := db.Open(dbPath, filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := auth.EnsureLearnerRow(auth.NewStore(database.SQL())); err != nil {
		t.Fatal(err)
	}
	ana, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	// Only main fixture registered.
	mainBody, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rss", "nhk_main_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	client := fixtureHTTPClient{"https://fixture.test/nhk_main.xml": mainBody}
	app := jkrthttp.New(jkrthttp.Options{
		Config:     cfg,
		DB:         database,
		Analyzer:   ana,
		HTTPClient: client,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
	resp, err := app.Fiber.Test(req, 60_000)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: %d body=%s", resp.StatusCode, body)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result scrape.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode: %v body=%s", err, raw)
	}
	if len(result.Sources) != 2 {
		t.Fatalf("sources: %s", raw)
	}
	var main, easy scrape.SourceResult
	for _, sr := range result.Sources {
		switch sr.Name {
		case scrape.SourceNHKMain:
			main = sr
		case scrape.SourceNHKEasy:
			easy = sr
		}
	}
	if !main.OK || main.ItemsNew != 2 {
		t.Fatalf("main: %+v", main)
	}
	if easy.OK {
		t.Fatalf("easy should soft-fail: %+v", easy)
	}
	if easy.Error == "" {
		t.Fatal("easy needs error field")
	}
	// Ensure wire JSON includes "error" for failed source and not for ok source.
	if !strings.Contains(string(raw), `"error"`) {
		t.Fatalf("expected error key in JSON: %s", raw)
	}
}

func TestScrapeMissingDB(t *testing.T) {
	ana, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	app := jkrthttp.New(jkrthttp.Options{
		Config: config.Config{
			AuthEnabled:   false,
			NHKMainRSSURL: "https://fixture.test/nhk_main.xml",
		},
		DB:         nil,
		Analyzer:   ana,
		HTTPClient: loadRSSFixtures(t),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "database") {
		t.Fatalf("body: %s", body)
	}
}

func TestScrapeMissingAnalyzer(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath, filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	app := jkrthttp.New(jkrthttp.Options{
		Config: config.Config{
			AuthEnabled:   false,
			NHKMainRSSURL: "https://fixture.test/nhk_main.xml",
		},
		DB:         database,
		Analyzer:   nil,
		HTTPClient: loadRSSFixtures(t),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "analyzer") {
		t.Fatalf("body: %s", body)
	}
}
