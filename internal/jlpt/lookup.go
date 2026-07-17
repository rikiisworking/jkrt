package jlpt

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed words.json
var wordsJSON []byte

type embedRow struct {
	L string `json:"l"`
	R string `json:"r"`
	V string `json:"v"`
}

var (
	mapOnce sync.Once
	// key: lemma + "\x00" + reading
	byLemmaReading map[string]Level
	// key: lemma only (ambiguous; first/easiest wins at load)
	byLemma map[string]Level
	loadErr error
)

func loadMaps() {
	mapOnce.Do(func() {
		byLemmaReading = make(map[string]Level, 8192)
		byLemma = make(map[string]Level, 8192)
		var rows []embedRow
		if err := json.Unmarshal(wordsJSON, &rows); err != nil {
			loadErr = err
			return
		}
		for _, row := range rows {
			l := strings.TrimSpace(row.L)
			r := strings.TrimSpace(row.R)
			lv, ok := ParseLevel(row.V)
			if !ok || l == "" || r == "" {
				continue
			}
			key := l + "\x00" + r
			if prev, ok := byLemmaReading[key]; !ok || Rank(lv) > Rank(prev) {
				byLemmaReading[key] = lv
			}
			if prev, ok := byLemma[l]; !ok || Rank(lv) > Rank(prev) {
				byLemma[l] = lv
			}
		}
	})
}

// Lookup returns the embedded map level for lemma+reading.
// Prefers exact lemma+reading; falls back to lemma-only.
// ok=false when not in map (or embed failed to load).
func Lookup(lemma, reading string) (Level, bool) {
	loadMaps()
	if loadErr != nil {
		return "", false
	}
	lemma = strings.TrimSpace(lemma)
	reading = strings.TrimSpace(reading)
	if lemma == "" {
		return "", false
	}
	if reading != "" {
		if lv, ok := byLemmaReading[lemma+"\x00"+reading]; ok {
			return lv, true
		}
	}
	if lv, ok := byLemma[lemma]; ok {
		return lv, true
	}
	return "", false
}

// EligibleFromMap is true when the word is listed as N2 or N1.
// Unlisted words return false (use Resolve + Classifier for unknowns).
func EligibleFromMap(lemma, reading string) bool {
	lv, ok := Lookup(lemma, reading)
	if !ok {
		return false
	}
	return IsN2Plus(lv)
}

// EmbedLoadError returns embed parse error if any (tests).
func EmbedLoadError() error {
	loadMaps()
	return loadErr
}

// EmbedSize returns number of lemma+reading keys (tests).
func EmbedSize() int {
	loadMaps()
	return len(byLemmaReading)
}
