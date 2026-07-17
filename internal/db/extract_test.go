package db_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/review"
	"github.com/rikiisworking/jkrt/internal/schedule"
)

func TestStoreArticleCreatesSentencesNoCards(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	res, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "nhk_main", FeedURL: "http://x"}, db.ArticleInput{
		ExternalID: "g1",
		Title:      "t",
		RawText:    "経済政策を発表した。市場は反応した。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != db.IngestCreated {
		t.Fatalf("status: %v", res.Status)
	}
	var sents, words, cards, sw int
	mustCount(t, d, `SELECT COUNT(1) FROM sentences`, &sents)
	mustCount(t, d, `SELECT COUNT(1) FROM words`, &words)
	mustCount(t, d, `SELECT COUNT(1) FROM cards`, &cards)
	mustCount(t, d, `SELECT COUNT(1) FROM sentence_words`, &sw)
	if sents < 2 {
		t.Fatalf("sentences: %d", sents)
	}
	if words != 0 || cards != 0 || sw != 0 {
		t.Fatalf("want empty study tables words=%d cards=%d sw=%d", words, cards, sw)
	}
	var extracted int
	mustCount(t, d, `SELECT COUNT(1) FROM sentences WHERE extracted_at IS NOT NULL AND extracted_at != ''`, &extracted)
	if extracted != 0 {
		t.Fatalf("extracted_at should be null: %d", extracted)
	}
}

func TestStoreArticleDedupeNoExtraSentences(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	src := db.SourceRef{Name: "src", FeedURL: "http://x"}
	first, err := d.StoreArticle(db.LearnerUserID, src, db.ArticleInput{
		ExternalID: "same",
		RawText:    "経済。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.StoreArticle(db.LearnerUserID, src, db.ArticleInput{
		ExternalID: "same",
		RawText:    "政策。政策。政策。",
		FetchedAt:  now.Add(time.Minute),
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != db.IngestExists || second.ArticleID != first.ArticleID {
		t.Fatalf("second: %+v first=%d", second, first.ArticleID)
	}
	var sents int
	mustCount(t, d, `SELECT COUNT(1) FROM sentences`, &sents)
	if sents != 1 {
		t.Fatalf("dedupe must not add sentences: %d", sents)
	}
}

func TestStoreArticleValidation(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.StoreArticle(0, db.SourceRef{Name: "s"}, db.ArticleInput{ExternalID: "e", RawText: "x"}, now); err == nil {
		t.Fatal("expected error for userID 0")
	}
	if _, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{}, db.ArticleInput{ExternalID: "e", RawText: "x"}, now); err == nil {
		t.Fatal("expected error for empty source name")
	}
	if _, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "s"}, db.ArticleInput{RawText: "x"}, now); err == nil {
		t.Fatal("expected error for empty external_id")
	}
	var nilDB *db.DB
	if _, err := nilDB.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "s"}, db.ArticleInput{ExternalID: "e", RawText: "x"}, now); err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestExtractSentenceCreatesWordsAndCards(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "s"}, db.ArticleInput{
		ExternalID: "e1",
		RawText:    "経済政策を発表した。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	var sid int64
	if err := d.SQL().QueryRow(`SELECT id FROM sentences WHERE article_id = ?`, store.ArticleID).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	er, err := d.ExtractSentence(db.LearnerUserID, sid, a, now)
	if err != nil {
		t.Fatal(err)
	}
	if er.AlreadyExtracted {
		t.Fatal("first extract should not be already")
	}
	if er.Candidates < 1 {
		t.Fatalf("candidates: %d", er.Candidates)
	}
	if er.CardsNew < 1 {
		t.Fatalf("cards_new: %d", er.CardsNew)
	}
	var cards int
	mustCount(t, d, `SELECT COUNT(1) FROM cards WHERE user_id = 1`, &cards)
	if cards < 1 {
		t.Fatal("expected cards")
	}
	if er.CardsNew != cards {
		t.Fatalf("cards_new %d != cards %d on empty deck", er.CardsNew, cards)
	}
	if er.SentenceID != sid || er.ArticleID != store.ArticleID {
		t.Fatalf("ids: %+v want sid=%d art=%d", er, sid, store.ArticleID)
	}
	var phase string
	if err := d.SQL().QueryRow(`SELECT phase FROM cards LIMIT 1`).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if phase != "new" {
		t.Fatalf("phase: %s", phase)
	}
	var ext string
	if err := d.SQL().QueryRow(`SELECT extracted_at FROM sentences WHERE id = ?`, sid).Scan(&ext); err != nil || ext == "" {
		t.Fatalf("extracted_at: %q err=%v", ext, err)
	}
	var sw int
	mustCount(t, d, `SELECT COUNT(1) FROM sentence_words WHERE sentence_id = ?`, &sw, sid)
	if sw < 1 {
		t.Fatal("expected sentence_words")
	}
}

func TestExtractSentenceIdempotent(t *testing.T) {
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
	var sid int64
	var firstExt string
	if err := d.SQL().QueryRow(`SELECT id, extracted_at FROM sentences LIMIT 1`).Scan(&sid, &firstExt); err != nil {
		t.Fatal(err)
	}
	var cards1, sw1 int
	mustCount(t, d, `SELECT COUNT(1) FROM cards`, &cards1)
	mustCount(t, d, `SELECT COUNT(1) FROM sentence_words WHERE sentence_id = ?`, &sw1, sid)

	er, err := d.ExtractSentence(db.LearnerUserID, sid, a, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !er.AlreadyExtracted {
		t.Fatal("want already extracted")
	}
	if er.CardsNew != 0 {
		t.Fatalf("cards_new on re-extract: %d", er.CardsNew)
	}
	if er.Candidates != sw1 {
		// Noop re-tap reports existing sentence_words count as Candidates.
		t.Fatalf("candidates on re-tap: got %d want %d (sentence_words)", er.Candidates, sw1)
	}
	var cards2, sw2 int
	mustCount(t, d, `SELECT COUNT(1) FROM cards`, &cards2)
	mustCount(t, d, `SELECT COUNT(1) FROM sentence_words WHERE sentence_id = ?`, &sw2, sid)
	if cards2 != cards1 {
		t.Fatalf("cards grew: %d → %d", cards1, cards2)
	}
	if sw2 != sw1 {
		t.Fatalf("sentence_words count changed: %d → %d", sw1, sw2)
	}
	var secondExt string
	if err := d.SQL().QueryRow(`SELECT extracted_at FROM sentences WHERE id = ?`, sid).Scan(&secondExt); err != nil {
		t.Fatal(err)
	}
	if secondExt != firstExt {
		t.Fatalf("extracted_at changed: %q → %q", firstExt, secondExt)
	}
}

func TestExtractSentencePreservesExistingCardSchedule(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済を見る。", a, now); err != nil {
		t.Fatal(err)
	}
	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	if err := svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", res.Item.UpdatedAt, now); err != nil {
		t.Fatal(err)
	}
	var phase1, due1 string
	var ease1 float64
	if err := d.SQL().QueryRow(
		`SELECT phase, due_at, ease FROM cards WHERE id = ?`, res.Item.CardID,
	).Scan(&phase1, &due1, &ease1); err != nil {
		t.Fatal(err)
	}

	// Second article with same word → extract should not reset graded card.
	store, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "s2"}, db.ArticleInput{
		ExternalID: "e2",
		RawText:    "経済は重要だ。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	var sid2 int64
	if err := d.SQL().QueryRow(`SELECT id FROM sentences WHERE article_id = ?`, store.ArticleID).Scan(&sid2); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExtractSentence(db.LearnerUserID, sid2, a, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var phase2, due2 string
	var ease2 float64
	if err := d.SQL().QueryRow(
		`SELECT phase, due_at, ease FROM cards WHERE id = ?`, res.Item.CardID,
	).Scan(&phase2, &due2, &ease2); err != nil {
		t.Fatal(err)
	}
	if phase2 != phase1 || due2 != due1 || ease2 != ease1 {
		t.Fatalf("schedule reset: before (%s %s %v) after (%s %s %v)", phase1, due1, ease1, phase2, due2, ease2)
	}
}

func TestExtractSentenceEmptyOrKanaOnly(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "s"}, db.ArticleInput{
		ExternalID: "kana",
		RawText:    "あいうえお。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	var sid int64
	if err := d.SQL().QueryRow(`SELECT id FROM sentences WHERE article_id = ?`, store.ArticleID).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	er, err := d.ExtractSentence(db.LearnerUserID, sid, a, now)
	if err != nil {
		t.Fatal(err)
	}
	if er.AlreadyExtracted {
		t.Fatal("first extract")
	}
	var cards int
	mustCount(t, d, `SELECT COUNT(1) FROM cards`, &cards)
	if cards != 0 {
		t.Fatalf("kana-only should not create cards: %d", cards)
	}
	if er.Candidates != 0 {
		t.Fatalf("candidates: %d want 0", er.Candidates)
	}
	var ext string
	if err := d.SQL().QueryRow(`SELECT extracted_at FROM sentences WHERE id = ?`, sid).Scan(&ext); err != nil || ext == "" {
		t.Fatalf("must still mark extracted: %q %v", ext, err)
	}
}

func TestExtractSentenceNotFound(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.ExtractSentence(db.LearnerUserID, 99999, a, time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC))
	if !errors.Is(err, db.ErrSentenceNotFound) {
		t.Fatalf("got %v want ErrSentenceNotFound", err)
	}
}

func TestExtractSentenceValidation(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.ExtractSentence(0, 1, a, now); err == nil {
		t.Fatal("expected error for userID 0")
	}
	if _, err := d.ExtractSentence(db.LearnerUserID, 0, a, now); err == nil {
		t.Fatal("expected error for sentenceID 0")
	}
}

func TestExtractSentenceArticleMismatch(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "s"}, db.ArticleInput{
		ExternalID: "e1",
		RawText:    "経済。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	var sid int64
	if err := d.SQL().QueryRow(`SELECT id FROM sentences WHERE article_id = ?`, store.ArticleID).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	_, err = d.ExtractSentenceForArticle(db.LearnerUserID, store.ArticleID+1, sid, a, now)
	if !errors.Is(err, db.ErrArticleMismatch) {
		t.Fatalf("got %v want ErrArticleMismatch", err)
	}
}

func TestMigration003RecordedAndColumnExists(t *testing.T) {
	d := openTestDB(t)
	var n int
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM schema_migrations WHERE name = '003_sentence_extract.sql'`,
	).Scan(&n); err != nil || n != 1 {
		t.Fatalf("migration 003: n=%d err=%v", n, err)
	}
	if _, err := d.SQL().Exec(`SELECT extracted_at FROM sentences LIMIT 0`); err != nil {
		t.Fatalf("extracted_at column: %v", err)
	}
}

// Backfill logic from 003_sentence_extract.sql (same SQL) on legacy-shaped rows.
func TestExtractedAtBackfillSQL(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	nowStr := now.UTC().Format(time.RFC3339)
	srcID, err := d.EnsureSource("legacy", "http://x", "")
	if err != nil {
		t.Fatal(err)
	}
	ar, err := d.SQL().Exec(
		`INSERT INTO articles (source_id, external_id, title, url, fetched_at, raw_text)
		 VALUES (?, 'leg-1', 't', '', ?, '経済。')`, srcID, nowStr,
	)
	if err != nil {
		t.Fatal(err)
	}
	aid, _ := ar.LastInsertId()
	sr, err := d.SQL().Exec(
		`INSERT INTO sentences (article_id, text, order_index, extracted_at) VALUES (?, '経済。', 0, NULL)`,
		aid,
	)
	if err != nil {
		t.Fatal(err)
	}
	sid, _ := sr.LastInsertId()
	wr, err := d.SQL().Exec(`INSERT INTO words (lemma, reading) VALUES ('経済', 'けいざい')`)
	if err != nil {
		t.Fatal(err)
	}
	wid, _ := wr.LastInsertId()
	if _, err := d.SQL().Exec(
		`INSERT INTO sentence_words (sentence_id, word_id, surface, char_start, char_end, created_at)
		 VALUES (?, ?, '経済', 0, 2, ?)`, sid, wid, nowStr,
	); err != nil {
		t.Fatal(err)
	}

	// Same statement as migrations/003_sentence_extract.sql backfill.
	if _, err := d.SQL().Exec(`
		UPDATE sentences SET extracted_at = (
			SELECT MIN(sw.created_at) FROM sentence_words sw WHERE sw.sentence_id = sentences.id
		)
		WHERE EXISTS (SELECT 1 FROM sentence_words sw WHERE sw.sentence_id = sentences.id)
		  AND extracted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	var ext string
	if err := d.SQL().QueryRow(`SELECT extracted_at FROM sentences WHERE id = ?`, sid).Scan(&ext); err != nil {
		t.Fatal(err)
	}
	if ext != nowStr {
		t.Fatalf("backfill extracted_at: got %q want %q", ext, nowStr)
	}
}

func TestExtractAfterStoreDedupeKeepsCards(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	src := db.SourceRef{Name: "nhk_main", FeedURL: "http://x"}
	art := db.ArticleInput{ExternalID: "g1", RawText: "経済政策を発表した。", FetchedAt: now}
	first, err := d.StoreArticle(db.LearnerUserID, src, art, now)
	if err != nil {
		t.Fatal(err)
	}
	var sid int64
	if err := d.SQL().QueryRow(`SELECT id FROM sentences WHERE article_id = ?`, first.ArticleID).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExtractSentence(db.LearnerUserID, sid, a, now); err != nil {
		t.Fatal(err)
	}
	var cards1 int
	mustCount(t, d, `SELECT COUNT(1) FROM cards`, &cards1)
	if cards1 < 1 {
		t.Fatal("need cards after extract")
	}
	// Re-scrape same guid must not wipe library or cards.
	second, err := d.StoreArticle(db.LearnerUserID, src, art, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != db.IngestExists {
		t.Fatalf("status: %v", second.Status)
	}
	var cards2 int
	mustCount(t, d, `SELECT COUNT(1) FROM cards`, &cards2)
	if cards2 != cards1 {
		t.Fatalf("cards after re-store: %d want %d", cards2, cards1)
	}
	var ext string
	if err := d.SQL().QueryRow(`SELECT extracted_at FROM sentences WHERE id = ?`, sid).Scan(&ext); err != nil || ext == "" {
		t.Fatalf("extracted_at lost: %q %v", ext, err)
	}
}

func TestLibraryCountsZeroCardsAfterStoreOnly(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "s"}, db.ArticleInput{
		ExternalID: "e",
		RawText:    "経済政策を発表した。",
		FetchedAt:  now,
	}, now); err != nil {
		t.Fatal(err)
	}
	c, err := d.LibraryCounts(db.LearnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Articles < 1 || c.Sentences < 1 {
		t.Fatalf("library: %+v", c)
	}
	if c.Cards != 0 || c.Words != 0 {
		t.Fatalf("store-only must not create study rows: %+v", c)
	}
}

// ADR 0006: extract is per-sentence; siblings stay library-only until tapped.
func TestExtractOnlySelectedSentenceLeavesSiblingsUnextracted(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	// Two sentences with distinct kanji so sibling-only lemmas stay off the deck.
	store, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "s"}, db.ArticleInput{
		ExternalID: "multi",
		RawText:    "経済政策を発表した。市場は反応した。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	var sid1, sid2 int64
	if err := d.SQL().QueryRow(
		`SELECT id FROM sentences WHERE article_id = ? ORDER BY order_index ASC LIMIT 1`,
		store.ArticleID,
	).Scan(&sid1); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL().QueryRow(
		`SELECT id FROM sentences WHERE article_id = ? ORDER BY order_index ASC LIMIT 1 OFFSET 1`,
		store.ArticleID,
	).Scan(&sid2); err != nil {
		t.Fatal(err)
	}
	if sid1 == 0 || sid2 == 0 || sid1 == sid2 {
		t.Fatalf("need two sentences: %d %d", sid1, sid2)
	}

	er, err := d.ExtractSentence(db.LearnerUserID, sid1, a, now)
	if err != nil {
		t.Fatal(err)
	}
	if er.AlreadyExtracted || er.Candidates < 1 || er.CardsNew < 1 {
		t.Fatalf("first extract: %+v", er)
	}

	var ext1, ext2 sql.NullString
	if err := d.SQL().QueryRow(`SELECT extracted_at FROM sentences WHERE id = ?`, sid1).Scan(&ext1); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL().QueryRow(`SELECT extracted_at FROM sentences WHERE id = ?`, sid2).Scan(&ext2); err != nil {
		t.Fatal(err)
	}
	if !ext1.Valid || strings.TrimSpace(ext1.String) == "" {
		t.Fatalf("sentence 1 must be extracted: %v", ext1)
	}
	if ext2.Valid && strings.TrimSpace(ext2.String) != "" {
		t.Fatalf("sentence 2 must stay unextracted: %q", ext2.String)
	}

	var sw1, sw2 int
	mustCount(t, d, `SELECT COUNT(1) FROM sentence_words WHERE sentence_id = ?`, &sw1, sid1)
	mustCount(t, d, `SELECT COUNT(1) FROM sentence_words WHERE sentence_id = ?`, &sw2, sid2)
	if sw1 < 1 {
		t.Fatal("extracted sentence needs sentence_words")
	}
	if sw2 != 0 {
		t.Fatalf("sibling sentence_words: got %d want 0", sw2)
	}

	// Browse flags: extracted + WordCount only on the tapped sentence.
	_, sents, found, err := d.GetArticle(store.ArticleID)
	if err != nil || !found {
		t.Fatalf("GetArticle: found=%v err=%v", found, err)
	}
	if len(sents) < 2 {
		t.Fatalf("sentences: %d", len(sents))
	}
	var first, second *db.SentenceListItem
	for i := range sents {
		switch sents[i].ID {
		case sid1:
			first = &sents[i]
		case sid2:
			second = &sents[i]
		}
	}
	if first == nil || second == nil {
		t.Fatal("missing sentence rows from GetArticle")
	}
	if !first.Extracted || first.WordCount < 1 {
		t.Fatalf("extracted browse item: %+v", first)
	}
	if second.Extracted || second.WordCount != 0 {
		t.Fatalf("sibling browse item must be library-only: %+v", second)
	}

	// Sibling-only surface (市場) must not have a Card until sentence 2 is extracted.
	var marketCards int
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM cards c
		 JOIN words w ON w.id = c.word_id
		 WHERE c.user_id = ? AND w.lemma = ?`,
		db.LearnerUserID, "市場",
	).Scan(&marketCards); err != nil {
		t.Fatal(err)
	}
	if marketCards != 0 {
		t.Fatalf("sibling lemma 市場 must not be a Card yet: %d", marketCards)
	}

	// After extracting sibling, its words enter the deck.
	if _, err := d.ExtractSentence(db.LearnerUserID, sid2, a, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM cards c
		 JOIN words w ON w.id = c.word_id
		 WHERE c.user_id = ? AND w.lemma = ?`,
		db.LearnerUserID, "市場",
	).Scan(&marketCards); err != nil {
		t.Fatal(err)
	}
	if marketCards != 1 {
		t.Fatalf("after sibling extract, 市場 cards: got %d want 1", marketCards)
	}
	mustCount(t, d, `SELECT COUNT(1) FROM sentence_words WHERE sentence_id = ?`, &sw2, sid2)
	if sw2 < 1 {
		t.Fatal("sibling should have sentence_words after extract")
	}
}

func mustCount(t *testing.T, d *db.DB, q string, dest *int, args ...any) {
	t.Helper()
	if err := d.SQL().QueryRow(q, args...).Scan(dest); err != nil {
		t.Fatalf("count %q: %v", q, err)
	}
}
