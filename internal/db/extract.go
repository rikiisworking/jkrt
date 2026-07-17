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

// IngestStatus reports Article dedupe outcome for StoreArticle / IngestArticle / IngestText.
type IngestStatus int

const (
	// IngestCreated: new Article row + Sentences (library only on scrape path).
	IngestCreated IngestStatus = iota
	// IngestExists: (source_id, external_id) already present; no re-store.
	IngestExists
)

// IngestResult is the outcome of Article store.
type IngestResult struct {
	ArticleID int64
	Status    IngestStatus
}

// ExtractResult is the outcome of Sentence extract (opt-in to study).
type ExtractResult struct {
	SentenceID       int64
	ArticleID        int64
	AlreadyExtracted bool
	// Candidates is the number of Word candidates persisted (or would-be on re-extract).
	Candidates int
	// CardsNew is how many new Card rows were inserted (0 on re-extract of same words).
	CardsNew int
}

// Sentinel errors for extract.
var (
	ErrSentenceNotFound = errors.New("sentence not found")
	ErrArticleMismatch  = errors.New("sentence does not belong to article")
)

// execQuerier is satisfied by *sql.DB and *sql.Tx so Source/Article helpers
// share one SQL path (no public/tx twin pairs).
type execQuerier interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

// StoreArticle ensures the Source, then dedupes the Article by
// (source_id, external_id). On IngestCreated it splits RawText into Sentences only.
// Does not create Words, sentence_words, or Cards (extract-on-tap / ADR 0006).
// Analyzer may be nil (unused).
func (d *DB) StoreArticle(userID int64, src SourceRef, art ArticleInput, now time.Time) (IngestResult, error) {
	if d == nil || d.sql == nil {
		return IngestResult{}, fmt.Errorf("db is nil")
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

	// Phase 6 size limit: cap raw_text before store (rune-safe).
	art.RawText, _ = TruncateRawText(art.RawText)

	sourceID, err := ensureSource(d.sql, src.Name, src.FeedURL, src.Notes)
	if err != nil {
		return IngestResult{}, err
	}
	if existingID, ok, err := lookupArticle(d.sql, sourceID, art.ExternalID); err != nil {
		return IngestResult{}, err
	} else if ok {
		return IngestResult{ArticleID: existingID, Status: IngestExists}, nil
	}

	sentences := analyze.SplitSentences(art.RawText)

	tx, err := d.sql.Begin()
	if err != nil {
		return IngestResult{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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

	for i, text := range sentences {
		if _, err := insertSentence(tx, articleID, text, i); err != nil {
			return IngestResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return IngestResult{}, fmt.Errorf("commit: %w", err)
	}
	return IngestResult{ArticleID: articleID, Status: IngestCreated}, nil
}

// IngestArticle is the scrape path: store library only (Articles + Sentences).
// Analyzer is accepted for call-site compatibility but is not used (store-only).
// Prefer StoreArticle in new code. See ADR 0006.
func (d *DB) IngestArticle(userID int64, src SourceRef, art ArticleInput, a *analyze.Analyzer, now time.Time) (IngestResult, error) {
	_ = a
	return d.StoreArticle(userID, src, art, now)
}

// IngestText is the library/manual convenience path: store text as an Article under
// Source "manual", then extract every Sentence (creates Words/Cards).
// Used by tests and any manual ingest that should immediately enter the queue.
// User-facing Scrape uses StoreArticle / IngestArticle without extract.
//
// Not a single transaction: StoreArticle commits, then each ExtractSentence commits.
// If extract fails mid-way, the Article/Sentences remain with a partial set extracted.
// Callers that need all-or-nothing should not use this helper for product-facing flows.
func (d *DB) IngestText(userID int64, text string, a *analyze.Analyzer, now time.Time) (IngestResult, error) {
	if a == nil {
		return IngestResult{}, fmt.Errorf("analyzer is nil")
	}
	extID, err := newManualExternalID()
	if err != nil {
		return IngestResult{}, err
	}
	res, err := d.StoreArticle(userID, SourceRef{
		Name:  ManualSourceName,
		Notes: "library ingest (not RSS)",
	}, ArticleInput{
		ExternalID: extID,
		Title:      ManualSourceName,
		RawText:    text,
		FetchedAt:  now,
	}, now)
	if err != nil {
		return res, err
	}
	if err := d.ExtractAllSentences(userID, res.ArticleID, a, now); err != nil {
		return res, err
	}
	return res, nil
}

// ExtractAllSentences runs ExtractSentence for every sentence of the article (order_index).
func (d *DB) ExtractAllSentences(userID, articleID int64, a *analyze.Analyzer, now time.Time) error {
	if d == nil || d.sql == nil {
		return fmt.Errorf("db is nil")
	}
	rows, err := d.sql.Query(
		`SELECT id FROM sentences WHERE article_id = ? ORDER BY order_index ASC, id ASC`,
		articleID,
	)
	if err != nil {
		return fmt.Errorf("list sentences: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := d.ExtractSentence(userID, id, a, now); err != nil {
			return err
		}
	}
	return nil
}

// ExtractSentence analyzes one Sentence and creates Words / sentence_words / Cards.
// Idempotent: re-extract does not reset Card schedules; first extracted_at is kept.
// articleID may be 0 to skip ownership check; otherwise sentence must belong to articleID.
func (d *DB) ExtractSentence(userID, sentenceID int64, a *analyze.Analyzer, now time.Time) (ExtractResult, error) {
	return d.ExtractSentenceForArticle(userID, 0, sentenceID, a, now)
}

// ExtractSentenceForArticle is like ExtractSentence but requires sentence.article_id == articleID
// when articleID != 0 (HTTP path ownership).
func (d *DB) ExtractSentenceForArticle(userID, articleID, sentenceID int64, a *analyze.Analyzer, now time.Time) (ExtractResult, error) {
	if d == nil || d.sql == nil {
		return ExtractResult{}, fmt.Errorf("db is nil")
	}
	if a == nil {
		return ExtractResult{}, fmt.Errorf("analyzer is nil")
	}
	if userID == 0 {
		return ExtractResult{}, fmt.Errorf("userID is required")
	}
	if sentenceID == 0 {
		return ExtractResult{}, fmt.Errorf("sentenceID is required")
	}

	var text string
	var artID int64
	var extractedAt sql.NullString
	err := d.sql.QueryRow(
		`SELECT text, article_id, extracted_at FROM sentences WHERE id = ?`,
		sentenceID,
	).Scan(&text, &artID, &extractedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ExtractResult{}, ErrSentenceNotFound
	}
	if err != nil {
		return ExtractResult{}, fmt.Errorf("load sentence: %w", err)
	}
	if articleID != 0 && artID != articleID {
		return ExtractResult{}, ErrArticleMismatch
	}

	already := extractedAt.Valid && strings.TrimSpace(extractedAt.String) != ""
	out := ExtractResult{
		SentenceID:       sentenceID,
		ArticleID:        artID,
		AlreadyExtracted: already,
	}

	// Pure noop re-tap: do not re-analyze or wipe sentence_words (ADR review fix).
	if already {
		var n int
		if err := d.sql.QueryRow(
			`SELECT COUNT(1) FROM sentence_words WHERE sentence_id = ?`, sentenceID,
		).Scan(&n); err != nil {
			return ExtractResult{}, fmt.Errorf("count sentence_words: %w", err)
		}
		out.Candidates = n
		out.CardsNew = 0
		return out, nil
	}

	cands, err := a.Candidates(text)
	if err != nil {
		return ExtractResult{}, err
	}

	tx, err := d.sql.Begin()
	if err != nil {
		return ExtractResult{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	cardsNew, candCount, err := persistCandidatesTx(tx, userID, sentenceID, cands, now, d.scheduleParams())
	if err != nil {
		return ExtractResult{}, err
	}
	out.Candidates = candCount
	out.CardsNew = cardsNew

	nowStr := now.UTC().Format(time.RFC3339)
	if _, err := tx.Exec(
		`UPDATE sentences SET extracted_at = ? WHERE id = ? AND (extracted_at IS NULL OR extracted_at = '')`,
		nowStr, sentenceID,
	); err != nil {
		return ExtractResult{}, fmt.Errorf("set extracted_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ExtractResult{}, fmt.Errorf("commit: %w", err)
	}
	return out, nil
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
// Prefer ExtractSentence for the study opt-in path; this remains for
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

	if _, _, err := persistCandidatesTx(tx, userID, sentenceID, candidates, now, d.scheduleParams()); err != nil {
		return err
	}
	// Mark extracted when using low-level PersistCandidates (tests).
	nowStr := now.UTC().Format(time.RFC3339)
	if _, err := tx.Exec(
		`UPDATE sentences SET extracted_at = ? WHERE id = ? AND (extracted_at IS NULL OR extracted_at = '')`,
		nowStr, sentenceID,
	); err != nil {
		return fmt.Errorf("set extracted_at: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
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

// persistCandidatesTx writes sentence_words + new Cards.
// Returns cardsNew (RowsAffected inserts only) and word-candidate count.
func persistCandidatesTx(tx *sql.Tx, userID, sentenceID int64, candidates []analyze.Candidate, now time.Time, params schedule.Params) (cardsNew, candCount int, err error) {
	nowStr := now.UTC().Format(time.RFC3339)

	// Replace occurrence rows for this sentence (no duplicates).
	if _, err := tx.Exec(`DELETE FROM sentence_words WHERE sentence_id = ?`, sentenceID); err != nil {
		return 0, 0, fmt.Errorf("clear sentence_words: %w", err)
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
		candCount++

		wordID, err := upsertWord(tx, lemma, reading)
		if err != nil {
			return 0, 0, err
		}

		if _, err := tx.Exec(
			`INSERT INTO sentence_words (sentence_id, word_id, surface, char_start, char_end, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			sentenceID, wordID, surface, c.CharStart, c.CharEnd, nowStr,
		); err != nil {
			return 0, 0, fmt.Errorf("insert sentence_word: %w", err)
		}

		inserted, err := upsertNewCard(tx, userID, wordID, now, params)
		if err != nil {
			return 0, 0, err
		}
		if inserted {
			cardsNew++
		}
	}
	return cardsNew, candCount, nil
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

// upsertNewCard inserts a new Card if missing. Returns true when a row was inserted.
func upsertNewCard(q execQuerier, userID, wordID int64, now time.Time, params schedule.Params) (inserted bool, err error) {
	// New Card defaults only from schedule.NewCard (sm2-spec / ADR 0005).
	st := schedule.NewCard(params, now)
	nowStr := now.UTC().Format(time.RFC3339)
	dueStr := st.DueAt.UTC().Format(time.RFC3339)
	res, err := q.Exec(
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
		return false, fmt.Errorf("insert card: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("card rows affected: %w", err)
	}
	return n > 0, nil
}
