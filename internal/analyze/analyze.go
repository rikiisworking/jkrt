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
// Tokens with ≥1 kanji and a non-empty reading. Empty/placeholder readings are skipped.
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
func candidateFromToken(tok tokenizer.Token) (Candidate, bool) {
	surface := tok.Surface
	if surface == "" {
		return Candidate{}, false
	}
	if !ContainsKanji(surface) {
		return Candidate{}, false
	}

	reading, ok := tok.Reading()
	if !ok || !ValidReading(reading) {
		// Empty or MeCab "*" placeholder: skip — no Word, no Card.
		return Candidate{}, false
	}
	reading = strings.TrimSpace(reading)

	lemma := surface
	if base, ok := tok.BaseForm(); ok {
		base = strings.TrimSpace(base)
		// MeCab uses "*" for unknown base; keep surface as lemma.
		if base != "" && !IsMeCabPlaceholder(base) {
			lemma = base
		}
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

// IsMeCabPlaceholder reports whether s is the MeCab/IPA unknown marker "*".
func IsMeCabPlaceholder(s string) bool {
	return strings.TrimSpace(s) == "*"
}

// ValidReading reports whether reading can form Word identity (non-empty kana, not "*").
func ValidReading(reading string) bool {
	r := strings.TrimSpace(reading)
	if r == "" || IsMeCabPlaceholder(r) {
		return false
	}
	return true
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
// Used by tests and PersistCandidates defense-in-depth without depending on Kagome.
func IsWordCandidate(surface, reading string) bool {
	if !ContainsKanji(surface) {
		return false
	}
	return ValidReading(reading)
}
