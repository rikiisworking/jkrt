package db_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/schedule"
)

func TestLibraryCountsEmpty(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	c, err := d.LibraryCounts(db.LearnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Articles != 0 || c.Words != 0 || c.Cards != 0 || c.Reviews != 0 {
		t.Fatalf("empty: %+v", c)
	}
	if c.ByPhase["new"] != 0 {
		t.Fatalf("by phase: %+v", c.ByPhase)
	}
}

func TestLibraryCountsAfterIngest(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}
	c, err := d.LibraryCounts(db.LearnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Articles != 1 || c.Words < 1 || c.Cards < 1 || c.Sentences < 1 {
		t.Fatalf("counts: %+v", c)
	}
	if c.ByPhase["new"] != c.Cards {
		t.Fatalf("all cards should be new: phase=%+v cards=%d", c.ByPhase, c.Cards)
	}
	if c.MatureCards != 0 {
		t.Fatalf("mature: %d", c.MatureCards)
	}
}

func TestLibraryCountsMature(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済。", a, now); err != nil {
		t.Fatal(err)
	}
	_, err = d.SQL().Exec(
		`UPDATE cards SET phase = 'review', interval_days = 30, due_at = ? WHERE user_id = 1`,
		now.Add(24*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}
	c, err := d.LibraryCounts(db.LearnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if c.MatureCards < 1 {
		t.Fatalf("want mature cards, got %+v", c)
	}
	if c.ByPhase["review"] < 1 {
		t.Fatalf("phase review: %+v", c.ByPhase)
	}
}

func TestTruncateRawText(t *testing.T) {
	s, trunc := db.TruncateRawText("短い")
	if trunc || s != "短い" {
		t.Fatalf("short: %q trunc=%v", s, trunc)
	}
	// Build a string longer than MaxRawTextRunes
	long := strings.Repeat("あ", db.MaxRawTextRunes+10)
	out, trunc := db.TruncateRawText(long)
	if !trunc {
		t.Fatal("expected truncation")
	}
	if utf8.RuneCountInString(out) != db.MaxRawTextRunes {
		t.Fatalf("runes: %d", utf8.RuneCountInString(out))
	}
}

func TestIngestTruncatesHugeRawText(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	// Huge body with a known sentence at the start so analyze still works.
	raw := "経済政策を発表した。" + strings.Repeat("あ", db.MaxRawTextRunes)
	src := db.SourceRef{Name: "nhk_main", FeedURL: "http://example.test"}
	res, err := d.IngestArticle(db.LearnerUserID, src, db.ArticleInput{
		ExternalID: "huge-1",
		Title:      "t",
		URL:        "http://x",
		RawText:    raw,
		FetchedAt:  now,
	}, a, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != db.IngestCreated {
		t.Fatal("want created")
	}
	var stored string
	if err := d.SQL().QueryRow(`SELECT raw_text FROM articles WHERE id = ?`, res.ArticleID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(stored) > db.MaxRawTextRunes {
		t.Fatalf("stored runes %d > max", utf8.RuneCountInString(stored))
	}
}

func TestPerfMigrationIndexesApplied(t *testing.T) {
	d := openTestDB(t)
	// 002_perf.sql should record indexes (or at least migration name).
	var n int
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM schema_migrations WHERE name = '002_perf.sql'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("002_perf.sql not applied: count=%d", n)
	}
	// Spot-check one index exists.
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = 'idx_reviews_user_reviewed'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("idx_reviews_user_reviewed missing")
	}
}

func TestLibraryCountsNilDB(t *testing.T) {
	var d *db.DB
	if _, err := d.LibraryCounts(1); err == nil {
		t.Fatal("expected error")
	}
}

// Mixed phases and reviews count correctly (not only all-new after ingest).
func TestLibraryCountsMixedPhasesAndReviews(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}
	// Split cards across phases.
	_, _ = d.SQL().Exec(`UPDATE cards SET phase = 'learning', learning_step = 0 WHERE id = (SELECT id FROM cards ORDER BY id LIMIT 1)`)
	_, _ = d.SQL().Exec(
		`UPDATE cards SET phase = 'relearning', learning_step = 0 WHERE id = (SELECT id FROM cards ORDER BY id LIMIT 1 OFFSET 1)`,
	)
	// Insert a review row manually for count.
	var cardID int64
	_ = d.SQL().QueryRow(`SELECT id FROM cards ORDER BY id LIMIT 1`).Scan(&cardID)
	var sid int64
	_ = d.SQL().QueryRow(`SELECT id FROM sentences LIMIT 1`).Scan(&sid)
	_, err = d.SQL().Exec(
		`INSERT INTO reviews (user_id, card_id, sentence_id, grade, reviewed_at) VALUES (1, ?, ?, 'good', ?)`,
		cardID, sid, now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}

	c, err := d.LibraryCounts(db.LearnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Reviews != 1 {
		t.Fatalf("reviews: %d", c.Reviews)
	}
	if c.ByPhase["learning"] < 1 {
		t.Fatalf("learning: %+v", c.ByPhase)
	}
	if c.ByPhase["relearning"] < 1 {
		t.Fatalf("relearning: %+v", c.ByPhase)
	}
	sumPhase := 0
	for _, n := range c.ByPhase {
		sumPhase += n
	}
	if sumPhase != c.Cards {
		t.Fatalf("phase sum %d != cards %d (%+v)", sumPhase, c.Cards, c.ByPhase)
	}
}

func TestTruncateRawTextExactBoundary(t *testing.T) {
	exact := strings.Repeat("漢", db.MaxRawTextRunes)
	out, trunc := db.TruncateRawText(exact)
	if trunc {
		t.Fatal("exact max should not truncate")
	}
	if out != exact {
		t.Fatal("exact max mutated")
	}
}

// Mature threshold follows SetScheduleParams (not a forked constant).
func TestLibraryCountsMatureUsesScheduleParams(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済。", a, now); err != nil {
		t.Fatal(err)
	}
	// interval 15: mature under threshold 10, not under default 21
	_, err = d.SQL().Exec(
		`UPDATE cards SET phase = 'review', interval_days = 15, due_at = ? WHERE user_id = 1`,
		now.Add(24*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}

	p := schedule.DefaultParams()
	p.ComfortableIntervalDays = 10
	d.SetScheduleParams(p)
	c, err := d.LibraryCounts(db.LearnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if c.MatureCards < 1 {
		t.Fatalf("want mature at threshold 10, got %+v", c)
	}

	p.ComfortableIntervalDays = 21
	d.SetScheduleParams(p)
	c, err = d.LibraryCounts(db.LearnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if c.MatureCards != 0 {
		t.Fatalf("interval 15 should not be mature at threshold 21: %+v", c)
	}
}
