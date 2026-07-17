package db_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
)

func TestSchemaAllTablesPresent(t *testing.T) {
	d := openTestDB(t)
	want := []string{
		"users", "news_sources", "articles", "sentences",
		"words", "sentence_words", "cards", "reviews",
	}
	for _, table := range want {
		var n int
		err := d.SQL().QueryRow(
			`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("missing table %s", table)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idem.db")
	mig := migrationsDir(t)

	d1, err := db.Open(path, mig)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	// Re-open same file: CREATE IF NOT EXISTS must succeed.
	d2, err := db.Open(path, mig)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { _ = d2.Close() })

	var n int
	if err := d2.SQL().QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='cards'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("cards missing after re-migrate")
	}
}

func TestOpenMissingMigrationsDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	missing := filepath.Join(t.TempDir(), "no-such-migrations")
	if _, err := db.Open(path, missing); err == nil {
		t.Fatal("expected error for missing migrations dir")
	}
}

func TestOpenEmptyMigrationsDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	empty := t.TempDir()
	if _, err := db.Open(path, empty); err == nil {
		t.Fatal("expected error when migrations dir has no .sql")
	}
}

func TestUniqueLemmaReadingConstraint(t *testing.T) {
	d := openTestDB(t)
	_, err := d.SQL().Exec(`INSERT INTO words (lemma, reading) VALUES ('経済', 'ケイザイ')`)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err = d.SQL().Exec(`INSERT INTO words (lemma, reading) VALUES ('経済', 'ケイザイ')`)
	if err == nil {
		t.Fatal("expected UNIQUE(lemma, reading) violation")
	}
	// Different reading for same lemma is allowed (homograph identity split).
	_, err = d.SQL().Exec(`INSERT INTO words (lemma, reading) VALUES ('経済', 'ケザイ')`)
	if err != nil {
		t.Fatalf("different reading should be ok: %v", err)
	}
}

func TestUniqueCardUserWordConstraint(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := d.SQL().Exec(`INSERT INTO words (lemma, reading) VALUES ('漢', 'カン')`)
	if err != nil {
		t.Fatal(err)
	}
	wordID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	insertCard := func() error {
		_, err := d.SQL().Exec(
			`INSERT INTO cards (
				user_id, word_id, phase, learning_step, interval_days, ease,
				due_at, reps, lapses, created_at, updated_at
			) VALUES (1, ?, 'new', 0, 0, 2.5, ?, 0, 0, ?, ?)`,
			wordID, now, now, now,
		)
		return err
	}
	if err := insertCard(); err != nil {
		t.Fatalf("first card: %v", err)
	}
	if err := insertCard(); err == nil {
		t.Fatal("expected UNIQUE(user_id, word_id) violation")
	}
}

func TestCardNotOverwrittenOnReExtract(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	if _, err := d.ProcessText(db.LearnerUserID, "経済を発表した。", a, now); err != nil {
		t.Fatalf("ProcessText: %v", err)
	}

	// Simulate learner progress: graduate a card out of new.
	res, err := d.SQL().Exec(
		`UPDATE cards SET phase = 'review', interval_days = 1, ease = 2.6, reps = 1,
		 learning_step = 0, due_at = ?, updated_at = ?
		 WHERE user_id = 1 AND phase = 'new'`,
		now.Add(24*time.Hour).Format(time.RFC3339),
		now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		t.Fatal("expected to update at least one card")
	}

	var reviewBefore int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM cards WHERE phase = 'review'`).Scan(&reviewBefore); err != nil {
		t.Fatal(err)
	}

	// Re-extract same content — must not reset progressed cards to new.
	if _, err := d.ProcessText(db.LearnerUserID, "経済を発表した。", a, now.Add(time.Minute)); err != nil {
		t.Fatalf("re ProcessText: %v", err)
	}

	var reviewAfter int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM cards WHERE phase = 'review'`).Scan(&reviewAfter); err != nil {
		t.Fatal(err)
	}
	if reviewAfter != reviewBefore {
		t.Fatalf("review cards clobbered: before=%d after=%d", reviewBefore, reviewAfter)
	}

	var badEase int
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM cards WHERE phase = 'review' AND ease = 2.6`,
	).Scan(&badEase); err != nil {
		t.Fatal(err)
	}
	if badEase != reviewBefore {
		t.Fatalf("ease reset on re-extract: kept ease=2.6 count=%d want %d", badEase, reviewBefore)
	}
}

func TestProcessTextMultiSentence(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	text := "経済政策を発表した。市場は反応した。"

	articleID, err := d.ProcessText(db.LearnerUserID, text, a, now)
	if err != nil {
		t.Fatalf("ProcessText: %v", err)
	}

	var sentCount int
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM sentences WHERE article_id = ?`, articleID,
	).Scan(&sentCount); err != nil {
		t.Fatal(err)
	}
	if sentCount != 2 {
		t.Fatalf("sentences: got %d want 2", sentCount)
	}

	// order_index 0 then 1
	var o0, o1 int
	var t0, t1 string
	rows, err := d.SQL().Query(
		`SELECT order_index, text FROM sentences WHERE article_id = ? ORDER BY order_index`,
		articleID,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("missing sentence 0")
	}
	if err := rows.Scan(&o0, &t0); err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("missing sentence 1")
	}
	if err := rows.Scan(&o1, &t1); err != nil {
		t.Fatal(err)
	}
	if o0 != 0 || o1 != 1 {
		t.Fatalf("order_index: %d %d", o0, o1)
	}
	if !strings.Contains(t0, "経済") || !strings.Contains(t1, "市場") {
		t.Fatalf("sentence texts: %q %q", t0, t1)
	}

	var wordCount int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&wordCount); err != nil {
		t.Fatal(err)
	}
	if wordCount < 4 {
		// 経済 政策 発表 市場 反応 at minimum-ish
		t.Fatalf("expected several words across two sentences, got %d", wordCount)
	}
}

func TestProcessTextEmptyString(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	articleID, err := d.ProcessText(db.LearnerUserID, "   ", a, now)
	if err != nil {
		t.Fatalf("ProcessText blank: %v", err)
	}
	var sentCount int
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM sentences WHERE article_id = ?`, articleID,
	).Scan(&sentCount); err != nil {
		t.Fatal(err)
	}
	if sentCount != 0 {
		t.Fatalf("blank text should yield 0 sentences, got %d", sentCount)
	}
	var words int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&words); err != nil {
		t.Fatal(err)
	}
	if words != 0 {
		t.Fatalf("expected 0 words, got %d", words)
	}
}

func TestPersistCandidatesValidation(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	if err := d.PersistCandidates(0, 1, nil, now); err == nil {
		t.Fatal("expected error for userID=0")
	}
	if err := d.PersistCandidates(1, 0, nil, now); err == nil {
		t.Fatal("expected error for sentenceID=0")
	}

	var nilDB *db.DB
	if err := nilDB.PersistCandidates(1, 1, nil, now); err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestExtractSentenceNilAnalyzer(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sourceID, err := d.EnsureSource("t", "u", "")
	if err != nil {
		t.Fatal(err)
	}
	aid, err := d.InsertArticle(sourceID, "e", "t", "", "x", now)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := d.InsertSentence(aid, "経済", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ExtractSentence(db.LearnerUserID, sid, "経済", nil, now); err == nil {
		t.Fatal("expected nil analyzer error")
	}
	if _, err := d.ProcessText(db.LearnerUserID, "経済", nil, now); err == nil {
		t.Fatal("expected nil analyzer error from ProcessText")
	}
}

func TestEmptyLemmaSkippedOnPersist(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sid := seedSentence(t, d, now, "x")

	cands := []analyze.Candidate{
		{Lemma: "", Reading: "ケイザイ", Surface: "経済", CharStart: 0, CharEnd: 2},
		{Lemma: "政策", Reading: "セイサク", Surface: "政策", CharStart: 2, CharEnd: 4},
	}
	if err := d.PersistCandidates(db.LearnerUserID, sid, cands, now); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("empty lemma must skip: words=%d", n)
	}
}

func TestSentenceWordSpansStored(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sentence := "経済政策を発表した。"
	if _, err := d.ProcessText(db.LearnerUserID, sentence, a, now); err != nil {
		t.Fatal(err)
	}

	rows, err := d.SQL().Query(
		`SELECT sw.surface, sw.char_start, sw.char_end, w.lemma, w.reading
		 FROM sentence_words sw JOIN words w ON w.id = sw.word_id
		 ORDER BY sw.char_start`,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	runes := []rune(sentence)
	count := 0
	for rows.Next() {
		var surface, lemma, reading string
		var start, end int
		if err := rows.Scan(&surface, &start, &end, &lemma, &reading); err != nil {
			t.Fatal(err)
		}
		if start < 0 || end > len(runes) || start >= end {
			t.Fatalf("bad stored span %d:%d for %s", start, end, surface)
		}
		if string(runes[start:end]) != surface {
			t.Fatalf("stored span %q != surface %q", string(runes[start:end]), surface)
		}
		if reading == "" {
			t.Fatalf("empty reading stored for %s", lemma)
		}
		count++
	}
	if count < 3 {
		t.Fatalf("expected ≥3 sentence_words, got %d", count)
	}
}

func TestEnsureSourceIdempotent(t *testing.T) {
	d := openTestDB(t)
	id1, err := d.EnsureSource("nhk_main", "http://example.test/main", "a")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := d.EnsureSource("nhk_main", "http://other", "b")
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("EnsureSource not idempotent: %d vs %d", id1, id2)
	}
	var n int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM news_sources WHERE name = 'nhk_main'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("duplicate sources: %d", n)
	}
}

func TestArticleUniqueExternalID(t *testing.T) {
	d := openTestDB(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sid, err := d.EnsureSource("s", "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.InsertArticle(sid, "guid-1", "t", "http://x", "body", now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.InsertArticle(sid, "guid-1", "t2", "http://y", "body2", now); err == nil {
		t.Fatal("expected UNIQUE(source_id, external_id) violation")
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	d := openTestDB(t)
	// sentence without article
	_, err := d.SQL().Exec(
		`INSERT INTO sentences (article_id, text, order_index) VALUES (99999, 'x', 0)`,
	)
	if err == nil {
		t.Fatal("expected FK failure for sentences.article_id")
	}
	// card without user
	_, err = d.SQL().Exec(
		`INSERT INTO words (lemma, reading) VALUES ('漢', 'カン')`,
	)
	if err != nil {
		t.Fatal(err)
	}
	var wordID int64
	if err := d.SQL().QueryRow(`SELECT id FROM words WHERE lemma = '漢'`).Scan(&wordID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.SQL().Exec(
		`INSERT INTO cards (
			user_id, word_id, phase, learning_step, interval_days, ease,
			due_at, reps, lapses, created_at, updated_at
		) VALUES (1, ?, 'new', 0, 0, 2.5, ?, 0, 0, ?, ?)`,
		wordID, now, now, now,
	)
	if err == nil {
		t.Fatal("expected FK failure for cards.user_id when user missing")
	}
}

func TestUniqueLemmaReadingExactWordCountStable(t *testing.T) {
	// Stronger than distinct-count: exact word count must not grow on re-extract.
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sentence := "経済政策を発表した。"

	if _, err := d.ProcessText(db.LearnerUserID, sentence, a, now); err != nil {
		t.Fatal(err)
	}
	var first int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if first != 3 {
		t.Fatalf("fixture should yield exactly 3 words, got %d", first)
	}

	for i := 0; i < 3; i++ {
		if _, err := d.ProcessText(db.LearnerUserID, sentence, a, now.Add(time.Duration(i+1)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	var after int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != first {
		t.Fatalf("word count grew on re-extract: %d → %d", first, after)
	}
	var cards int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM cards`).Scan(&cards); err != nil {
		t.Fatal(err)
	}
	if cards != first {
		t.Fatalf("card count %d != word count %d", cards, first)
	}
}

func TestExtractFixtureExactWords(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.ProcessText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"経済": "ケイザイ",
		"政策": "セイサク",
		"発表": "ハッピョウ",
	}
	rows, err := d.SQL().Query(`SELECT lemma, reading FROM words ORDER BY lemma`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var lemma, reading string
		if err := rows.Scan(&lemma, &reading); err != nil {
			t.Fatal(err)
		}
		got[lemma] = reading
	}
	if len(got) != len(want) {
		t.Fatalf("words got %v want %v", got, want)
	}
	for lemma, reading := range want {
		if got[lemma] != reading {
			t.Errorf("%s: got %q want %q", lemma, got[lemma], reading)
		}
	}
}

func TestIsUnfamiliarBoundaryAndUnknownPhase(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Second)

	// interval just under 21 → unfamiliar; exactly 21 and not due → familiar
	if !db.IsUnfamiliar("review", 20.999, future, now) {
		t.Fatal("interval < 21 should be unfamiliar")
	}
	if db.IsUnfamiliar("review", 21.0, future, now) {
		t.Fatal("interval == 21 and not due should be familiar")
	}
	// due wins even when mature
	if !db.IsUnfamiliar("review", 100, past, now) {
		t.Fatal("due mature card should be unfamiliar")
	}
	// unknown phase, not due, no young-review branch
	if db.IsUnfamiliar("graduated", 100, future, now) {
		t.Fatal("unknown phase not due should be familiar")
	}
	if !db.IsUnfamiliar("graduated", 100, past, now) {
		t.Fatal("unknown phase due should still be unfamiliar via due_at")
	}
	// learning always unfamiliar even if due far future and huge interval
	if !db.IsUnfamiliar("learning", 999, future, now) {
		t.Fatal("learning always unfamiliar")
	}
}

func TestFindMigrationsDirFromPackageCwd(t *testing.T) {
	// OpenStore / empty migrationsDir rely on walking up from cwd.
	// When tests run as `go test ./internal/db`, cwd is this package dir.
	dir, err := db.FindMigrationsDir()
	if err != nil {
		t.Fatalf("FindMigrationsDir: %v", err)
	}
	if !strings.HasSuffix(filepath.Clean(dir), "migrations") {
		t.Fatalf("unexpected migrations dir: %s", dir)
	}
	// Open with empty migrationsDir should work from package cwd.
	path := filepath.Join(t.TempDir(), "auto.db")
	d, err := db.Open(path, "")
	if err != nil {
		t.Fatalf("Open with empty migrationsDir: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if d.SQL() == nil {
		t.Fatal("nil sql")
	}
}

func seedSentence(t *testing.T, d *db.DB, now time.Time, text string) int64 {
	t.Helper()
	sourceID, err := d.EnsureSource("seed", "http://example.test", "")
	if err != nil {
		t.Fatal(err)
	}
	articleID, err := d.InsertArticle(sourceID, "seed-"+text, "t", "", text, now)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := d.InsertSentence(articleID, text, 0)
	if err != nil {
		t.Fatal(err)
	}
	return sid
}
