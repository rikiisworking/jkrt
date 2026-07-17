// Package analyze wraps Kagome (IPA) to emit Word candidates from Japanese text.
package analyze

import (
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome/v2/tokenizer"
)

// Candidate is a Word candidate: lemma + reading with surface span in the sentence.
// CharStart/CharEnd are rune offsets into the sentence text (end exclusive).
type Candidate struct {
	Lemma     string
	Reading   string
	Surface   string
	CharStart int
	CharEnd   int
}

// Analyzer tokenizes Japanese with Kagome + IPA dictionary.
type Analyzer struct {
	tok *tokenizer.Tokenizer
}

var (
	defaultOnce sync.Once
	defaultAna  *Analyzer
	defaultErr  error
)

// New builds an Analyzer with the IPA dictionary.
func New() (*Analyzer, error) {
	t, err := tokenizer.New(ipa.Dict(), tokenizer.OmitBosEos())
	if err != nil {
		return nil, fmt.Errorf("kagome tokenizer: %w", err)
	}
	return &Analyzer{tok: t}, nil
}

// Default returns a process-wide Analyzer (lazy init). Safe for concurrent use
// after first successful call (Kagome tokenizer is safe for concurrent Tokenize).
func Default() (*Analyzer, error) {
	defaultOnce.Do(func() {
		defaultAna, defaultErr = New()
	})
	return defaultAna, defaultErr
}

// Candidates tokenizes sentence text and returns Word candidates only:
// Tokens with ≥1 kanji and a non-empty reading. Empty readings are skipped.
func (a *Analyzer) Candidates(sentence string) ([]Candidate, error) {
	if a == nil || a.tok == nil {
		return nil, fmt.Errorf("analyzer is nil")
	}
	tokens := a.tok.Tokenize(sentence)
	out := make([]Candidate, 0, len(tokens))
	for _, tok := range tokens {
		c, ok := candidateFromToken(tok)
		if !ok {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// candidateFromToken applies the locked Word-candidate rule.
// Exported via testing through Candidates; logic kept pure for unit tests of edge cases.
func candidateFromToken(tok tokenizer.Token) (Candidate, bool) {
	surface := tok.Surface
	if surface == "" {
		return Candidate{}, false
	}
	if !ContainsKanji(surface) {
		return Candidate{}, false
	}

	reading, ok := tok.Reading()
	reading = strings.TrimSpace(reading)
	if !ok || reading == "" {
		// Empty reading: skip — no Word, no Card (DEVELOPMENT_PLAN).
		return Candidate{}, false
	}

	lemma := surface
	if base, ok := tok.BaseForm(); ok && strings.TrimSpace(base) != "" {
		lemma = base
	}

	// Kagome Start/End are rune offsets into the input string.
	return Candidate{
		Lemma:     lemma,
		Reading:   reading,
		Surface:   surface,
		CharStart: tok.Start,
		CharEnd:   tok.End,
	}, true
}

// ContainsKanji reports whether s contains at least one Han (kanji) character.
func ContainsKanji(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// IsWordCandidate reports whether surface+reading would become a Word candidate.
// Used by tests for empty-reading and pure-kana cases without depending on Kagome output.
func IsWordCandidate(surface, reading string) bool {
	if !ContainsKanji(surface) {
		return false
	}
	if strings.TrimSpace(reading) == "" {
		return false
	}
	return true
}
