package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ArticleListItem is one row for the browse list (newest first).
type ArticleListItem struct {
	ID            int64
	SourceName    string
	Title         string
	URL           string
	FetchedAt     string
	SentenceCount int
}

// ArticleDetail is one Article for the detail page.
type ArticleDetail struct {
	ID         int64
	SourceName string
	Title      string
	URL        string
	FetchedAt  string
}

// SentenceListItem is one Sentence under an Article (order_index order).
type SentenceListItem struct {
	ID         int64
	Text       string
	OrderIndex int
	// Extracted is true when the learner opted this Sentence into study (extracted_at set).
	Extracted bool
	// ExtractedAt is RFC3339 when set; empty if not extracted.
	ExtractedAt string
	// WordCount is number of sentence_words rows (0 if never extracted or no kanji words).
	WordCount int
}

// DefaultArticleListLimit caps the articles browse list.
const DefaultArticleListLimit = 50

// ListArticles returns recent Articles with Source name and Sentence count.
// limit ≤ 0 uses DefaultArticleListLimit.
func (d *DB) ListArticles(limit int) ([]ArticleListItem, error) {
	if d == nil || d.sql == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if limit <= 0 {
		limit = DefaultArticleListLimit
	}
	rows, err := d.sql.Query(`
		SELECT a.id, ns.name, a.title, a.url, a.fetched_at,
		       (SELECT COUNT(1) FROM sentences s WHERE s.article_id = a.id)
		FROM articles a
		JOIN news_sources ns ON ns.id = a.source_id
		ORDER BY a.fetched_at DESC, a.id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list articles: %w", err)
	}
	defer rows.Close()

	var out []ArticleListItem
	for rows.Next() {
		var it ArticleListItem
		if err := rows.Scan(&it.ID, &it.SourceName, &it.Title, &it.URL, &it.FetchedAt, &it.SentenceCount); err != nil {
			return nil, fmt.Errorf("scan article: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []ArticleListItem{}
	}
	return out, nil
}

// GetArticle returns one Article and its Sentences ordered by order_index.
// ok is false when the Article does not exist.
func (d *DB) GetArticle(id int64) (ArticleDetail, []SentenceListItem, bool, error) {
	if d == nil || d.sql == nil {
		return ArticleDetail{}, nil, false, fmt.Errorf("db is nil")
	}
	var art ArticleDetail
	err := d.sql.QueryRow(`
		SELECT a.id, ns.name, a.title, a.url, a.fetched_at
		FROM articles a
		JOIN news_sources ns ON ns.id = a.source_id
		WHERE a.id = ?`, id).Scan(
		&art.ID, &art.SourceName, &art.Title, &art.URL, &art.FetchedAt,
	)
	if err == sql.ErrNoRows {
		return ArticleDetail{}, nil, false, nil
	}
	if err != nil {
		return ArticleDetail{}, nil, false, fmt.Errorf("get article: %w", err)
	}

	rows, err := d.sql.Query(`
		SELECT s.id, s.text, s.order_index, s.extracted_at,
		       (SELECT COUNT(1) FROM sentence_words sw WHERE sw.sentence_id = s.id)
		FROM sentences s
		WHERE s.article_id = ?
		ORDER BY s.order_index ASC, s.id ASC`, id)
	if err != nil {
		return ArticleDetail{}, nil, false, fmt.Errorf("list sentences: %w", err)
	}
	defer rows.Close()

	var sents []SentenceListItem
	for rows.Next() {
		var s SentenceListItem
		var ext sql.NullString
		if err := rows.Scan(&s.ID, &s.Text, &s.OrderIndex, &ext, &s.WordCount); err != nil {
			return ArticleDetail{}, nil, false, fmt.Errorf("scan sentence: %w", err)
		}
		if ext.Valid && strings.TrimSpace(ext.String) != "" {
			s.Extracted = true
			s.ExtractedAt = ext.String
		}
		sents = append(sents, s)
	}
	if err := rows.Err(); err != nil {
		return ArticleDetail{}, nil, false, err
	}
	if sents == nil {
		sents = []SentenceListItem{}
	}
	return art, sents, true, nil
}

// GetSentence returns one Sentence if it belongs to articleID.
func (d *DB) GetSentence(articleID, sentenceID int64) (SentenceListItem, bool, error) {
	if d == nil || d.sql == nil {
		return SentenceListItem{}, false, fmt.Errorf("db is nil")
	}
	var s SentenceListItem
	var ext sql.NullString
	err := d.sql.QueryRow(`
		SELECT s.id, s.text, s.order_index, s.extracted_at,
		       (SELECT COUNT(1) FROM sentence_words sw WHERE sw.sentence_id = s.id)
		FROM sentences s
		WHERE s.id = ? AND s.article_id = ?`, sentenceID, articleID,
	).Scan(&s.ID, &s.Text, &s.OrderIndex, &ext, &s.WordCount)
	if errors.Is(err, sql.ErrNoRows) {
		return SentenceListItem{}, false, nil
	}
	if err != nil {
		return SentenceListItem{}, false, fmt.Errorf("get sentence: %w", err)
	}
	if ext.Valid && strings.TrimSpace(ext.String) != "" {
		s.Extracted = true
		s.ExtractedAt = ext.String
	}
	return s, true, nil
}

// LastArticleFetchedAt returns the newest articles.fetched_at (any Source).
// ok is false when there are no Articles yet.
func (d *DB) LastArticleFetchedAt() (fetchedAt string, ok bool, err error) {
	if d == nil || d.sql == nil {
		return "", false, fmt.Errorf("db is nil")
	}
	var raw sql.NullString
	if err := d.sql.QueryRow(`SELECT MAX(fetched_at) FROM articles`).Scan(&raw); err != nil {
		return "", false, fmt.Errorf("last fetched: %w", err)
	}
	if !raw.Valid || raw.String == "" {
		return "", false, nil
	}
	return raw.String, true, nil
}

// CountArticles returns the total number of Article rows.
func (d *DB) CountArticles() (int, error) {
	if d == nil || d.sql == nil {
		return 0, fmt.Errorf("db is nil")
	}
	var n int
	if err := d.sql.QueryRow(`SELECT COUNT(1) FROM articles`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count articles: %w", err)
	}
	return n, nil
}
