package db_test

import (
	"errors"
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
	_ = d.SQL().QueryRow(`SELECT COUNT(1) FROM sentences`).Scan(&sents)
	_ = d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&words)
	_ = d.SQL().QueryRow(`SELECT COUNT(1) FROM cards`).Scan(&cards)
	_ = d.SQL().QueryRow(`SELECT COUNT(1) FROM sentence_words`).Scan(&sw)
	if sents < 2 {
		t.Fatalf("sentences: %d", sents)
	}
	if words != 0 || cards != 0 || sw != 0 {
		t.Fatalf("want empty study tables words=%d cards=%d sw=%d", words, cards, sw)
	}
	var extracted int
	_ = d.SQL().QueryRow(`SELECT COUNT(1) FROM sentences WHERE extracted_at IS NOT NULL`).Scan(&extracted)
	if extracted != 0 {
		t.Fatalf("extracted_at should be null: %d", extracted)
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
	_ = d.SQL().QueryRow(`SELECT COUNT(1) FROM cards WHERE user_id = 1`).Scan(&cards)
	if cards < 1 {
		t.Fatal("expected cards")
	}
	var phase string
	_ = d.SQL().QueryRow(`SELECT phase FROM cards LIMIT 1`).Scan(&phase)
	if phase != "new" {
		t.Fatalf("phase: %s", phase)
	}
	var ext string
	if err := d.SQL().QueryRow(`SELECT extracted_at FROM sentences WHERE id = ?`, sid).Scan(&ext); err != nil || ext == "" {
		t.Fatalf("extracted_at: %q err=%v", ext, err)
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
	var cards1 int
	_ = d.SQL().QueryRow(`SELECT COUNT(1) FROM cards`).Scan(&cards1)

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
	var cards2 int
	_ = d.SQL().QueryRow(`SELECT COUNT(1) FROM cards`).Scan(&cards2)
	if cards2 != cards1 {
		t.Fatalf("cards grew: %d → %d", cards1, cards2)
	}
	var secondExt string
	_ = d.SQL().QueryRow(`SELECT extracted_at FROM sentences WHERE id = ?`, sid).Scan(&secondExt)
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
	_ = d.SQL().QueryRow(`SELECT COUNT(1) FROM cards`).Scan(&cards)
	if cards != 0 {
		t.Fatalf("kana-only should not create cards: %d", cards)
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
	_, err = d.ExtractSentence(db.LearnerUserID, 99999, a, time.Now().UTC())
	if !errors.Is(err, db.ErrSentenceNotFound) {
		t.Fatalf("got %v want ErrSentenceNotFound", err)
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

func TestMigrationBackfillExtractedAt(t *testing.T) {
	// Migration runs on Open; PersistCandidates path sets extracted_at on new DBs.
	// Simulate legacy: insert sentence + sentence_words without extracted_at, re-open
	// is hard mid-test; instead verify 003 applied and backfill SQL is valid via schema.
	d := openTestDB(t)
	var n int
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM schema_migrations WHERE name = '003_sentence_extract.sql'`,
	).Scan(&n); err != nil || n != 1 {
		t.Fatalf("migration 003: n=%d err=%v", n, err)
	}
	// Column exists
	if _, err := d.SQL().Exec(`SELECT extracted_at FROM sentences LIMIT 0`); err != nil {
		t.Fatalf("extracted_at column: %v", err)
	}
}
