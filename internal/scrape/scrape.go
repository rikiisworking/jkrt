// Package scrape fetches configured RSS feeds and stores Articles/Sentences via db.StoreArticle.
// Store-only (ADR 0006): no morphological analysis and no Cards on Scrape.
package scrape

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/rikiisworking/jkrt/internal/db"
)

// Source names (news_sources.name). Stable identifiers for UI/JSON and DB rows.
const (
	SourceNHKMain     = "nhk_main"
	SourceYahooTopics = "yahoo_topics"
	SourceITmediaNews = "itmedia_news"
	SourceBBCJapanese = "bbc_japanese"
)

// Default feed URLs (hardcoded; NHK main may be overridden via config env).
const (
	// DefaultMainRSSURL is the verified NHK main cat0 feed (DEVELOPMENT_PLAN.md).
	DefaultMainRSSURL = "https://news.web.nhk/n-data/conf/na/rss/cat0.xml"
	// DefaultYahooTopicsRSSURL is Yahoo!ニュース major topics (Japanese, RSS 2.0).
	DefaultYahooTopicsRSSURL = "https://news.yahoo.co.jp/rss/topics/top-picks.xml"
	// DefaultITmediaNewsRSSURL is ITmedia NEWS latest list (Japanese, RSS 2.0).
	DefaultITmediaNewsRSSURL = "https://rss.itmedia.co.jp/rss/2.0/news_bursts.xml"
	// DefaultBBCJapaneseRSSURL is BBC News 日本語 (RSS 2.0).
	DefaultBBCJapaneseRSSURL = "https://feeds.bbci.co.uk/japanese/rss.xml"
)

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

// Scraper fetches every configured Source and stores Articles/Sentences only
// (extract-on-tap / ADR 0006). Analysis and Cards happen later via Sentence extract.
type Scraper struct {
	Client  HTTPDoer
	DB      *db.DB
	Sources []Source
	Timeout time.Duration // request context timeout per feed when Client has no timeout
	UserID  int64
}

// DefaultSources returns the built-in multi-publisher RSS list.
// mainURL overrides the NHK main default (empty → DefaultMainRSSURL).
// Extra publishers use hardcoded defaults (same style as NHK main).
func DefaultSources(mainURL string) []Source {
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
			Name:    SourceYahooTopics,
			FeedURL: DefaultYahooTopicsRSSURL,
			Notes:   "Yahoo!ニュース 主要トピックス RSS",
		},
		{
			Name:    SourceITmediaNews,
			FeedURL: DefaultITmediaNewsRSSURL,
			Notes:   "ITmedia NEWS 最新記事 RSS",
		},
		{
			Name:    SourceBBCJapanese,
			FeedURL: DefaultBBCJapaneseRSSURL,
			Notes:   "BBC News 日本語 RSS",
		},
	}
}

// New builds a Scraper with defaults. client may be nil (*http.Client with DefaultTimeout).
func New(database *db.DB, sources []Source, client HTTPDoer) *Scraper {
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	if sources == nil {
		sources = DefaultSources(DefaultMainRSSURL)
	}
	return &Scraper{
		Client:  client,
		DB:      database,
		Sources: sources,
		Timeout: DefaultTimeout,
		UserID:  db.LearnerUserID,
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
		ing, err := s.DB.StoreArticle(userID, srcRef, db.ArticleInput{
			ExternalID: extID,
			Title:      it.Title,
			URL:        it.Link,
			RawText:    raw,
			FetchedAt:  now,
		}, now)
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
