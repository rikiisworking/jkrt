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
}
