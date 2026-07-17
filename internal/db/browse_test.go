package db_test

import (
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
)

func TestBrowseEmpty(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)

	list, err := d.ListArticles(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("list: got %d want 0", len(list))
	}

	_, ok, err := d.LastArticleFetchedAt()
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no last fetch")
	}

	n, err := d.CountArticles()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("count: %d", n)
	}

	_, _, found, err := d.GetArticle(999)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected missing article")
	}
}

func TestBrowseListAndDetail(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	res, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now)
	if err != nil {
		t.Fatal(err)
	}

	n, err := d.CountArticles()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count: %d", n)
	}

	fetched, ok, err := d.LastArticleFetchedAt()
	if err != nil || !ok {
		t.Fatalf("last fetch: ok=%v err=%v", ok, err)
	}
	if fetched == "" {
		t.Fatal("empty fetched_at")
	}

	list, err := d.ListArticles(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len: %d", len(list))
	}
	if list[0].ID != res.ArticleID {
		t.Fatalf("id: got %d want %d", list[0].ID, res.ArticleID)
	}
	if list[0].SourceName != db.ManualSourceName {
		t.Fatalf("source: %q", list[0].SourceName)
	}
	if list[0].SentenceCount < 1 {
		t.Fatalf("sentence count: %d", list[0].SentenceCount)
	}

	art, sents, found, err := d.GetArticle(res.ArticleID)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if art.ID != res.ArticleID {
		t.Fatalf("detail id: %d", art.ID)
	}
	if len(sents) < 1 {
		t.Fatal("expected sentences")
	}
	if sents[0].Text == "" {
		t.Fatal("empty sentence text")
	}
	// IngestText extracts all sentences → Extracted flag set.
	if !sents[0].Extracted || sents[0].ExtractedAt == "" {
		t.Fatalf("expected extracted sentence after IngestText: %+v", sents[0])
	}
}

func TestBrowseExtractedFlagBeforeAndAfterExtract(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "s"}, db.ArticleInput{
		ExternalID: "e",
		RawText:    "経済政策を発表した。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, sents, found, err := d.GetArticle(store.ArticleID)
	if err != nil || !found {
		t.Fatal(err)
	}
	if len(sents) < 1 {
		t.Fatal("need sentence")
	}
	if sents[0].Extracted {
		t.Fatal("store-only sentence must not be extracted")
	}
	if _, err := d.ExtractSentence(db.LearnerUserID, sents[0].ID, a, now); err != nil {
		t.Fatal(err)
	}
	_, sents2, _, err := d.GetArticle(store.ArticleID)
	if err != nil {
		t.Fatal(err)
	}
	if !sents2[0].Extracted || sents2[0].ExtractedAt == "" {
		t.Fatalf("after extract: %+v", sents2[0])
	}
}

// ListArticles(limit<=0) uses DefaultArticleListLimit; newest fetched_at first.
func TestBrowseListDefaultLimitAndOrder(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	t1 := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 17, 14, 0, 0, 0, time.UTC)

	older, err := d.IngestText(db.LearnerUserID, "古い記事です。", a, t1)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := d.IngestText(db.LearnerUserID, "新しい記事です。", a, t2)
	if err != nil {
		t.Fatal(err)
	}

	list, err := d.ListArticles(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 {
		t.Fatalf("list len: %d", len(list))
	}
	if list[0].ID != newer.ArticleID {
		t.Fatalf("want newest first: got %d want %d", list[0].ID, newer.ArticleID)
	}
	if list[1].ID != older.ArticleID {
		t.Fatalf("want older second: got %d want %d", list[1].ID, older.ArticleID)
	}

	limited, err := d.ListArticles(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].ID != newer.ArticleID {
		t.Fatalf("limit 1: %+v", limited)
	}
}

func TestBrowseNilDB(t *testing.T) {
	var d *db.DB
	if _, err := d.ListArticles(10); err == nil {
		t.Fatal("ListArticles nil want error")
	}
	if _, _, _, err := d.GetArticle(1); err == nil {
		t.Fatal("GetArticle nil want error")
	}
	if _, _, err := d.LastArticleFetchedAt(); err == nil {
		t.Fatal("LastArticleFetchedAt nil want error")
	}
	if _, err := d.CountArticles(); err == nil {
		t.Fatal("CountArticles nil want error")
	}
}
