package db_test

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/schedule"
)

func TestOpenAndMigrate(t *testing.T) {
	d := openTestDB(t)
	// users table must exist (Phase 0 compat)
	var n int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='words'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("words table missing after migrate")
	}
}

func TestIngestTextFixtureCreatesWordsAndCards(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)

	a, err := analyze.New()
	if err != nil {
		t.Fatalf("analyzer: %v", err)
	}

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sentence := "経済政策を発表した。"

	res, err := d.IngestText(db.LearnerUserID, sentence, a, now)
	if err != nil {
		t.Fatalf("IngestText: %v", err)
	}
	if res.ArticleID == 0 {
		t.Fatal("expected article id")
	}
	if res.Status != db.IngestCreated {
		t.Fatalf("status: got %v want Created", res.Status)
	}

	var wordCount int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&wordCount); err != nil {
		t.Fatal(err)
	}
	if wordCount != 3 {
		t.Fatalf("expected exactly 3 words (経済/政策/発表), got %d", wordCount)
	}

	var cardCount int
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM cards WHERE user_id = 1 AND phase = 'new'`,
	).Scan(&cardCount); err != nil {
		t.Fatal(err)
	}
	if cardCount != wordCount {
		t.Fatalf("cards %d != words %d", cardCount, wordCount)
	}

	// New-card defaults from sm2-spec
	var phase string
	var step int
	var interval, ease float64
	var reps, lapses int
	var dueAt string
	err = d.SQL().QueryRow(
		`SELECT phase, learning_step, interval_days, ease, due_at, reps, lapses
		 FROM cards WHERE user_id = 1 LIMIT 1`,
	).Scan(&phase, &step, &interval, &ease, &dueAt, &reps, &lapses)
	if err != nil {
		t.Fatal(err)
	}
	wantEase := schedule.DefaultParams().StartingEase
	if phase != "new" || step != 0 || interval != 0 || ease != wantEase || reps != 0 || lapses != 0 {
		t.Fatalf("new card fields: phase=%s step=%d interval=%v ease=%v reps=%d lapses=%d",
			phase, step, interval, ease, reps, lapses)
	}
	if dueAt != now.UTC().Format(time.RFC3339) {
		t.Fatalf("due_at: got %q want %q", dueAt, now.UTC().Format(time.RFC3339))
	}

	// sentence_words with spans
	var swCount int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM sentence_words`).Scan(&swCount); err != nil {
		t.Fatal(err)
	}
	if swCount != wordCount {
		t.Fatalf("sentence_words %d != words %d", swCount, wordCount)
	}
}

func TestUniqueLemmaReading(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)

	a, err := analyze.New()
	if err != nil {
		t.Fatalf("analyzer: %v", err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sentence := "経済政策を発表した。"

	if _, err := d.IngestText(db.LearnerUserID, sentence, a, now); err != nil {
		t.Fatalf("first IngestText: %v", err)
	}
	if _, err := d.IngestText(db.LearnerUserID, sentence, a, now.Add(time.Second)); err != nil {
		t.Fatalf("second IngestText: %v", err)
	}

	var wordCount int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&wordCount); err != nil {
		t.Fatal(err)
	}
	// Same lemmas+readings must not duplicate word rows.
	var distinct int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM (SELECT DISTINCT lemma, reading FROM words)`).Scan(&distinct); err != nil {
		t.Fatal(err)
	}
	if wordCount != distinct {
		t.Fatalf("duplicate words: count=%d distinct=%d", wordCount, distinct)
	}

	// Still one card per word for the user.
	var cardCount int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM cards WHERE user_id = 1`).Scan(&cardCount); err != nil {
		t.Fatal(err)
	}
	if cardCount != wordCount {
		t.Fatalf("cards should stay 1:1 with words after re-ingest: cards=%d words=%d", cardCount, wordCount)
	}

	// But two sentence_words occurrences per word (two sentences under two articles).
	var swCount int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM sentence_words`).Scan(&swCount); err != nil {
		t.Fatal(err)
	}
	if swCount != wordCount*2 {
		t.Fatalf("expected 2 occurrences per word: sw=%d words=%d", swCount, wordCount)
	}
}

func TestEmptyReadingSkippedOnPersist(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sid := seedSentence(t, d, now, "漢字と空読み")

	// Manually craft candidates: one valid, one empty reading (must be skipped).
	cands := []analyze.Candidate{
		{Lemma: "漢字", Reading: "カンジ", Surface: "漢字", CharStart: 0, CharEnd: 2},
		{Lemma: "空", Reading: "", Surface: "空", CharStart: 3, CharEnd: 4},
	}
	if err := d.PersistCandidates(db.LearnerUserID, sid, cands, now); err != nil {
		t.Fatalf("PersistCandidates: %v", err)
	}

	var wordCount int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&wordCount); err != nil {
		t.Fatal(err)
	}
	if wordCount != 1 {
		t.Fatalf("empty reading should not create word: got %d words", wordCount)
	}
	var cardCount int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM cards`).Scan(&cardCount); err != nil {
		t.Fatal(err)
	}
	if cardCount != 1 {
		t.Fatalf("empty reading should not create card: got %d", cardCount)
	}
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path, migrationsDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

func seedUser(t *testing.T, d *db.DB) {
	t.Helper()
	_, err := d.SQL().Exec(
		`INSERT INTO users (id, password_hash, created_at) VALUES (1, 'x', ?)`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
}
