package jlpt_test

import (
	"testing"

	"github.com/rikiisworking/jkrt/internal/jlpt"
)

func TestEmbedLoads(t *testing.T) {
	if err := jlpt.EmbedLoadError(); err != nil {
		t.Fatal(err)
	}
	if n := jlpt.EmbedSize(); n < 1000 {
		t.Fatalf("embed too small: %d", n)
	}
}

func TestLookupKnown(t *testing.T) {
	// From open-anki CSVs (committed words.json).
	cases := []struct {
		lemma, reading string
		want           jlpt.Level
	}{
		{"犬", "いぬ", jlpt.N5},
		{"就任", "しゅうにん", jlpt.N2},
		{"政策", "せいさく", jlpt.N1},
	}
	for _, tc := range cases {
		lv, ok := jlpt.Lookup(tc.lemma, tc.reading)
		if !ok {
			t.Fatalf("Lookup(%q,%q) miss", tc.lemma, tc.reading)
		}
		if lv != tc.want {
			t.Fatalf("Lookup(%q,%q)=%s want %s", tc.lemma, tc.reading, lv, tc.want)
		}
	}
}

func TestEligibleFromMap(t *testing.T) {
	if jlpt.EligibleFromMap("犬", "いぬ") {
		t.Fatal("n5 must not be eligible")
	}
	if !jlpt.EligibleFromMap("就任", "しゅうにん") {
		t.Fatal("n2 must be eligible")
	}
	if !jlpt.EligibleFromMap("政策", "せいさく") {
		t.Fatal("n1 must be eligible")
	}
	if jlpt.EligibleFromMap("これはない語", "ない") {
		t.Fatal("unlisted must not be eligible from map alone")
	}
}

func TestIsN2Plus(t *testing.T) {
	if !jlpt.IsN2Plus(jlpt.N1) || !jlpt.IsN2Plus(jlpt.N2) {
		t.Fatal("n1/n2")
	}
	if jlpt.IsN2Plus(jlpt.N3) || jlpt.IsN2Plus(jlpt.N5) {
		t.Fatal("n3/n5")
	}
}
