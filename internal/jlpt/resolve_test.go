package jlpt_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/rikiisworking/jkrt/internal/jlpt"
)

type memCache struct {
	m map[string]jlpt.Level
}

func (c *memCache) Get(lemma, reading string) (jlpt.Level, bool, error) {
	lv, ok := c.m[lemma+"\x00"+reading]
	return lv, ok, nil
}

func (c *memCache) Put(lemma, reading string, level jlpt.Level, source string) error {
	if c.m == nil {
		c.m = map[string]jlpt.Level{}
	}
	c.m[lemma+"\x00"+reading] = level
	return nil
}

func TestResolveEmbedHitNoClassifier(t *testing.T) {
	var calls atomic.Int32
	clf := jlpt.FuncClassifier(func(ctx context.Context, lemma, reading string) (jlpt.Level, error) {
		calls.Add(1)
		return jlpt.N1, nil
	})
	r, err := jlpt.Resolve(context.Background(), "犬", "いぬ", jlpt.ResolveOptions{Classifier: clf})
	if err != nil {
		t.Fatal(err)
	}
	if r.Eligible || r.Source != "embed" {
		t.Fatalf("%+v", r)
	}
	if calls.Load() != 0 {
		t.Fatal("classifier must not run on embed hit")
	}
}

func TestResolveCacheHit(t *testing.T) {
	var calls atomic.Int32
	clf := jlpt.FuncClassifier(func(ctx context.Context, lemma, reading string) (jlpt.Level, error) {
		calls.Add(1)
		return jlpt.N1, nil
	})
	cache := &memCache{m: map[string]jlpt.Level{"珍語\x00ちんご": jlpt.N1}}
	r, err := jlpt.Resolve(context.Background(), "珍語", "ちんご", jlpt.ResolveOptions{
		Classifier: clf,
		Cache:      cache,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Eligible || r.Source != "cache" {
		t.Fatalf("%+v", r)
	}
	if calls.Load() != 0 {
		t.Fatal("classifier must not run on cache hit")
	}
}

func TestResolveClassifyN1(t *testing.T) {
	cache := &memCache{}
	r, err := jlpt.Resolve(context.Background(), "珍語", "ちんご", jlpt.ResolveOptions{
		Classifier: jlpt.StaticClassifier{Level: jlpt.N1},
		Cache:      cache,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Eligible || r.Source != "headless" || !r.Classified {
		t.Fatalf("%+v", r)
	}
	if lv, ok, _ := cache.Get("珍語", "ちんご"); !ok || lv != jlpt.N1 {
		t.Fatalf("cache: %v %v", lv, ok)
	}
}

func TestResolveClassifyN3Skip(t *testing.T) {
	r, err := jlpt.Resolve(context.Background(), "珍語", "ちんご", jlpt.ResolveOptions{
		Classifier: jlpt.StaticClassifier{Level: jlpt.N3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Eligible {
		t.Fatalf("n3 must not be eligible: %+v", r)
	}
}

func TestResolveClassifyErrorSkip(t *testing.T) {
	r, err := jlpt.Resolve(context.Background(), "珍語", "ちんご", jlpt.ResolveOptions{
		Classifier: jlpt.StaticClassifier{Err: errors.New("boom")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Eligible {
		t.Fatalf("%+v", r)
	}
}

func TestResolveNoClassifierSkipUnlisted(t *testing.T) {
	r, err := jlpt.Resolve(context.Background(), "珍語", "ちんご", jlpt.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Eligible || r.Source != "skip" {
		t.Fatalf("%+v", r)
	}
}

func TestResolveBatchCap(t *testing.T) {
	var calls atomic.Int32
	clf := jlpt.FuncClassifier(func(ctx context.Context, lemma, reading string) (jlpt.Level, error) {
		calls.Add(1)
		return jlpt.N1, nil
	})
	items := make([][2]string, 5)
	for i := range items {
		items[i] = [2]string{"未知" + string(rune('A'+i)), "みち"}
	}
	out, err := jlpt.ResolveBatch(context.Background(), items, jlpt.ResolveOptions{
		Classifier:  clf,
		MaxClassify: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2", calls.Load())
	}
	elig := 0
	for _, r := range out {
		if r.Eligible {
			elig++
		}
	}
	if elig != 2 {
		t.Fatalf("eligible=%d want 2", elig)
	}
}
