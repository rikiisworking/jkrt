package jlpt

import (
	"context"
	"fmt"
)

// Classifier estimates a JLPT-ish level for Words not in the embed map.
// Production may shell out to Grok Build headless; tests inject fakes.
type Classifier interface {
	Classify(ctx context.Context, lemma, reading string) (Level, error)
}

// StaticClassifier always returns Level (or err if Level empty).
type StaticClassifier struct {
	Level Level
	Err   error
}

// Classify implements Classifier.
func (s StaticClassifier) Classify(ctx context.Context, lemma, reading string) (Level, error) {
	if s.Err != nil {
		return "", s.Err
	}
	if s.Level == "" {
		return "", fmt.Errorf("static classifier: empty level")
	}
	return s.Level, nil
}

// MapClassifier looks up fixed lemma\x00reading or lemma keys.
type MapClassifier map[string]Level

// Classify implements Classifier.
func (m MapClassifier) Classify(ctx context.Context, lemma, reading string) (Level, error) {
	if m == nil {
		return "", fmt.Errorf("map classifier: nil")
	}
	if reading != "" {
		if lv, ok := m[lemma+"\x00"+reading]; ok {
			return lv, nil
		}
	}
	if lv, ok := m[lemma]; ok {
		return lv, nil
	}
	return "", fmt.Errorf("map classifier: unknown %q/%q", lemma, reading)
}

// FuncClassifier adapts a function.
type FuncClassifier func(ctx context.Context, lemma, reading string) (Level, error)

// Classify implements Classifier.
func (f FuncClassifier) Classify(ctx context.Context, lemma, reading string) (Level, error) {
	return f(ctx, lemma, reading)
}
