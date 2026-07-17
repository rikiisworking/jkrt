package scrape_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rikiisworking/jkrt/internal/scrape"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/scrape → repo root testdata/rss
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "rss", name)
}

func TestParseMainFixture(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t, "nhk_main_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	items, err := scrape.ParseRSSBytes(data)
	if err != nil {
		t.Fatalf("ParseRSSBytes: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items: got %d want 2", len(items))
	}
	if items[0].ExternalID() != "https://example.test/main/item-1" {
		t.Fatalf("item0 external_id: %q", items[0].ExternalID())
	}
	// guid isPermaLink=false still uses guid text
	if items[1].ExternalID() != "main-guid-2" {
		t.Fatalf("item1 external_id: %q", items[1].ExternalID())
	}
	raw := items[0].RawText()
	if !strings.Contains(raw, "経済政策を発表した。") {
		t.Fatalf("raw_text missing title: %q", raw)
	}
	if !strings.Contains(raw, "政府は新しい経済政策") {
		t.Fatalf("raw_text missing description: %q", raw)
	}
	// content:encoded appended when present
	raw2 := items[1].RawText()
	if !strings.Contains(raw2, "追加の本文がある") {
		t.Fatalf("raw_text missing content:encoded: %q", raw2)
	}
	// Plan: title + "\n" + description (+ content)
	wantPrefix := "東京で会議が開かれた。\n専門家が経済の見通しについて議論した。\n追加の本文がある"
	if !strings.HasPrefix(raw2, wantPrefix) {
		t.Fatalf("raw_text shape:\n got %q\nwant prefix %q", raw2, wantPrefix)
	}
}

func TestParseEasyFixture(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t, "nhk_easy_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	items, err := scrape.ParseRSSBytes(data)
	if err != nil {
		t.Fatalf("ParseRSSBytes: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items: got %d want 3", len(items))
	}
	// item without guid falls back to link
	if items[2].ExternalID() != "https://example.test/easy/item-3" {
		t.Fatalf("item2 external_id: %q", items[2].ExternalID())
	}
}

func TestParseRSSReader(t *testing.T) {
	// ParseRSS (io.Reader) path — used when streaming; keep covered.
	f, err := os.Open(fixturePath(t, "nhk_easy_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	items, err := scrape.ParseRSS(f)
	if err != nil {
		t.Fatalf("ParseRSS: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items: got %d want 3", len(items))
	}
}

func TestParseSkipsItemsWithoutID(t *testing.T) {
	xml := `<?xml version="1.0"?><rss version="2.0"><channel>
		<item><title>no id</title><description>x</description></item>
		<item><title>has link</title><link>https://example.test/a</link><description>y</description></item>
	</channel></rss>`
	items, err := scrape.ParseRSSBytes([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items: got %d want 1", len(items))
	}
	if items[0].ExternalID() != "https://example.test/a" {
		t.Fatalf("external_id: %q", items[0].ExternalID())
	}
}

func TestParseEmptyChannelOK(t *testing.T) {
	xml := `<?xml version="1.0"?><rss version="2.0"><channel><title>empty</title></channel></rss>`
	items, err := scrape.ParseRSSBytes([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items: got %d want 0", len(items))
	}
}

func TestParseEmptyAndInvalid(t *testing.T) {
	if _, err := scrape.ParseRSSBytes(nil); err == nil {
		t.Fatal("expected error for empty")
	}
	if _, err := scrape.ParseRSSBytes([]byte("   ")); err == nil {
		t.Fatal("expected error for whitespace")
	}
	if _, err := scrape.ParseRSSBytes([]byte("not xml")); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := scrape.ParseRSS(strings.NewReader("")); err == nil {
		t.Fatal("expected error for empty reader")
	}
}

func TestExternalID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		it   scrape.Item
		want string
	}{
		{name: "guid wins over link", it: scrape.Item{GUID: "g1", Link: "https://x"}, want: "g1"},
		{name: "link when no guid", it: scrape.Item{Link: "https://x"}, want: "https://x"},
		{name: "trim guid", it: scrape.Item{GUID: "  g  ", Link: "https://x"}, want: "g"},
		{name: "whitespace guid falls back to link", it: scrape.Item{GUID: "   ", Link: "https://x"}, want: "https://x"},
		{name: "both empty", it: scrape.Item{}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.it.ExternalID(); got != tc.want {
				t.Fatalf("ExternalID: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRawText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		it   scrape.Item
		want string
	}{
		{
			name: "title only",
			it:   scrape.Item{Title: " だけ "},
			want: "だけ",
		},
		{
			name: "title and description",
			it:   scrape.Item{Title: "題", Description: "本文"},
			want: "題\n本文",
		},
		{
			name: "title description and content",
			it:   scrape.Item{Title: "題", Description: "概要", ContentEncoded: "詳細"},
			want: "題\n概要\n詳細",
		},
		{
			name: "content only",
			it:   scrape.Item{ContentEncoded: " 詳細 "},
			want: "詳細",
		},
		{
			name: "description only",
			it:   scrape.Item{Description: "概要"},
			want: "概要",
		},
		{
			name: "title and content no description",
			it:   scrape.Item{Title: "題", ContentEncoded: "詳細"},
			want: "題\n詳細",
		},
		{
			name: "all empty",
			it:   scrape.Item{},
			want: "",
		},
		{
			name: "whitespace only fields",
			it:   scrape.Item{Title: "  ", Description: "\t", ContentEncoded: "\n"},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.it.RawText(); got != tc.want {
				t.Fatalf("RawText: got %q want %q", got, tc.want)
			}
		})
	}
}
