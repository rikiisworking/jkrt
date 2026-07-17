package db

import "unicode/utf8"

// Phase 6 size limits (performance / memory safety).

// MaxRawTextRunes caps Article.raw_text on ingest (title + RSS body).
// Oversized input is truncated at a rune boundary (Japanese-safe).
const MaxRawTextRunes = 50_000

// MaxExportCards caps rows returned by card export (JSON/CSV).
const MaxExportCards = 100_000

// MaxExportReviews caps review history rows in JSON export.
const MaxExportReviews = 200_000

// TruncateRawText returns s limited to MaxRawTextRunes (rune-safe).
// ok is false when truncation occurred.
func TruncateRawText(s string) (out string, truncated bool) {
	if utf8.RuneCountInString(s) <= MaxRawTextRunes {
		return s, false
	}
	runes := []rune(s)
	return string(runes[:MaxRawTextRunes]), true
}
