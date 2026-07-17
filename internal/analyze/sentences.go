package analyze

import (
	"strings"
	"unicode/utf8"
)

// sentenceEndRunes are clause terminators used to split article text into Sentences.
// Includes ASCII and fullwidth variants of 。！？
var sentenceEndRunes = map[rune]bool{
	'。': true,
	'！': true,
	'？': true,
	'!': true,
	'?': true,
	// Fullwidth question/exclamation already covered; some feeds use mixed punctuation.
}

// SplitSentences splits text on Japanese/ASCII sentence terminators.
// Terminators stay attached to the preceding sentence. Empty segments are dropped.
// If no terminator is found, the whole trimmed text is one sentence (when non-empty).
func SplitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var out []string
	start := 0
	for i, r := range text {
		if !sentenceEndRunes[r] {
			continue
		}
		// Include terminator in the sentence (i is byte index of rune start).
		end := i + utf8.RuneLen(r)
		seg := strings.TrimSpace(text[start:end])
		if seg != "" {
			out = append(out, seg)
		}
		start = end
	}
	if start < len(text) {
		seg := strings.TrimSpace(text[start:])
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}
