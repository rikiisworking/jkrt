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
	"github.com/rikiisworking/jkrt/internal/review"
	"github.com/rikiisworking/jkrt/internal/schedule"
	"github.com/rikiisworking/jkrt/internal/scrape"
)

func newTestApp(t *testing.T, authOn bool) *jkrthttp.App {
	t.Helper()
	return newTestAppOpts(t, authOn, filepath.Join("..", "..", "web", "static"), nil)
}

// http.New copies Review.Params onto DB so extract + LibraryCounts share one knobs set.
func TestAppNewSyncsReviewParamsToDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sync.db")
	database, err := db.Open(dbPath, filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := auth.EnsureLearnerRow(auth.NewStore(database.SQL())); err != nil {
		t.Fatal(err)
	}
	ana, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}

	p := schedule.DefaultParams()
	p.StartingEase = 1.85
	p.ComfortableIntervalDays = 5
	rev := review.New(database, p)

	app := jkrthttp.New(jkrthttp.Options{
		Config: config.Config{
			Addr:        ":0",
			AuthEnabled: false,
			DBPath:      dbPath,
		},
		DB:       database,
		Analyzer: ana,
		Review:   rev,
	})
	if app.Review.Params().StartingEase != 1.85 {
		t.Fatalf("review params: %+v", app.Review.Params())
	}

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	// Ingest after New so extract sees synced schedule params.
	if _, err := database.IngestText(db.LearnerUserID, "経済政策を発表した。", ana, now); err != nil {
		t.Fatal(err)
	}
	var ease float64
	if err := database.SQL().QueryRow(`SELECT ease FROM cards WHERE user_id = 1 LIMIT 1`).Scan(&ease); err != nil {
		t.Fatal(err)
	}
	if ease != 1.85 {
		t.Fatalf("extract ease: got %v want 1.85 (Review.Params should sync to DB)", ease)
	}

	// interval 10 ≥ threshold 5 → mature
	_, err = database.SQL().Exec(
		`UPDATE cards SET phase = 'review', interval_days = 10, due_at = ? WHERE user_id = 1`,
		now.Add(24*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := database.LibraryCounts(db.LearnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if lib.MatureCards < 1 {
		t.Fatalf("mature with threshold 5: %+v", lib)
	}
}

// Stats and export JSON must share the same snapshot.Load composition (queue + library).
func TestStatsAndExportShareSnapshotComposition(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	res, err := app.Review.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	if err := app.Review.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", res.Item.UpdatedAt, now); err != nil {
		t.Fatal(err)
	}

	sreq := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	sresp, err := app.Fiber.Test(sreq)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	sbody, _ := io.ReadAll(sresp.Body)

	ereq := httptest.NewRequest(http.MethodGet, "/api/export?format=json", nil)
	eresp, err := app.Fiber.Test(ereq)
	if err != nil {
		t.Fatal(err)
	}
	defer eresp.Body.Close()
	ebody, _ := io.ReadAll(eresp.Body)

	var statsPayload struct {
		Queue   review.Stats      `json:"queue"`
		Library db.LibraryCounts  `json:"library"`
	}
	var exportPayload struct {
		Queue   review.Stats     `json:"queue"`
		Library db.LibraryCounts `json:"library"`
	}
	if err := json.Unmarshal(sbody, &statsPayload); err != nil {
		t.Fatalf("stats json: %v", err)
	}
	if err := json.Unmarshal(ebody, &exportPayload); err != nil {
		t.Fatalf("export json: %v", err)
	}
	if statsPayload.Queue.ReviewsToday != 1 || exportPayload.Queue.ReviewsToday != 1 {
		t.Fatalf("reviews today stats=%+v export=%+v", statsPayload.Queue, exportPayload.Queue)
	}
	if statsPayload.Library.Cards != exportPayload.Library.Cards {
		t.Fatalf("cards stats=%d export=%d", statsPayload.Library.Cards, exportPayload.Library.Cards)
	}
	if statsPayload.Queue.NewCount != exportPayload.Queue.NewCount {
		t.Fatalf("new count stats=%d export=%d", statsPayload.Queue.NewCount, exportPayload.Queue.NewCount)
	}
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

// Expired session cookie is treated as unauthenticated (Phase 5).
func TestExpiredSessionRedirectsHTML(t *testing.T) {
	app := newTestApp(t, true)
	cookie := signedSessionCookie(t, time.Now().UTC().Add(-time.Hour))

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

func TestExpiredSessionUnauthorizedAPI(t *testing.T) {
	app := newTestApp(t, true)
	cookie := signedSessionCookie(t, time.Now().UTC().Add(-time.Minute))

	req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
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

func TestExpiredSessionUnauthorizedJSONAccept(t *testing.T) {
	app := newTestApp(t, true)
	cookie := signedSessionCookie(t, time.Now().UTC().Add(-time.Second))

	req := httptest.NewRequest(http.MethodGet, "/review", nil)
	req.Header.Set("Accept", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", resp.StatusCode)
	}
}

// signedSessionCookie builds a valid HMAC cookie that expires at exp (may be past).
func signedSessionCookie(t *testing.T, exp time.Time) string {
	t.Helper()
	secret := []byte("0123456789abcdef0123456789abcdef")
	payload := fmt.Sprintf("%d|%d", auth.UserID, exp.Unix())
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	raw := payload + "|" + hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func TestLoginCookieMaxAgeMatchesTTL(t *testing.T) {
	app := newTestApp(t, true)
	form := strings.NewReader("password=test-password-change-me")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	wantMax := int(time.Hour.Seconds()) // newTestApp SessionTTL
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			if c.MaxAge != wantMax {
				t.Fatalf("MaxAge: got %d want %d", c.MaxAge, wantMax)
			}
			if !c.HttpOnly {
				t.Fatal("cookie should be HttpOnly")
			}
			return
		}
	}
	t.Fatal("session cookie missing")
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

// After password rotate, old password fails login; new password works (Phase 5).
func TestLoginAfterPasswordRotate(t *testing.T) {
	app := newTestApp(t, true)
	if err := auth.SetPassword(app.Store, "rotated-pass"); err != nil {
		t.Fatal(err)
	}

	// Old bootstrap password rejected
	form := strings.NewReader("password=test-password-change-me")
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password status: got %d want 401", resp.StatusCode)
	}

	// New password accepted
	form2 := strings.NewReader("password=rotated-pass")
	req2 := httptest.NewRequest(http.MethodPost, "/login", form2)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp2, err := app.Fiber.Test(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusFound {
		t.Fatalf("new password status: got %d want 302", resp2.StatusCode)
	}
	var session string
	for _, c := range resp2.Cookies() {
		if c.Name == auth.CookieName {
			session = c.Value
		}
	}
	if session == "" {
		t.Fatal("expected session cookie after rotate login")
	}
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session})
	resp3, err := app.Fiber.Test(req3)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("authed index: %d", resp3.StatusCode)
	}
}

// Wrong HMAC secret is unauthenticated even if payload shape is valid.
func TestWrongSessionSecretRejected(t *testing.T) {
	app := newTestApp(t, true)
	// Sign with a different secret than the app's SessionSecret.
	wrongSecret := []byte("ffffffffffffffffffffffffffffffff")
	exp := time.Now().UTC().Add(time.Hour).Unix()
	payload := fmt.Sprintf("%d|%d", auth.UserID, exp)
	mac := hmac.New(sha256.New, wrongSecret)
	_, _ = mac.Write([]byte(payload))
	raw := payload + "|" + hex.EncodeToString(mac.Sum(nil))
	cookie := base64.RawURLEncoding.EncodeToString([]byte(raw))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: got %d want 302", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/login" {
		t.Fatalf("Location: %q", resp.Header.Get("Location"))
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
	// Empty-but-valid RSS so multi-publisher defaults succeed without live network.
	empty := []byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`)
	return fixtureHTTPClient{
		"https://fixture.test/nhk_main.xml": main,
		"https://fixture.test/nhk_easy.xml": easy,
		scrape.DefaultYahooTopicsRSSURL:    empty,
		scrape.DefaultITmediaNewsRSSURL:    empty,
		scrape.DefaultBBCJapaneseRSSURL:    empty,
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
	wantN := len(scrape.DefaultSources("https://fixture.test/nhk_main.xml", "https://fixture.test/nhk_easy.xml"))
	if len(result.Sources) != wantN {
		t.Fatalf("sources: got %d want %d %+v", len(result.Sources), wantN, result.Sources)
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
	wantN := len(scrape.DefaultSources("https://fixture.test/nhk_main.xml", "https://fixture.test/nhk_easy.xml"))
	if len(result.Sources) != wantN {
		t.Fatalf("sources: got %d want %d %+v", len(result.Sources), wantN, result.Sources)
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
	if len(result.Sources) < 2 {
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
	// Extra publishers have no fixture → not ok (partial success still HTTP 200).
	// Ensure wire JSON includes "error" for failed source.
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

func TestReviewEmptyQueue(t *testing.T) {
	app := newTestApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/review", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Queue empty") {
		t.Fatalf("expected empty queue HTML, body=%s", body)
	}
}

func TestReviewAuthRequired(t *testing.T) {
	app := newTestApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/review", nil)
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

func TestReviewCardAndGrade(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/review", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, `name="card_id"`) || !strings.Contains(s, `name="grade"`) {
		t.Fatalf("expected grade form, body=%s", s)
	}
	if !strings.Contains(s, `name="card_updated_at"`) {
		t.Fatalf("expected card_updated_at lock token, body=%s", s)
	}
	if !strings.Contains(s, `hx-post="/review"`) {
		t.Fatalf("expected HTMX form wiring")
	}
	if !strings.Contains(s, `class="unfamiliar"`) {
		t.Fatalf("expected unfamiliar highlight, body=%s", s)
	}
	if !strings.Contains(s, "Furigana") {
		t.Fatalf("expected furigana toggle")
	}
	// Furigana default off: CSS hides rt unless body.show-furi
	if !strings.Contains(s, "body.show-furi") && !strings.Contains(s, "show-furi") {
		t.Fatalf("expected furigana CSS toggle class")
	}
	// Sentence context present
	if !strings.Contains(s, "経済") {
		t.Fatalf("expected Japanese sentence content")
	}
	// All four grade buttons
	for _, g := range []string{"again", "hard", "good", "easy"} {
		if !strings.Contains(s, `value="`+g+`"`) {
			t.Fatalf("missing grade button %s", g)
		}
	}

	cardID := formHiddenValue(t, s, "card_id")
	sentID := formHiddenValue(t, s, "sentence_id")

	upd := formHiddenValue(t, s, "card_updated_at")
	form := strings.NewReader(fmt.Sprintf("card_id=%s&sentence_id=%s&card_updated_at=%s&grade=good", cardID, sentID, upd))
	preq := httptest.NewRequest(http.MethodPost, "/review", form)
	preq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	presp, err := app.Fiber.Test(preq)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer presp.Body.Close()
	if presp.StatusCode != http.StatusFound {
		t.Fatalf("POST status: got %d want 302", presp.StatusCode)
	}
	if presp.Header.Get("Location") != "/review" {
		t.Fatalf("Location: %q", presp.Header.Get("Location"))
	}

	var phase string
	if err := app.DB.SQL().QueryRow(`SELECT phase FROM cards WHERE id = ?`, cardID).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if phase != "learning" {
		t.Fatalf("phase after grade: %s", phase)
	}

	var reviewCount int
	if err := app.DB.SQL().QueryRow(`SELECT COUNT(1) FROM reviews WHERE card_id = ?`, cardID).Scan(&reviewCount); err != nil {
		t.Fatal(err)
	}
	if reviewCount != 1 {
		t.Fatalf("reviews rows: %d", reviewCount)
	}
}

func TestReviewBadGrade(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	res, err := app.Review.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need a card")
	}
	form := strings.NewReader(fmt.Sprintf(
		"card_id=%d&sentence_id=%d&card_updated_at=%s&grade=maybe",
		res.Item.CardID, res.Item.SentenceID, res.Item.UpdatedAt,
	))
	req := httptest.NewRequest(http.MethodPost, "/review", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
}

// HTMX validation errors re-show next card (or empty) with an alert banner.
func TestReviewBadGradeHTMXReturnsPartial(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	res, err := app.Review.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need a card")
	}
	form := strings.NewReader(fmt.Sprintf(
		"card_id=%d&sentence_id=%d&card_updated_at=%s&grade=maybe",
		res.Item.CardID, res.Item.SentenceID, res.Item.UpdatedAt,
	))
	req := httptest.NewRequest(http.MethodPost, "/review", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if strings.Contains(s, "<!DOCTYPE") {
		t.Fatal("HTMX error should return partial, not full document")
	}
	if !strings.Contains(s, `role="alert"`) {
		t.Fatalf("expected error banner, body=%s", s)
	}
	// Card still new (grade rejected); queue should still show a card form.
	if !strings.Contains(s, `name="card_id"`) && !strings.Contains(s, "Queue empty") {
		t.Fatalf("expected review partial content, body=%s", s)
	}
	var n int
	if err := app.DB.SQL().QueryRow(`SELECT COUNT(1) FROM reviews`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("bad grade must not insert reviews: %d", n)
	}
}

func TestReviewSentenceNotLinked(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	res, err := app.Review.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	var articleID int64
	if err := app.DB.SQL().QueryRow(`SELECT id FROM articles LIMIT 1`).Scan(&articleID); err != nil {
		t.Fatal(err)
	}
	r, err := app.DB.SQL().Exec(`INSERT INTO sentences (article_id, text, order_index) VALUES (?, '別。', 99)`, articleID)
	if err != nil {
		t.Fatal(err)
	}
	badSID, _ := r.LastInsertId()
	form := strings.NewReader(fmt.Sprintf(
		"card_id=%d&sentence_id=%d&card_updated_at=%s&grade=good",
		res.Item.CardID, badSID, res.Item.UpdatedAt,
	))
	req := httptest.NewRequest(http.MethodPost, "/review", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
}

func TestReviewMissingFields(t *testing.T) {
	app := newTestApp(t, false)
	form := strings.NewReader("grade=good")
	req := httptest.NewRequest(http.MethodPost, "/review", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
}

func TestReviewCardNotFound(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	res, err := app.Review.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need a card for sentence_id")
	}
	form := strings.NewReader(fmt.Sprintf(
		"card_id=99999&sentence_id=%d&card_updated_at=%s&grade=good",
		res.Item.SentenceID, res.Item.UpdatedAt,
	))
	req := httptest.NewRequest(http.MethodPost, "/review", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", resp.StatusCode)
	}
}

func TestReviewHTMXReturnsHTML(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	res, err := app.Review.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	form := strings.NewReader(fmt.Sprintf(
		"card_id=%d&sentence_id=%d&card_updated_at=%s&grade=good",
		res.Item.CardID, res.Item.SentenceID, res.Item.UpdatedAt,
	))
	req := httptest.NewRequest(http.MethodPost, "/review", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// HTMX path returns 200 partial for #review-main (not full document, not 302)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if strings.Contains(s, "<!DOCTYPE") {
		t.Fatalf("HTMX should return partial, not full document")
	}
	if !strings.Contains(s, "Queue empty") && !strings.Contains(s, `name="card_id"`) && !strings.Contains(s, "unfamiliar") {
		t.Fatalf("expected review partial after HTMX grade, body=%s", s)
	}
}

func TestReviewDoubleSubmitStaleIsIdempotent(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	res, err := app.Review.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	formBody := fmt.Sprintf(
		"card_id=%d&sentence_id=%d&card_updated_at=%s&grade=good",
		res.Item.CardID, res.Item.SentenceID, res.Item.UpdatedAt,
	)
	// First grade
	req1 := httptest.NewRequest(http.MethodPost, "/review", strings.NewReader(formBody))
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp1, err := app.Fiber.Test(req1)
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusFound {
		t.Fatalf("first grade: %d", resp1.StatusCode)
	}
	// Second grade with same token must not double-apply
	req2 := httptest.NewRequest(http.MethodPost, "/review", strings.NewReader(formBody))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp2, err := app.Fiber.Test(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusFound {
		t.Fatalf("stale grade should re-next (302), got %d", resp2.StatusCode)
	}
	var n int
	if err := app.DB.SQL().QueryRow(`SELECT COUNT(1) FROM reviews WHERE card_id = ?`, res.Item.CardID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected one review row after double submit, got %d", n)
	}
	var phase string
	var step int
	if err := app.DB.SQL().QueryRow(`SELECT phase, learning_step FROM cards WHERE id = ?`, res.Item.CardID).Scan(&phase, &step); err != nil {
		t.Fatal(err)
	}
	// new+Good once → learning step 0 (not step 1 from double Good)
	if phase != "learning" || step != 0 {
		t.Fatalf("after double submit: phase=%s step=%d (want learning/0)", phase, step)
	}
}

// formHiddenValue extracts value="..." from <input ... name="NAME" value="...">
// (value may appear before or after name).
func formHiddenValue(t *testing.T, htmlBody, name string) string {
	t.Helper()
	// Prefer pattern: name="X" ... value="Y" within the same tag
	marker := `name="` + name + `"`
	i := strings.Index(htmlBody, marker)
	if i < 0 {
		t.Fatalf("missing name=%q", name)
	}
	// Expand to nearest <input ...> bounds
	tagStart := strings.LastIndex(htmlBody[:i+1], "<input")
	if tagStart < 0 {
		tagStart = i
	}
	tagEnd := strings.Index(htmlBody[i:], ">")
	if tagEnd < 0 {
		t.Fatalf("unclosed input for %s", name)
	}
	tag := htmlBody[tagStart : i+tagEnd]
	const pref = `value="`
	j := strings.Index(tag, pref)
	if j < 0 {
		t.Fatalf("no value in input tag %q", tag)
	}
	rest := tag[j+len(pref):]
	k := strings.Index(rest, `"`)
	if k < 0 {
		t.Fatal("unclosed value")
	}
	return rest[:k]
}

func TestDashboardEmpty(t *testing.T) {
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
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	for _, want := range []string{
		"Japanese Kanji Reading Trainer",
		"Due",
		"Session progress",
		"Scrape NHK",
		"No articles yet",
		`hx-post="/api/scrape"`,
		`href="/articles"`,
		`href="/review"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in dashboard", want)
		}
	}
}

func TestDashboardWithData(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if strings.Contains(s, "No articles yet") {
		t.Fatal("should not show empty library hint after ingest")
	}
	if !strings.Contains(s, "articles") {
		t.Fatal("expected article count section")
	}
	// New cards should show in new count (not zero)
	if !strings.Contains(s, "New in queue") {
		t.Fatal("expected new queue label")
	}
}

func TestArticlesListEmpty(t *testing.T) {
	app := newTestApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "No articles") {
		t.Fatalf("expected empty articles state, body=%s", body)
	}
}

func TestArticlesListAndDetail(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	res, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, fmt.Sprintf(`/articles/%d`, res.ArticleID)) {
		t.Fatalf("expected link to article %d", res.ArticleID)
	}
	if !strings.Contains(s, "manual") && !strings.Contains(s, db.ManualSourceName) {
		t.Fatalf("expected source name in list")
	}

	dreq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/articles/%d", res.ArticleID), nil)
	dresp, err := app.Fiber.Test(dreq)
	if err != nil {
		t.Fatal(err)
	}
	defer dresp.Body.Close()
	if dresp.StatusCode != http.StatusOK {
		t.Fatalf("detail status: %d", dresp.StatusCode)
	}
	dbody, _ := io.ReadAll(dresp.Body)
	ds := string(dbody)
	if !strings.Contains(ds, "経済") {
		t.Fatalf("expected sentence text, body=%s", ds)
	}
	if !strings.Contains(ds, "Sentence") {
		t.Fatal("expected sentence labels")
	}
}

func TestArticleNotFound(t *testing.T) {
	app := newTestApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/articles/99999", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not found") {
		t.Fatalf("body: %s", body)
	}
}

func TestArticleInvalidID(t *testing.T) {
	app := newTestApp(t, false)
	for _, path := range []string{"/articles/0", "/articles/-1", "/articles/abc"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp, err := app.Fiber.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status got %d want 400 body=%s", path, resp.StatusCode, body)
		}
	}
}

func TestArticlesAuthRequired(t *testing.T) {
	app := newTestApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: got %d want 302", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/login" {
		t.Fatalf("Location: %q", resp.Header.Get("Location"))
	}
}

func TestScrapeHTMXReturnsHTML(t *testing.T) {
	client := loadRSSFixtures(t)
	app := newTestAppOpts(t, false, filepath.Join("..", "..", "web", "static"), client)
	req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
	req.Header.Set("HX-Request", "true")
	resp, err := app.Fiber.Test(req, 10000)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "Scrape finished") {
		t.Fatalf("expected HTML summary, body=%s", s)
	}
	if strings.HasPrefix(strings.TrimSpace(s), "{") {
		t.Fatal("HTMX scrape must not return raw JSON")
	}
}

// --- Phase 6: stats + export ---

func TestStatsAPI(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var payload struct {
		Queue struct {
			DueCount int `json:"DueCount"`
			NewCount int `json:"NewCount"`
		} `json:"queue"`
		Library struct {
			Articles int            `json:"Articles"`
			Words    int            `json:"Words"`
			Cards    int            `json:"Cards"`
			ByPhase  map[string]int `json:"ByPhase"`
		} `json:"library"`
		AsOf string `json:"as_of"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if payload.AsOf == "" {
		t.Fatal("missing as_of")
	}
	if payload.Library.Articles != 1 {
		t.Fatalf("articles: %d", payload.Library.Articles)
	}
	if payload.Library.Words < 1 || payload.Library.Cards < 1 {
		t.Fatalf("words/cards: %+v", payload.Library)
	}
	if payload.Library.ByPhase["new"] < 1 {
		t.Fatalf("by_phase new: %+v", payload.Library.ByPhase)
	}
	if payload.Queue.NewCount < 1 {
		t.Fatalf("queue new: %+v", payload.Queue)
	}
}

func TestStatsAPIRequiresAuth(t *testing.T) {
	app := newTestApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", resp.StatusCode)
	}
}

func TestExportJSONAPI(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/export?format=json", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type: %s", ct)
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "jkrt-export.json") {
		t.Fatalf("Content-Disposition: %s", resp.Header.Get("Content-Disposition"))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "経済") {
		t.Fatalf("export missing lemma, body=%s", body)
	}
	if !strings.Contains(string(body), `"cards"`) {
		t.Fatal("export missing cards")
	}
}

// Default format (no query) is JSON.
func TestExportDefaultFormatIsJSON(t *testing.T) {
	app := newTestApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/api/export", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type: %s", resp.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"user_id"`) {
		t.Fatalf("body: %s", body)
	}
}

func TestExportCSVAPI(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/export?format=csv", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("Content-Type: %s", resp.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "lemma,reading") {
		t.Fatalf("csv header: %s", s)
	}
	if !strings.Contains(s, "経済") {
		t.Fatalf("csv body: %s", s)
	}
}

func TestExportBadFormat(t *testing.T) {
	app := newTestApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/api/export?format=xml", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
}

func TestExportRequiresAuth(t *testing.T) {
	app := newTestApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/export", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", resp.StatusCode)
	}
}

func TestDashboardShowsExportLinks(t *testing.T) {
	app := newTestApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	for _, want := range []string{
		`/api/export?format=json`,
		`/api/export?format=csv`,
		`/api/stats`,
		"Export",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
}

func TestDashboardLibraryNumbersAfterIngest(t *testing.T) {
	app := newTestApp(t, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := app.DB.IngestText(db.LearnerUserID, "経済政策を発表した。", app.Analyzer, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	// dashboardHTML: "Cards: N · words: N · reviews: N · mature: N"
	if !strings.Contains(s, "Cards:") || !strings.Contains(s, "words:") {
		t.Fatalf("expected library card/word summary")
	}
	// by-phase line always rendered
	if !strings.Contains(s, "relearning") {
		t.Fatalf("expected phase breakdown on dashboard")
	}
	if strings.Contains(s, "No articles yet") {
		t.Fatal("should not show empty hint after ingest")
	}
}

func TestExportCSVContentDisposition(t *testing.T) {
	app := newTestApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/api/export?format=csv", nil)
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "jkrt-cards.csv") {
		t.Fatalf("Content-Disposition: %s", resp.Header.Get("Content-Disposition"))
	}
}

func TestExportWithAuthCookie(t *testing.T) {
	app := newTestApp(t, true)
	cookie := loginCookie(t, app)
	req := httptest.NewRequest(http.MethodGet, "/api/export?format=json", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	resp, err := app.Fiber.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}
