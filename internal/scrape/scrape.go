// Package scrape fetches dual NHK RSS feeds and ingests Articles via db.IngestArticle.
package scrape

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
)

// v1 Source names (news_sources.name).
const (
	SourceNHKMain = "nhk_main"
	SourceNHKEasy = "nhk_easy"
)

// DefaultMainRSSURL is the verified NHK main cat0 feed (DEVELOPMENT_PLAN.md).
const DefaultMainRSSURL = "https://news.web.nhk/n-data/conf/na/rss/cat0.xml"

// DefaultTimeout is the per-feed HTTP timeout.
const DefaultTimeout = 15 * time.Second

// HTTPDoer is a mockable HTTP client (satisfied by *http.Client).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Source is one configured RSS feed to pull on Scrape.
type Source struct {
	Name    string
	FeedURL string
	Notes   string
}

// SourceResult is the per-source outcome for POST /api/scrape JSON.
type SourceResult struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	ItemsNew int    `json:"items_new"`
	Error    string `json:"error,omitempty"`
}

// Result is the full Scrape response body.
type Result struct {
	Sources []SourceResult `json:"sources"`
}

// Scraper fetches both NHK feeds and calls IngestArticle per item.
type Scraper struct {
	Client   HTTPDoer
	DB       *db.DB
	Analyzer *analyze.Analyzer
	Sources  []Source
	Timeout  time.Duration // request context timeout per feed when Client has no timeout
	UserID   int64
}

// DefaultSources returns the v1 dual-NHK source list from configured URLs.
// Easy URL may be empty (soft-fail at scrape time until JKRT_NHK_EASY_RSS_URL is set).
func DefaultSources(mainURL, easyURL string) []Source {
	if strings.TrimSpace(mainURL) == "" {
		mainURL = DefaultMainRSSURL
	}
	return []Source{
		{
			Name:    SourceNHKMain,
			FeedURL: strings.TrimSpace(mainURL),
			Notes:   "NHK main news RSS",
		},
		{
			Name:    SourceNHKEasy,
			FeedURL: strings.TrimSpace(easyURL),
			Notes:   "NHK Easy RSS (optional URL until configured)",
		},
	}
}

// New builds a Scraper with defaults. client may be nil (*http.Client with DefaultTimeout).
func New(database *db.DB, ana *analyze.Analyzer, sources []Source, client HTTPDoer) *Scraper {
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	if sources == nil {
		sources = DefaultSources(DefaultMainRSSURL, "")
	}
	return &Scraper{
		Client:   client,
		DB:       database,
		Analyzer: ana,
		Sources:  sources,
		Timeout:  DefaultTimeout,
		UserID:   db.LearnerUserID,
	}
}

// Run fetches every configured Source sequentially and ingests new Articles.
// Partial success is normal: each source gets its own ok/error in Result.
// Never returns a top-level error for per-source failures (HTTP layer always 200).
func (s *Scraper) Run(ctx context.Context, now time.Time) Result {
	if s == nil {
		return Result{Sources: []SourceResult{{
			Name:  "unknown",
			OK:    false,
			Error: "scraper is nil",
		}}}
	}
	out := Result{Sources: make([]SourceResult, 0, len(s.Sources))}
	userID := s.UserID
	if userID == 0 {
		userID = db.LearnerUserID
	}
	for _, src := range s.Sources {
		out.Sources = append(out.Sources, s.scrapeOne(ctx, userID, src, now))
	}
	return out
}

func (s *Scraper) scrapeOne(ctx context.Context, userID int64, src Source, now time.Time) SourceResult {
	res := SourceResult{Name: src.Name}
	if strings.TrimSpace(src.Name) == "" {
		res.Error = "source name is required"
		return res
	}
	if strings.TrimSpace(src.FeedURL) == "" {
		res.Error = "feed URL not configured"
		log.Printf("scrape: %s: %s", src.Name, res.Error)
		return res
	}
	if s.DB == nil {
		res.Error = "database is nil"
		return res
	}
	if s.Analyzer == nil {
		res.Error = "analyzer is nil"
		return res
	}
	if s.Client == nil {
		res.Error = "http client is nil"
		return res
	}

	items, err := s.fetchItems(ctx, src.FeedURL)
	if err != nil {
		res.Error = err.Error()
		log.Printf("scrape: %s: %v", src.Name, err)
		return res
	}

	srcRef := db.SourceRef{
		Name:    src.Name,
		FeedURL: src.FeedURL,
		Notes:   src.Notes,
	}

	newCount := 0
	for _, it := range items {
		extID := it.ExternalID()
		raw := it.RawText()
		if raw == "" {
			// Nothing useful to store; skip without failing the source.
			continue
		}
		ing, err := s.DB.IngestArticle(userID, srcRef, db.ArticleInput{
			ExternalID: extID,
			Title:      it.Title,
			URL:        it.Link,
			RawText:    raw,
			FetchedAt:  now,
		}, s.Analyzer, now)
		if err != nil {
			res.Error = fmt.Sprintf("ingest %s: %v", extID, err)
			res.ItemsNew = newCount
			log.Printf("scrape: %s: %v", src.Name, err)
			return res
		}
		if ing.Status == db.IngestCreated {
			newCount++
		}
	}

	res.OK = true
	res.ItemsNew = newCount
	return res
}

func (s *Scraper) fetchItems(ctx context.Context, feedURL string) ([]Item, error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "jkrt/1.0 (+local Japanese reading trainer; RSS only)")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Drain a little for connection reuse; ignore body errors.
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		return nil, fmt.Errorf("feed HTTP %d", resp.StatusCode)
	}

	// Cap body so a runaway response cannot blow memory.
	const maxBody = 8 << 20 // 8 MiB
	limited := io.LimitReader(resp.Body, maxBody+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read feed body: %w", err)
	}
	if len(data) > maxBody {
		return nil, fmt.Errorf("feed body exceeds %d bytes", maxBody)
	}

	items, err := ParseRSSBytes(data)
	if err != nil {
		return nil, err
	}
	return items, nil
}
