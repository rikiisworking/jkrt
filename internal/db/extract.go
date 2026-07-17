package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
)

// StartingEase is the SM-2 starting ease for new Cards (docs/sm2-spec.md).
const StartingEase = 2.5

// LearnerUserID is the single v1 learner (users.id = 1).
const LearnerUserID int64 = 1

// PersistCandidates upserts Words, inserts sentence_words rows, and creates
// new Cards (phase=new) for the learner. Existing cards are left unchanged.
//
// candidates must already be filtered Word candidates (kanji + non-empty reading).
func (d *DB) PersistCandidates(userID, sentenceID int64, candidates []analyze.Candidate, now time.Time) error {
	if d == nil || d.sql == nil {
		return fmt.Errorf("db is nil")
	}
	if userID == 0 {
		return fmt.Errorf("userID is required")
	}
	if sentenceID == 0 {
		return fmt.Errorf("sentenceID is required")
	}

	tx, err := d.sql.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	nowStr := now.UTC().Format(time.RFC3339)

	for _, c := range candidates {
		if c.Lemma == "" || c.Reading == "" {
			// Defense in depth: empty reading must never create a Word/Card.
			continue
		}

		wordID, err := upsertWordTx(tx, c.Lemma, c.Reading)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(
			`INSERT INTO sentence_words (sentence_id, word_id, surface, char_start, char_end, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			sentenceID, wordID, c.Surface, c.CharStart, c.CharEnd, nowStr,
		); err != nil {
			return fmt.Errorf("insert sentence_word: %w", err)
		}

		if err := upsertNewCardTx(tx, userID, wordID, nowStr); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ExtractSentence analyzes sentence text and persists Words/Cards for that sentence row.
func (d *DB) ExtractSentence(userID, sentenceID int64, text string, a *analyze.Analyzer, now time.Time) error {
	if a == nil {
		return fmt.Errorf("analyzer is nil")
	}
	cands, err := a.Candidates(text)
	if err != nil {
		return err
	}
	return d.PersistCandidates(userID, sentenceID, cands, now)
}

// InsertSentence adds a sentence under an article and returns its id.
func (d *DB) InsertSentence(articleID int64, text string, orderIndex int) (int64, error) {
	res, err := d.sql.Exec(
		`INSERT INTO sentences (article_id, text, order_index) VALUES (?, ?, ?)`,
		articleID, text, orderIndex,
	)
	if err != nil {
		return 0, fmt.Errorf("insert sentence: %w", err)
	}
	return res.LastInsertId()
}

// EnsureSource inserts a news_sources row by name if missing and returns its id.
func (d *DB) EnsureSource(name, feedURL, notes string) (int64, error) {
	var id int64
	err := d.sql.QueryRow(`SELECT id FROM news_sources WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	res, err := d.sql.Exec(
		`INSERT INTO news_sources (name, feed_url, enabled, notes) VALUES (?, ?, 1, ?)`,
		name, feedURL, notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert source: %w", err)
	}
	return res.LastInsertId()
}

// InsertArticle inserts an article row and returns its id.
func (d *DB) InsertArticle(sourceID int64, externalID, title, url, rawText string, fetchedAt time.Time) (int64, error) {
	res, err := d.sql.Exec(
		`INSERT INTO articles (source_id, external_id, title, url, fetched_at, raw_text)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sourceID, externalID, title, url, fetchedAt.UTC().Format(time.RFC3339), rawText,
	)
	if err != nil {
		return 0, fmt.Errorf("insert article: %w", err)
	}
	return res.LastInsertId()
}

// ProcessText is the Phase 1 library path: raw Japanese string → sentences → words/cards.
// It creates a synthetic source/article chain, splits sentences, analyzes, and persists.
func (d *DB) ProcessText(userID int64, text string, a *analyze.Analyzer, now time.Time) (articleID int64, err error) {
	if a == nil {
		return 0, fmt.Errorf("analyzer is nil")
	}

	sourceID, err := d.EnsureSource("manual", "", "Phase 1 library ingest (not RSS)")
	if err != nil {
		return 0, err
	}

	// external_id unique per call via timestamp + length
	extID := fmt.Sprintf("manual-%d-%d", now.UnixNano(), len(text))
	articleID, err = d.InsertArticle(sourceID, extID, "manual", "", text, now)
	if err != nil {
		return 0, err
	}

	sentences := analyze.SplitSentences(text)
	for i, s := range sentences {
		sid, err := d.InsertSentence(articleID, s, i)
		if err != nil {
			return articleID, err
		}
		if err := d.ExtractSentence(userID, sid, s, a, now); err != nil {
			return articleID, err
		}
	}
	return articleID, nil
}

func upsertWordTx(tx interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}, lemma, reading string) (int64, error) {
	var id int64
	err := tx.QueryRow(
		`SELECT id FROM words WHERE lemma = ? AND reading = ?`,
		lemma, reading,
	).Scan(&id)
	if err == nil {
		return id, nil
	}

	res, err := tx.Exec(
		`INSERT INTO words (lemma, reading) VALUES (?, ?)`,
		lemma, reading,
	)
	if err != nil {
		// Race / concurrent insert: re-select
		err2 := tx.QueryRow(
			`SELECT id FROM words WHERE lemma = ? AND reading = ?`,
			lemma, reading,
		).Scan(&id)
		if err2 == nil {
			return id, nil
		}
		return 0, fmt.Errorf("insert word: %w", err)
	}
	return res.LastInsertId()
}

func upsertNewCardTx(tx interface {
	Exec(query string, args ...any) (sql.Result, error)
}, userID, wordID int64, nowStr string) error {
	// New card defaults from docs/sm2-spec.md
	_, err := tx.Exec(
		`INSERT INTO cards (
			user_id, word_id, phase, learning_step, interval_days, ease,
			due_at, reps, lapses, created_at, updated_at
		) VALUES (?, ?, 'new', 0, 0, ?, ?, 0, 0, ?, ?)
		ON CONFLICT(user_id, word_id) DO NOTHING`,
		userID, wordID, StartingEase, nowStr, nowStr, nowStr,
	)
	if err != nil {
		return fmt.Errorf("insert card: %w", err)
	}
	return nil
}
