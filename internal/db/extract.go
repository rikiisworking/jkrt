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
	"github.com/rikiisworking/jkrt/internal/schedule"
)

// LearnerUserID is the single v1 learner (users.id = 1).
const LearnerUserID int64 = 1

// ManualSourceName is the news_sources.name used by IngestText (library/manual path).
const ManualSourceName = "manual"

// SourceRef identifies a news Source (configured RSS feed).
type SourceRef struct {
	Name    string
	FeedURL string
	Notes   string
}

// ArticleInput is one feed item (or manual bag) ready for ingest.
type ArticleInput struct {
	ExternalID string
	Title      string
	URL        string
	RawText    string
	FetchedAt  time.Time
}

// IngestStatus reports Article dedupe outcome for IngestArticle / IngestText.
type IngestStatus int

const (
	// IngestCreated: new Article row; Sentences, Words, and Cards written.
	IngestCreated IngestStatus = iota
	// IngestExists: (source_id, external_id) already present; no re-extract.
	IngestExists
)

// IngestResult is the outcome of Article ingest.
type IngestResult struct {
	ArticleID int64
	Status    IngestStatus
}

// execQuerier is satisfied by *sql.DB and *sql.Tx so Source/Article helpers
// share one SQL path (no public/tx twin pairs).
type execQuerier interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

// IngestArticle ensures the Source, then dedupes the Article by
// (source_id, external_id). On IngestCreated it splits RawText into Sentences,
// analyzes Word candidates, and persists Words/Cards in one transaction.
// On IngestExists it returns the existing article id and does not re-analyze
// (stable Scrape dedupe).
func (d *DB) IngestArticle(userID int64, src SourceRef, art ArticleInput, a *analyze.Analyzer, now time.Time) (IngestResult, error) {
	if d == nil || d.sql == nil {
		return IngestResult{}, fmt.Errorf("db is nil")
	}
	if a == nil {
		return IngestResult{}, fmt.Errorf("analyzer is nil")
	}
	if userID == 0 {
		return IngestResult{}, fmt.Errorf("userID is required")
	}
	if strings.TrimSpace(src.Name) == "" {
		return IngestResult{}, fmt.Errorf("source name is required")
	}
	if strings.TrimSpace(art.ExternalID) == "" {
		return IngestResult{}, fmt.Errorf("external_id is required")
	}

	fetchedAt := art.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = now
	}

	// Phase 6 size limit: cap raw_text before analyze (rune-safe).
	art.RawText, _ = TruncateRawText(art.RawText)

	// Fast dedupe path: no analyze when Article already stored (stable Scrape re-run).
	sourceID, err := ensureSource(d.sql, src.Name, src.FeedURL, src.Notes)
	if err != nil {
		return IngestResult{}, err
	}
	if existingID, ok, err := lookupArticle(d.sql, sourceID, art.ExternalID); err != nil {
		return IngestResult{}, err
	} else if ok {
		return IngestResult{ArticleID: existingID, Status: IngestExists}, nil
	}

	// Analyze outside the write transaction so tokenizer failures leave no partial rows.
	items, err := prepareSentenceItems(art.RawText, a)
	if err != nil {
		return IngestResult{}, err
	}

	tx, err := d.sql.Begin()
	if err != nil {
		return IngestResult{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Re-check inside the tx in case another writer inserted the same key.
	if existingID, ok, err := lookupArticle(tx, sourceID, art.ExternalID); err != nil {
		return IngestResult{}, err
	} else if ok {
		if err := tx.Commit(); err != nil {
			return IngestResult{}, fmt.Errorf("commit: %w", err)
		}
		return IngestResult{ArticleID: existingID, Status: IngestExists}, nil
	}

	title := art.Title
	if title == "" {
		title = src.Name
	}

	articleID, err := insertArticle(tx, sourceID, art.ExternalID, title, art.URL, art.RawText, fetchedAt)
	if err != nil {
		return IngestResult{}, err
	}

	if err := persistSentenceItems(tx, userID, articleID, items, now, d.scheduleParams()); err != nil {
		return IngestResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return IngestResult{}, fmt.Errorf("commit: %w", err)
	}
	return IngestResult{ArticleID: articleID, Status: IngestCreated}, nil
}

// IngestText is the library/manual path: raw Japanese string → Sentences → Words/Cards.
// Each call creates a new Article under Source "manual" with a unique external_id
// (always IngestCreated). Use IngestArticle with a stable external_id for Scrape dedupe.
func (d *DB) IngestText(userID int64, text string, a *analyze.Analyzer, now time.Time) (IngestResult, error) {
	extID, err := newManualExternalID()
	if err != nil {
		return IngestResult{}, err
	}
	return d.IngestArticle(userID, SourceRef{
		Name:  ManualSourceName,
		Notes: "library ingest (not RSS)",
	}, ArticleInput{
		ExternalID: extID,
		Title:      ManualSourceName,
		RawText:    text,
		FetchedAt:  now,
	}, a, now)
}

// EnsureSource inserts a news_sources row by name if missing and returns its id.
// Existing rows keep their feed_url (idempotent on name only).
func (d *DB) EnsureSource(name, feedURL, notes string) (int64, error) {
	if d == nil || d.sql == nil {
		return 0, fmt.Errorf("db is nil")
	}
	if strings.TrimSpace(name) == "" {
		return 0, fmt.Errorf("source name is required")
	}
	return ensureSource(d.sql, name, feedURL, notes)
}

// PersistCandidates upserts Words, replaces sentence_words for the sentence, and creates
// new Cards (phase=new) for the learner. Existing cards are left unchanged.
//
// Candidates are filtered again for empty/placeholder readings (defense in depth).
// Prefer IngestArticle / IngestText for full Article pipelines; this remains for
// tests and callers that already hold Candidates.
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

	if err := persistCandidatesTx(tx, userID, sentenceID, candidates, now, d.scheduleParams()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

type sentenceItem struct {
	text  string
	cands []analyze.Candidate
}

func prepareSentenceItems(text string, a *analyze.Analyzer) ([]sentenceItem, error) {
	sentences := analyze.SplitSentences(text)
	items := make([]sentenceItem, 0, len(sentences))
	for _, s := range sentences {
		cands, err := a.Candidates(s)
		if err != nil {
			return nil, err
		}
		items = append(items, sentenceItem{text: s, cands: cands})
	}
	return items, nil
}

func persistSentenceItems(tx *sql.Tx, userID, articleID int64, items []sentenceItem, now time.Time, params schedule.Params) error {
	for i, it := range items {
		sid, err := insertSentence(tx, articleID, it.text, i)
		if err != nil {
			return err
		}
		if err := persistCandidatesTx(tx, userID, sid, it.cands, now, params); err != nil {
			return err
		}
	}
	return nil
}

func newManualExternalID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("random external_id: %w", err)
	}
	return "manual-" + hex.EncodeToString(b[:]), nil
}

func persistCandidatesTx(tx *sql.Tx, userID, sentenceID int64, candidates []analyze.Candidate, now time.Time, params schedule.Params) error {
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

		wordID, err := upsertWord(tx, lemma, reading)
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

		if err := upsertNewCard(tx, userID, wordID, now, params); err != nil {
			return err
		}
	}
	return nil
}

func ensureSource(q execQuerier, name, feedURL, notes string) (int64, error) {
	var id int64
	err := q.QueryRow(`SELECT id FROM news_sources WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("select source: %w", err)
	}
	res, err := q.Exec(
		`INSERT INTO news_sources (name, feed_url, enabled, notes) VALUES (?, ?, 1, ?)`,
		name, feedURL, notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert source: %w", err)
	}
	return res.LastInsertId()
}

func lookupArticle(q execQuerier, sourceID int64, externalID string) (id int64, ok bool, err error) {
	err = q.QueryRow(
		`SELECT id FROM articles WHERE source_id = ? AND external_id = ?`,
		sourceID, externalID,
	).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return 0, false, fmt.Errorf("lookup article: %w", err)
}

func insertArticle(q execQuerier, sourceID int64, externalID, title, url, rawText string, fetchedAt time.Time) (int64, error) {
	res, err := q.Exec(
		`INSERT INTO articles (source_id, external_id, title, url, fetched_at, raw_text)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sourceID, externalID, title, url, fetchedAt.UTC().Format(time.RFC3339), rawText,
	)
	if err != nil {
		return 0, fmt.Errorf("insert article: %w", err)
	}
	return res.LastInsertId()
}

func insertSentence(q execQuerier, articleID int64, text string, orderIndex int) (int64, error) {
	res, err := q.Exec(
		`INSERT INTO sentences (article_id, text, order_index) VALUES (?, ?, ?)`,
		articleID, text, orderIndex,
	)
	if err != nil {
		return 0, fmt.Errorf("insert sentence: %w", err)
	}
	return res.LastInsertId()
}

func upsertWord(q execQuerier, lemma, reading string) (int64, error) {
	var id int64
	err := q.QueryRow(
		`SELECT id FROM words WHERE lemma = ? AND reading = ?`,
		lemma, reading,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("select word: %w", err)
	}

	res, err := q.Exec(
		`INSERT INTO words (lemma, reading) VALUES (?, ?)`,
		lemma, reading,
	)
	if err != nil {
		// Concurrent insert race: re-select.
		err2 := q.QueryRow(
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

func upsertNewCard(q execQuerier, userID, wordID int64, now time.Time, params schedule.Params) error {
	// New Card defaults only from schedule.NewCard (sm2-spec / ADR 0005).
	st := schedule.NewCard(params, now)
	nowStr := now.UTC().Format(time.RFC3339)
	dueStr := st.DueAt.UTC().Format(time.RFC3339)
	_, err := q.Exec(
		`INSERT INTO cards (
			user_id, word_id, phase, learning_step, interval_days, ease,
			due_at, reps, lapses, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, word_id) DO NOTHING`,
		userID, wordID,
		string(st.Phase), st.LearningStep, st.IntervalDays, st.Ease,
		dueStr, st.Reps, st.Lapses, nowStr, nowStr,
	)
	if err != nil {
		return fmt.Errorf("insert card: %w", err)
	}
	return nil
}
