package scrape

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Item is one RSS 2.0 item after parsing.
type Item struct {
	Title          string
	Link           string
	GUID           string
	Description    string
	ContentEncoded string
}

// ExternalID returns guid if set, otherwise link. Empty if both missing.
func (it Item) ExternalID() string {
	if g := strings.TrimSpace(it.GUID); g != "" {
		return g
	}
	return strings.TrimSpace(it.Link)
}

// RawText builds article raw_text: title + "\n" + description,
// and content:encoded when present (DEVELOPMENT_PLAN RSS ingest).
func (it Item) RawText() string {
	title := strings.TrimSpace(it.Title)
	desc := strings.TrimSpace(it.Description)
	content := strings.TrimSpace(it.ContentEncoded)

	var body string
	switch {
	case desc != "" && content != "":
		body = desc + "\n" + content
	case desc != "":
		body = desc
	case content != "":
		body = content
	}

	switch {
	case title != "" && body != "":
		return title + "\n" + body
	case title != "":
		return title
	default:
		return body
	}
}

type rssDoc struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	// content:encoded (optional)
	ContentEncoded string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
}

// ParseRSS reads an RSS 2.0 document and returns channel items.
// Items with neither guid nor link are skipped (no stable external_id).
func ParseRSS(r io.Reader) ([]Item, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read rss: %w", err)
	}
	return ParseRSSBytes(data)
}

// ParseRSSBytes is like ParseRSS but takes the full document bytes.
func ParseRSSBytes(data []byte) ([]Item, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("empty rss document")
	}

	var doc rssDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}

	out := make([]Item, 0, len(doc.Channel.Items))
	for _, raw := range doc.Channel.Items {
		it := Item{
			Title:          strings.TrimSpace(raw.Title),
			Link:           strings.TrimSpace(raw.Link),
			GUID:           strings.TrimSpace(raw.GUID),
			Description:    strings.TrimSpace(raw.Description),
			ContentEncoded: strings.TrimSpace(raw.ContentEncoded),
		}
		if it.ExternalID() == "" {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}
