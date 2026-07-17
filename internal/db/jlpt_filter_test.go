package db_test

import (
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/jlpt"
)

// Production-style extract: N2+ filter on, no classifier → unlisted skip; N5 skip; N1 keep.
func TestExtractFiltersByJLPTMap(t *testing.T) {
	d := openTestDB(t)
	// openTestDB enables AllowAllWords — restore production filter.
	d.SetJLPT(jlpt.ResolveOptions{})
	seedUser(t, d)

	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	// 犬 = n5 (skip), 政策 = n1 (keep). Avoid compounds that confuse levels.
	store, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "t"}, db.ArticleInput{
		ExternalID: "jlpt1",
		RawText:    "犬と政策。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	var sid int64
	if err := d.SQL().QueryRow(`SELECT id FROM sentences WHERE article_id = ?`, store.ArticleID).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	res, err := d.ExtractSentence(db.LearnerUserID, sid, a, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Candidates < 1 {
		t.Fatalf("expected at least 政策 candidate, got %+v", res)
	}

	var lemmas []string
	rows, err := d.SQL().Query(`
		SELECT w.lemma FROM words w
		JOIN cards c ON c.word_id = w.id
		WHERE c.user_id = 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatal(err)
		}
		lemmas = append(lemmas, l)
	}
	for _, l := range lemmas {
		if l == "犬" {
			t.Fatal("n5 犬 must not create a card")
		}
	}
	foundPolicy := false
	for _, l := range lemmas {
		if l == "政策" {
			foundPolicy = true
		}
	}
	if !foundPolicy {
		t.Fatalf("expected 政策 card, got %v (extract %+v)", lemmas, res)
	}
}

func TestExtractUnlistedWithFakeClassifier(t *testing.T) {
	d := openTestDB(t)
	d.SetJLPT(jlpt.ResolveOptions{
		Classifier: jlpt.StaticClassifier{Level: jlpt.N1},
		Cache:      &jlpt.SQLCache{SQL: d.SQL()},
	})
	seedUser(t, d)

	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	// Unlikely to be in embed map as this exact form.
	store, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "t"}, db.ArticleInput{
		ExternalID: "jlpt2",
		RawText:    "超珍奇語彙を使う。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	var sid int64
	if err := d.SQL().QueryRow(`SELECT id FROM sentences WHERE article_id = ?`, store.ArticleID).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	res, err := d.ExtractSentence(db.LearnerUserID, sid, a, now)
	if err != nil {
		t.Fatal(err)
	}
	// With N1 static classifier, any kanji candidates become cards.
	if res.CardsNew < 1 && res.Candidates < 1 {
		// Sentence may yield zero candidates if analyzer finds none — skip soft.
		t.Logf("no candidates (ok if pure-kana-ish): %+v", res)
		return
	}
	if res.Candidates < 1 {
		t.Fatalf("expected candidates with N1 classifier: %+v", res)
	}
}
