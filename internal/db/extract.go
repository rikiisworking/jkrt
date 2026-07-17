package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
)

// StartingEase is the SM-2 starting ease for new Cards (docs/sm2-spec.md).
const StartingEase = 2.5

// LearnerUserID is the single v1 learner (users.id = 1).
const LearnerUserID int64 = 1

// PersistCandidates upserts Words, replaces sentence_words for the sentence, and creates
// new Cards (phase=new) for the learner. Existing cards are left unchanged.
//
// Candidates are filtered again for empty/placeholder readings (defense in depth).
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

	if err := persistCandidatesTx(tx, userID, sentenceID, candidates, now); err != nil {
		return err
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
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("select source: %w", err)
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
// It creates a synthetic source/article chain, splits sentences, analyzes, and persists
// in a single transaction (no partial articles on mid-loop failure).
func (d *DB) ProcessText(userID int64, text string, a *analyze.Analyzer, now time.Time) (articleID int64, err error) {
	if d == nil || d.sql == nil {
		return 0, fmt.Errorf("db is nil")
	}
	if a == nil {
		return 0, fmt.Errorf("analyzer is nil")
	}
	if userID == 0 {
		return 0, fmt.Errorf("userID is required")
	}

	// Analyze outside the DB transaction so failures leave no partial rows.
	sentences := analyze.SplitSentences(text)
	type item struct {
		text  string
		cands []analyze.Candidate
	}
	items := make([]item, 0, len(sentences))
	for _, s := range sentences {
		cands, err := a.Candidates(s)
		if err != nil {
			return 0, err
		}
		items = append(items, item{text: s, cands: cands})
	}

	tx, err := d.sql.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	sourceID, err := ensureSourceTx(tx, "manual", "", "Phase 1 library ingest (not RSS)")
	if err != nil {
		return 0, err
	}

	extID, err := newManualExternalID()
	if err != nil {
		return 0, err
	}
	articleID, err = insertArticleTx(tx, sourceID, extID, "manual", "", text, now)
	if err != nil {
		return 0, err
	}

	for i, it := range items {
		sid, err := insertSentenceTx(tx, articleID, it.text, i)
		if err != nil {
			return 0, err
		}
		if err := persistCandidatesTx(tx, userID, sid, it.cands, now); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return articleID, nil
}

func newManualExternalID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("random external_id: %w", err)
	}
	return "manual-" + hex.EncodeToString(b[:]), nil
}

func persistCandidatesTx(tx *sql.Tx, userID, sentenceID int64, candidates []analyze.Candidate, now time.Time) error {
	nowStr := now.UTC().Format(time.RFC3339)

	// Re-extract replaces occurrence rows for this sentence (no duplicates).
	if _, err := tx.Exec(`DELETE FROM sentence_words WHERE sentence_id = ?`, sentenceID); err != nil {
		return fmt.Errorf("clear sentence_words: %w", err)
	}

	for _, c := range candidates {
		surface := c.Surface
		reading := strings.TrimSpace(c.Reading)
		// Defense in depth: empty / MeCab "*" reading must never create a Word/Card.
		if !analyze.IsWordCandidate(surface, reading) {
			continue
		}
		lemma := strings.TrimSpace(c.Lemma)
		if lemma == "" || analyze.IsMeCabPlaceholder(lemma) {
			lemma = strings.TrimSpace(surface)
		}
		if lemma == "" || analyze.IsMeCabPlaceholder(lemma) {
			continue
		}

		wordID, err := upsertWordTx(tx, lemma, reading)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(
			`INSERT INTO sentence_words (sentence_id, word_id, surface, char_start, char_end, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			sentenceID, wordID, surface, c.CharStart, c.CharEnd, nowStr,
		); err != nil {
			return fmt.Errorf("insert sentence_word: %w", err)
		}

		if err := upsertNewCardTx(tx, userID, wordID, nowStr); err != nil {
			return err
		}
	}
	return nil
}

func ensureSourceTx(tx *sql.Tx, name, feedURL, notes string) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM news_sources WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("select source: %w", err)
	}
	res, err := tx.Exec(
		`INSERT INTO news_sources (name, feed_url, enabled, notes) VALUES (?, ?, 1, ?)`,
		name, feedURL, notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert source: %w", err)
	}
	return res.LastInsertId()
}

func insertArticleTx(tx *sql.Tx, sourceID int64, externalID, title, url, rawText string, fetchedAt time.Time) (int64, error) {
	res, err := tx.Exec(
		`INSERT INTO articles (source_id, external_id, title, url, fetched_at, raw_text)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sourceID, externalID, title, url, fetchedAt.UTC().Format(time.RFC3339), rawText,
	)
	if err != nil {
		return 0, fmt.Errorf("insert article: %w", err)
	}
	return res.LastInsertId()
}

func insertSentenceTx(tx *sql.Tx, articleID int64, text string, orderIndex int) (int64, error) {
	res, err := tx.Exec(
		`INSERT INTO sentences (article_id, text, order_index) VALUES (?, ?, ?)`,
		articleID, text, orderIndex,
	)
	if err != nil {
		return 0, fmt.Errorf("insert sentence: %w", err)
	}
	return res.LastInsertId()
}

func upsertWordTx(tx *sql.Tx, lemma, reading string) (int64, error) {
	var id int64
	err := tx.QueryRow(
		`SELECT id FROM words WHERE lemma = ? AND reading = ?`,
		lemma, reading,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("select word: %w", err)
	}

	res, err := tx.Exec(
		`INSERT INTO words (lemma, reading) VALUES (?, ?)`,
		lemma, reading,
	)
	if err != nil {
		// Concurrent insert race: re-select.
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

func upsertNewCardTx(tx *sql.Tx, userID, wordID int64, nowStr string) error {
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
