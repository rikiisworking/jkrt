package db

import (
	"database/sql"
	"fmt"
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
		SELECT id, text, order_index
		FROM sentences
		WHERE article_id = ?
		ORDER BY order_index ASC, id ASC`, id)
	if err != nil {
		return ArticleDetail{}, nil, false, fmt.Errorf("list sentences: %w", err)
	}
	defer rows.Close()

	var sents []SentenceListItem
	for rows.Next() {
		var s SentenceListItem
		if err := rows.Scan(&s.ID, &s.Text, &s.OrderIndex); err != nil {
			return ArticleDetail{}, nil, false, fmt.Errorf("scan sentence: %w", err)
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
