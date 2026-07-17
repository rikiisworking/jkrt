package jlpt

import (
	"context"
	"fmt"
	"log"
)

// ResolveResult is the outcome of map/cache/classifier for one Word.
type ResolveResult struct {
	Level     Level
	Eligible  bool
	Source    string // embed | cache | headless | skip
	Classified bool  // true when classifier was invoked
}

// Cache stores classified levels (SQLite-backed in production).
type Cache interface {
	Get(lemma, reading string) (Level, bool, error)
	Put(lemma, reading string, level Level, source string) error
}

// ResolveOptions controls unknown-word handling.
type ResolveOptions struct {
	Classifier Classifier
	Cache      Cache
	// MaxClassify caps classifier calls per extract batch (0 = unlimited).
	MaxClassify int
	// ClassifyUsed counts classifier invocations in this batch (optional shared pointer).
	ClassifyUsed *int
}

// Resolve decides whether a Word candidate is N2+ eligible.
// Order: embed map → cache → classifier (if configured) → skip unlisted.
func Resolve(ctx context.Context, lemma, reading string, opt ResolveOptions) (ResolveResult, error) {
	if lv, ok := Lookup(lemma, reading); ok {
		return ResolveResult{
			Level:    lv,
			Eligible: IsN2Plus(lv),
			Source:   "embed",
		}, nil
	}

	if opt.Cache != nil {
		if lv, ok, err := opt.Cache.Get(lemma, reading); err != nil {
			return ResolveResult{}, fmt.Errorf("jlpt cache get: %w", err)
		} else if ok {
			return ResolveResult{
				Level:    lv,
				Eligible: IsN2Plus(lv),
				Source:   "cache",
			}, nil
		}
	}

	if opt.Classifier == nil {
		return ResolveResult{Source: "skip"}, nil
	}

	if opt.MaxClassify > 0 && opt.ClassifyUsed != nil && *opt.ClassifyUsed >= opt.MaxClassify {
		return ResolveResult{Source: "skip"}, nil
	}

	lv, err := opt.Classifier.Classify(ctx, lemma, reading)
	if opt.ClassifyUsed != nil {
		*opt.ClassifyUsed++
	}
	if err != nil {
		log.Printf("jlpt: classify %q/%q: %v", lemma, reading, err)
		return ResolveResult{Source: "skip", Classified: true}, nil
	}
	if _, ok := ParseLevel(string(lv)); !ok {
		log.Printf("jlpt: invalid level %q for %q/%q", lv, lemma, reading)
		return ResolveResult{Source: "skip", Classified: true}, nil
	}

	if opt.Cache != nil {
		if err := opt.Cache.Put(lemma, reading, lv, "headless"); err != nil {
			log.Printf("jlpt: cache put %q/%q: %v", lemma, reading, err)
		}
	}

	return ResolveResult{
		Level:      lv,
		Eligible:   IsN2Plus(lv),
		Source:     "headless",
		Classified: true,
	}, nil
}

// ResolveBatch applies Resolve to many candidates with a shared classify budget.
func ResolveBatch(ctx context.Context, items [][2]string, opt ResolveOptions) ([]ResolveResult, error) {
	used := 0
	opt.ClassifyUsed = &used
	out := make([]ResolveResult, len(items))
	for i, it := range items {
		r, err := Resolve(ctx, it[0], it[1], opt)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}
