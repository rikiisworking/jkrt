package scrape_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/scrape"
)

// fixedTransport serves fixture bodies by full URL — never dials the network.
type fixedTransport map[string][]byte

func (ft fixedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, ok := ft[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("not found")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// errTransport always fails the HTTP call (simulates dial/timeout without network).
type errTransport struct{ err error }

func (e errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, e.err
}

// recordingTransport records request headers and returns a fixed body.
type recordingTransport struct {
	body    []byte
	lastReq *http.Request
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.lastReq = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(r.body))),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	mig := filepath.Join(repoRoot(t), "migrations")
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"), mig)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_, err = d.SQL().Exec(
		`INSERT INTO users (id, password_hash, created_at) VALUES (1, 'x', ?)`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return d
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "rss", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureClient(t *testing.T, mainURL, easyURL string) *http.Client {
	t.Helper()
	tr := fixedTransport{
		mainURL: readFixture(t, "nhk_main_sample.xml"),
		easyURL: readFixture(t, "nhk_easy_sample.xml"),
	}
	return &http.Client{Transport: tr}
}

func mustAnalyzer(t *testing.T) *analyze.Analyzer {
	t.Helper()
	a, err := analyze.New()
	if err != nil {
		t.Fatalf("analyzer: %v", err)
	}
	return a
}

func sourceByName(res scrape.Result, name string) (scrape.SourceResult, bool) {
	for _, sr := range res.Sources {
		if sr.Name == name {
			return sr, true
		}
	}
	return scrape.SourceResult{}, false
}

func TestScrapeBothFixturesIngest(t *testing.T) {
	d := openTestDB(t)
	a := mustAnalyzer(t)

	const (
		mainURL = "https://fixture.test/nhk_main.xml"
		easyURL = "https://fixture.test/nhk_easy.xml"
	)
	client := fixtureClient(t, mainURL, easyURL)
	sources := scrape.DefaultSources(mainURL, easyURL)
	s := scrape.New(d, a, sources, client)

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	res := s.Run(context.Background(), now)

	wantN := len(scrape.DefaultSources(mainURL, easyURL))
	if len(res.Sources) != wantN {
		t.Fatalf("sources: got %d want %d", len(res.Sources), wantN)
	}
	main, ok := sourceByName(res, scrape.SourceNHKMain)
	if !ok {
		t.Fatal("missing nhk_main in result")
	}
	easy, ok := sourceByName(res, scrape.SourceNHKEasy)
	if !ok {
		t.Fatal("missing nhk_easy in result")
	}
	if !main.OK {
		t.Fatalf("main not ok: %+v", main)
	}
	if !easy.OK {
		t.Fatalf("easy not ok: %+v", easy)
	}
	if main.ItemsNew != 2 {
		t.Fatalf("main items_new: got %d want 2", main.ItemsNew)
	}
	if easy.ItemsNew != 3 {
		t.Fatalf("easy items_new: got %d want 3", easy.ItemsNew)
	}
	if main.Error != "" || easy.Error != "" {
		t.Fatalf("unexpected errors main=%q easy=%q", main.Error, easy.Error)
	}
	// Extra publishers have no fixture URLs → partial fail is OK (no network).
	for _, name := range []string{scrape.SourceYahooTopics, scrape.SourceITmediaNews, scrape.SourceBBCJapanese} {
		sr, ok := sourceByName(res, name)
		if !ok {
			t.Fatalf("missing %s in result", name)
		}
		if sr.OK {
			t.Fatalf("%s should fail without fixture transport entry: %+v", name, sr)
		}
	}

	var articles int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM articles`).Scan(&articles); err != nil {
		t.Fatal(err)
	}
	if articles != 5 {
		t.Fatalf("articles: got %d want 5", articles)
	}

	// Sources seeded via EnsureSource path inside IngestArticle.
	var srcCount int
	if err := d.SQL().QueryRow(
		`SELECT COUNT(1) FROM news_sources WHERE name IN (?, ?)`,
		scrape.SourceNHKMain, scrape.SourceNHKEasy,
	).Scan(&srcCount); err != nil {
		t.Fatal(err)
	}
	if srcCount != 2 {
		t.Fatalf("news_sources: got %d want 2", srcCount)
	}

	// Spot-check one Article row maps feed fields correctly.
	var title, url, externalID, rawText, sourceName string
	err := d.SQL().QueryRow(`
		SELECT a.title, a.url, a.external_id, a.raw_text, s.name
		FROM articles a JOIN news_sources s ON s.id = a.source_id
		WHERE a.external_id = ?`,
		"https://example.test/main/item-1",
	).Scan(&title, &url, &externalID, &rawText, &sourceName)
	if err != nil {
		t.Fatalf("lookup article: %v", err)
	}
	if sourceName != scrape.SourceNHKMain {
		t.Fatalf("source: %q", sourceName)
	}
	if title != "経済政策を発表した。" {
		t.Fatalf("title: %q", title)
	}
	if url != "https://example.test/main/item-1" {
		t.Fatalf("url: %q", url)
	}
	if !strings.Contains(rawText, "政府は新しい経済政策") {
		t.Fatalf("raw_text: %q", rawText)
	}

	var sentences int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM sentences`).Scan(&sentences); err != nil {
		t.Fatal(err)
	}
	if sentences == 0 {
		t.Fatal("expected sentences after scrape")
	}

	var words int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&words); err != nil {
		t.Fatal(err)
	}
	if words == 0 {
		t.Fatal("expected words from analyzer")
	}
	var cards int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM cards WHERE user_id = 1`).Scan(&cards); err != nil {
		t.Fatal(err)
	}
	if cards == 0 {
		t.Fatal("expected cards on extract")
	}
	if cards != words {
		t.Fatalf("cards %d != words %d", cards, words)
	}

	// Second run: NHK fixtures dedupe → items_new = 0; extras still soft-fail without fixtures.
	res2 := s.Run(context.Background(), now)
	for _, name := range []string{scrape.SourceNHKMain, scrape.SourceNHKEasy} {
		sr, ok := sourceByName(res2, name)
		if !ok {
			t.Fatalf("second scrape missing %s", name)
		}
		if !sr.OK {
			t.Fatalf("second scrape %s not ok: %+v", name, sr)
		}
		if sr.ItemsNew != 0 {
			t.Fatalf("second scrape %s items_new: got %d want 0", name, sr.ItemsNew)
		}
	}
	var articles2 int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM articles`).Scan(&articles2); err != nil {
		t.Fatal(err)
	}
	if articles2 != articles {
		t.Fatalf("articles grew on dedupe scrape: %d → %d", articles, articles2)
	}
}

func TestScrapeEasyMissingURLSoftFail(t *testing.T) {
	d := openTestDB(t)
	a := mustAnalyzer(t)

	const mainURL = "https://fixture.test/nhk_main.xml"
	client := fixtureClient(t, mainURL, "https://unused/")
	sources := scrape.DefaultSources(mainURL, "") // empty easy
	s := scrape.New(d, a, sources, client)

	res := s.Run(context.Background(), time.Now().UTC())
	if len(res.Sources) != len(sources) {
		t.Fatalf("sources: %d want %d", len(res.Sources), len(sources))
	}
	main, _ := sourceByName(res, scrape.SourceNHKMain)
	easy, _ := sourceByName(res, scrape.SourceNHKEasy)
	if !main.OK || main.ItemsNew != 2 {
		t.Fatalf("main: %+v", main)
	}
	if easy.OK {
		t.Fatal("easy should soft-fail when URL empty")
	}
	if easy.ItemsNew != 0 {
		t.Fatalf("easy items_new: %d", easy.ItemsNew)
	}
	if !strings.Contains(easy.Error, "not configured") {
		t.Fatalf("easy error: %q", easy.Error)
	}
}

func TestScrapePartialHTTPFailure(t *testing.T) {
	d := openTestDB(t)
	a := mustAnalyzer(t)

	const (
		mainURL = "https://fixture.test/nhk_main.xml"
		easyURL = "https://fixture.test/nhk_easy_missing.xml"
	)
	// Only main is registered → easy gets 404 from transport.
	tr := fixedTransport{mainURL: readFixture(t, "nhk_main_sample.xml")}
	client := &http.Client{Transport: tr}
	s := scrape.New(d, a, scrape.DefaultSources(mainURL, easyURL), client)

	res := s.Run(context.Background(), time.Now().UTC())
	main, _ := sourceByName(res, scrape.SourceNHKMain)
	easy, _ := sourceByName(res, scrape.SourceNHKEasy)
	if !main.OK {
		t.Fatalf("main: %+v", main)
	}
	if easy.OK {
		t.Fatalf("easy should fail: %+v", easy)
	}
	if !strings.Contains(easy.Error, "HTTP 404") {
		t.Fatalf("easy error should mention HTTP 404, got %q", easy.Error)
	}
}

func TestScrapeClientDialError(t *testing.T) {
	d := openTestDB(t)
	a := mustAnalyzer(t)
	client := &http.Client{Transport: errTransport{err: errors.New("dial blocked")}}
	s := scrape.New(d, a, []scrape.Source{{
		Name:    scrape.SourceNHKMain,
		FeedURL: "https://fixture.test/main.xml",
	}}, client)

	res := s.Run(context.Background(), time.Now().UTC())
	if len(res.Sources) != 1 {
		t.Fatalf("sources: %+v", res.Sources)
	}
	sr := res.Sources[0]
	if sr.OK {
		t.Fatal("expected not ok")
	}
	if !strings.Contains(sr.Error, "fetch feed") && !strings.Contains(sr.Error, "dial blocked") {
		t.Fatalf("error: %q", sr.Error)
	}
}

func TestScrapeInvalidXMLBody(t *testing.T) {
	d := openTestDB(t)
	a := mustAnalyzer(t)
	const url = "https://fixture.test/bad.xml"
	client := &http.Client{Transport: fixedTransport{url: []byte("not-rss-at-all")}}
	s := scrape.New(d, a, []scrape.Source{{Name: "bad", FeedURL: url}}, client)

	res := s.Run(context.Background(), time.Now().UTC())
	if res.Sources[0].OK {
		t.Fatalf("expected parse failure: %+v", res.Sources[0])
	}
	if res.Sources[0].Error == "" {
		t.Fatal("expected error message")
	}
}

func TestScrapeSkipsEmptyRawTextItems(t *testing.T) {
	d := openTestDB(t)
	a := mustAnalyzer(t)
	// Item has guid (so it parses) but empty title/description → skip, not fail.
	xml := `<?xml version="1.0"?><rss version="2.0"><channel>
		<item><guid>empty-body</guid><title>  </title><description></description></item>
		<item><guid>good</guid><title>経済政策を発表した。</title><description>政府が発表した。</description></item>
	</channel></rss>`
	const url = "https://fixture.test/mixed.xml"
	client := &http.Client{Transport: fixedTransport{url: []byte(xml)}}
	s := scrape.New(d, a, []scrape.Source{{Name: "mixed", FeedURL: url}}, client)

	res := s.Run(context.Background(), time.Now().UTC())
	sr := res.Sources[0]
	if !sr.OK {
		t.Fatalf("should succeed while skipping empty: %+v", sr)
	}
	if sr.ItemsNew != 1 {
		t.Fatalf("items_new: got %d want 1", sr.ItemsNew)
	}
	var n int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM articles`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("articles: %d", n)
	}
}

func TestScrapeNilDeps(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.Background()

	// nil receiver
	var nilS *scrape.Scraper
	res := nilS.Run(ctx, now)
	if len(res.Sources) != 1 || res.Sources[0].OK {
		t.Fatalf("nil scraper: %+v", res)
	}

	d := openTestDB(t)
	a := mustAnalyzer(t)
	client := &http.Client{Transport: fixedTransport{}}

	// nil DB
	s := scrape.New(nil, a, []scrape.Source{{Name: "x", FeedURL: "https://x"}}, client)
	res = s.Run(ctx, now)
	if res.Sources[0].OK || !strings.Contains(res.Sources[0].Error, "database") {
		t.Fatalf("nil db: %+v", res.Sources[0])
	}

	// nil analyzer
	s = scrape.New(d, nil, []scrape.Source{{Name: "x", FeedURL: "https://x"}}, client)
	res = s.Run(ctx, now)
	if res.Sources[0].OK || !strings.Contains(res.Sources[0].Error, "analyzer") {
		t.Fatalf("nil analyzer: %+v", res.Sources[0])
	}

	// empty source name
	s = scrape.New(d, a, []scrape.Source{{Name: "  ", FeedURL: "https://x"}}, client)
	res = s.Run(ctx, now)
	if res.Sources[0].OK || !strings.Contains(res.Sources[0].Error, "name") {
		t.Fatalf("empty name: %+v", res.Sources[0])
	}
}

func TestScrapeSetsRequestHeaders(t *testing.T) {
	d := openTestDB(t)
	a := mustAnalyzer(t)
	rec := &recordingTransport{body: []byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`)}
	client := &http.Client{Transport: rec}
	const url = "https://fixture.test/headers.xml"
	s := scrape.New(d, a, []scrape.Source{{Name: "hdr", FeedURL: url}}, client)

	_ = s.Run(context.Background(), time.Now().UTC())
	if rec.lastReq == nil {
		t.Fatal("no request recorded")
	}
	if rec.lastReq.Method != http.MethodGet {
		t.Fatalf("method: %s", rec.lastReq.Method)
	}
	if rec.lastReq.URL.String() != url {
		t.Fatalf("url: %s", rec.lastReq.URL)
	}
	ua := rec.lastReq.Header.Get("User-Agent")
	if !strings.Contains(ua, "jkrt") {
		t.Fatalf("User-Agent: %q", ua)
	}
	if rec.lastReq.Header.Get("Accept") == "" {
		t.Fatal("expected Accept header")
	}
}

func TestDefaultSourcesUsesDefaultMainURL(t *testing.T) {
	src := scrape.DefaultSources("", "https://easy.example/rss")
	if len(src) < 5 {
		t.Fatalf("len: %d want at least NHK×2 + 3 extras", len(src))
	}
	if src[0].Name != scrape.SourceNHKMain || src[1].Name != scrape.SourceNHKEasy {
		t.Fatalf("names: %+v", src)
	}
	if src[0].FeedURL != scrape.DefaultMainRSSURL {
		t.Fatalf("main url: %q", src[0].FeedURL)
	}
	if src[1].FeedURL != "https://easy.example/rss" {
		t.Fatalf("easy url: %q", src[1].FeedURL)
	}
	byName := map[string]scrape.Source{}
	for _, s := range src {
		byName[s.Name] = s
	}
	for _, name := range []string{scrape.SourceYahooTopics, scrape.SourceITmediaNews, scrape.SourceBBCJapanese} {
		if byName[name].FeedURL == "" {
			t.Fatalf("%s missing default feed URL", name)
		}
	}
	if byName[scrape.SourceYahooTopics].FeedURL != scrape.DefaultYahooTopicsRSSURL {
		t.Fatalf("yahoo url: %q", byName[scrape.SourceYahooTopics].FeedURL)
	}
	if byName[scrape.SourceITmediaNews].FeedURL != scrape.DefaultITmediaNewsRSSURL {
		t.Fatalf("itmedia url: %q", byName[scrape.SourceITmediaNews].FeedURL)
	}
	if byName[scrape.SourceBBCJapanese].FeedURL != scrape.DefaultBBCJapaneseRSSURL {
		t.Fatalf("bbc url: %q", byName[scrape.SourceBBCJapanese].FeedURL)
	}
}

func TestNewDefaults(t *testing.T) {
	// nil client and nil sources should not panic and should fill defaults.
	s := scrape.New(nil, nil, nil, nil)
	if s.Client == nil {
		t.Fatal("expected default client")
	}
	want := len(scrape.DefaultSources(scrape.DefaultMainRSSURL, ""))
	if len(s.Sources) != want {
		t.Fatalf("default sources: %d want %d", len(s.Sources), want)
	}
	if s.Timeout != scrape.DefaultTimeout {
		t.Fatalf("timeout: %v", s.Timeout)
	}
	if s.UserID != db.LearnerUserID {
		t.Fatalf("userID: %d", s.UserID)
	}
}

// All built-in sources ingest offline fixtures (multi-publisher shapes).
func TestScrapeAllDefaultSourcesFixtures(t *testing.T) {
	d := openTestDB(t)
	a := mustAnalyzer(t)

	const (
		mainURL = "https://fixture.test/nhk_main.xml"
		easyURL = "https://fixture.test/nhk_easy.xml"
	)
	tr := fixedTransport{
		mainURL:                          readFixture(t, "nhk_main_sample.xml"),
		easyURL:                          readFixture(t, "nhk_easy_sample.xml"),
		scrape.DefaultYahooTopicsRSSURL:  readFixture(t, "yahoo_topics_sample.xml"),
		scrape.DefaultITmediaNewsRSSURL:  readFixture(t, "itmedia_news_sample.xml"),
		scrape.DefaultBBCJapaneseRSSURL:  readFixture(t, "bbc_japanese_sample.xml"),
	}
	client := &http.Client{Transport: tr}
	sources := scrape.DefaultSources(mainURL, easyURL)
	s := scrape.New(d, a, sources, client)

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	res := s.Run(context.Background(), now)
	if len(res.Sources) != len(sources) {
		t.Fatalf("sources: %d", len(res.Sources))
	}

	wantNew := map[string]int{
		scrape.SourceNHKMain:     2,
		scrape.SourceNHKEasy:     3,
		scrape.SourceYahooTopics: 2,
		scrape.SourceITmediaNews: 1,
		scrape.SourceBBCJapanese: 1,
	}
	totalNew := 0
	for _, sr := range res.Sources {
		if !sr.OK {
			t.Fatalf("%s not ok: %+v", sr.Name, sr)
		}
		want, ok := wantNew[sr.Name]
		if !ok {
			t.Fatalf("unexpected source %q", sr.Name)
		}
		if sr.ItemsNew != want {
			t.Fatalf("%s items_new: got %d want %d", sr.Name, sr.ItemsNew, want)
		}
		totalNew += sr.ItemsNew
	}

	var articles int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM articles`).Scan(&articles); err != nil {
		t.Fatal(err)
	}
	if articles != totalNew {
		t.Fatalf("articles: got %d want %d", articles, totalNew)
	}
	var srcCount int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM news_sources`).Scan(&srcCount); err != nil {
		t.Fatal(err)
	}
	if srcCount != len(sources) {
		t.Fatalf("news_sources: got %d want %d", srcCount, len(sources))
	}
	var words int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM words`).Scan(&words); err != nil {
		t.Fatal(err)
	}
	if words == 0 {
		t.Fatal("expected words from multi-publisher ingest")
	}
}

func TestScrapeContextCanceledSoftFail(t *testing.T) {
	d := openTestDB(t)
	a := mustAnalyzer(t)
	// Transport that blocks until the request context is done.
	tr := &blockingTransport{}
	client := &http.Client{Transport: tr}
	s := scrape.New(d, a, []scrape.Source{{
		Name:    "cancel_me",
		FeedURL: "https://fixture.test/blocked.xml",
	}}, client)
	s.Timeout = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before Run
	res := s.Run(ctx, time.Now().UTC())
	if len(res.Sources) != 1 {
		t.Fatalf("sources: %+v", res.Sources)
	}
	sr := res.Sources[0]
	if sr.OK {
		t.Fatalf("expected not ok: %+v", sr)
	}
	if sr.Error == "" {
		t.Fatal("expected error message")
	}
	var n int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM articles`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("canceled scrape must not insert articles: %d", n)
	}
}

// blockingTransport never returns a body; waits for req.Context().Done().
type blockingTransport struct{}

func (b *blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}
