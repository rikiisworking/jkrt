package analyze_test

import (
	"strings"
	"testing"

	"github.com/rikiisworking/jkrt/internal/analyze"
)

// sharedAnalyzer loads Kagome once for this file's tests (IPA dict is heavy).
func sharedAnalyzer(t *testing.T) *analyze.Analyzer {
	t.Helper()
	a, err := analyze.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	return a
}

func TestCandidatesPureKanaNoWords(t *testing.T) {
	a := sharedAnalyzer(t)
	// Particles / pure kana only — no Word candidates.
	cands, err := a.Candidates("これはテストです。")
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	// テスト is katakana (no Han); これ/は/です have no kanji.
	for _, c := range cands {
		if !analyze.ContainsKanji(c.Surface) {
			t.Fatalf("leaked non-kanji candidate: %+v", c)
		}
	}
	// Expect zero for this pure-kana/katakana sentence.
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates for pure kana/katakana, got %v", cands)
	}
}

func TestCandidatesExactSetForFixture(t *testing.T) {
	a := sharedAnalyzer(t)
	sentence := strings.TrimSpace(loadFixture(t, "keizai.txt"))
	cands, err := a.Candidates(sentence)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}

	// Tight contract for the plan fixture: exactly these three Words.
	want := map[string]string{
		"経済": "ケイザイ",
		"政策": "セイサク",
		"発表": "ハッピョウ",
	}
	if len(cands) != len(want) {
		t.Fatalf("candidate count: got %d want %d (%v)", len(cands), len(want), cands)
	}
	for _, c := range cands {
		reading, ok := want[c.Lemma]
		if !ok {
			t.Errorf("unexpected lemma %q (reading %q surface %q)", c.Lemma, c.Reading, c.Surface)
			continue
		}
		if c.Reading != reading {
			t.Errorf("lemma %s: reading got %q want %q", c.Lemma, c.Reading, reading)
		}
	}
}

func TestCandidatesNilAnalyzer(t *testing.T) {
	var a *analyze.Analyzer
	if _, err := a.Candidates("経済"); err == nil {
		t.Fatal("expected error for nil analyzer")
	}
	empty := &analyze.Analyzer{}
	if _, err := empty.Candidates("経済"); err == nil {
		t.Fatal("expected error for zero-value analyzer")
	}
}

func TestDefaultAnalyzerReusable(t *testing.T) {
	a1, err := analyze.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	a2, err := analyze.Default()
	if err != nil {
		t.Fatalf("Default second: %v", err)
	}
	if a1 != a2 {
		t.Fatal("Default should return same instance")
	}
	cands, err := a1.Candidates("東京")
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) == 0 {
		t.Fatal("expected at least 東京")
	}
}

func TestContainsKanjiTable(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"漢", true},
		{"漢字", true},
		{"漢字かな", true},
		{"カナ漢", true},
		{"ひらがな", false},
		{"カタカナ", false},
		{"abc", false},
		{"123", false},
		{"。", false},
		{"", false},
		{"々", true}, // iteration mark is Han
	}
	for _, tc := range cases {
		if got := analyze.ContainsKanji(tc.in); got != tc.want {
			t.Errorf("ContainsKanji(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsWordCandidateTable(t *testing.T) {
	cases := []struct {
		surface, reading string
		want             bool
	}{
		{"経済", "ケイザイ", true},
		{"経済", "", false},
		{"経済", " \t ", false},
		{"経済", "*", false},
		{"経済", " * ", false},
		{"を", "ヲ", false},
		{"する", "スル", false},
		{"", "ケイザイ", false},
		{"発表", "ハッピョウ", true},
	}
	for _, tc := range cases {
		if got := analyze.IsWordCandidate(tc.surface, tc.reading); got != tc.want {
			t.Errorf("IsWordCandidate(%q,%q)=%v want %v", tc.surface, tc.reading, got, tc.want)
		}
	}
}

func TestValidReadingAndMeCabPlaceholder(t *testing.T) {
	if analyze.ValidReading("*") {
		t.Fatal("MeCab * must not be a valid reading")
	}
	if analyze.ValidReading("") {
		t.Fatal("empty reading invalid")
	}
	if !analyze.ValidReading("ケイザイ") {
		t.Fatal("kana reading should be valid")
	}
	if !analyze.IsMeCabPlaceholder("*") || !analyze.IsMeCabPlaceholder(" * ") {
		t.Fatal("expected * placeholders")
	}
	if analyze.IsMeCabPlaceholder("ケイザイ") {
		t.Fatal("real reading is not a placeholder")
	}
}

func TestSplitSentencesTable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "ascii bang question",
			in:   "Hello! World?",
			want: []string{"Hello!", "World?"},
		},
		{
			name: "mixed fullwidth ascii",
			in:   "はい！本当?",
			want: []string{"はい！", "本当?"},
		},
		{
			name: "trailing text without terminator",
			in:   "一文。残り",
			want: []string{"一文。", "残り"},
		},
		{
			name: "only terminator",
			in:   "。",
			want: []string{"。"},
		},
		{
			name: "consecutive terminators",
			in:   "え？！",
			want: []string{"え？", "！"},
		},
		{
			name: "whitespace trim around segments",
			in:   "  あ。  い。  ",
			want: []string{"あ。", "い。"},
		},
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "multi jp news-like",
			in:   "経済政策を発表した。市場は反応した。",
			want: []string{"経済政策を発表した。", "市場は反応した。"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := analyze.SplitSentences(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len got %v want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestCandidatesMixedKanjiKanaCompound(t *testing.T) {
	a := sharedAnalyzer(t)
	// 自動車 has kanji; should be a candidate with a non-empty reading.
	cands, err := a.Candidates("自動車を買った。")
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	found := false
	for _, c := range cands {
		if strings.Contains(c.Lemma, "自動車") || c.Surface == "自動車" {
			found = true
			if c.Reading == "" {
				t.Fatal("自動車 reading empty")
			}
			if !analyze.ContainsKanji(c.Surface) {
				t.Fatal("surface must have kanji")
			}
		}
		// 買った → 買 may appear as kanji verb stem
	}
	if !found {
		// Kagome may split 自動車 differently; at least one kanji candidate required.
		if len(cands) == 0 {
			t.Fatalf("expected some kanji candidates, got none: %v", cands)
		}
	}
}

func TestCandidatesSpansNonOverlappingOrder(t *testing.T) {
	a := sharedAnalyzer(t)
	s := "経済政策を発表した。"
	cands, err := a.Candidates(s)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	prevEnd := -1
	for i, c := range cands {
		if c.CharStart < prevEnd {
			// Spans may be non-adjacent (particles skipped) but should not go backwards.
			t.Fatalf("span order broken at %d: prevEnd=%d cand=%+v", i, prevEnd, c)
		}
		if c.CharStart >= c.CharEnd {
			t.Fatalf("empty/inverted span: %+v", c)
		}
		prevEnd = c.CharEnd
	}
}
