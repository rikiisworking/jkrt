package analyze_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unicode/utf8"

	"github.com/rikiisworking/jkrt/internal/analyze"
)

func TestCandidatesFixtureSentence(t *testing.T) {
	a, err := analyze.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sentence := loadFixture(t, "keizai.txt")
	// Normalize trailing newline from file.
	if len(sentence) > 0 && sentence[len(sentence)-1] == '\n' {
		sentence = sentence[:len(sentence)-1]
	}

	cands, err := a.Candidates(sentence)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) == 0 {
		t.Fatal("expected at least one Word candidate")
	}

	// Expected kanji-bearing content words from 経済政策を発表した。
	// 経済, 政策, 発表 (する is split; し/た/を/。 are not candidates).
	wantLemmas := map[string]string{
		"経済": "ケイザイ",
		"政策": "セイサク",
		"発表": "ハッピョウ",
	}
	if len(cands) != len(wantLemmas) {
		t.Fatalf("candidate count: got %d want %d (%v)", len(cands), len(wantLemmas), cands)
	}
	got := make(map[string]string, len(cands))
	for _, c := range cands {
		if c.Reading == "" {
			t.Fatalf("candidate with empty reading: %+v", c)
		}
		if !analyze.ContainsKanji(c.Surface) {
			t.Fatalf("candidate without kanji: %+v", c)
		}
		if _, dup := got[c.Lemma]; dup {
			t.Fatalf("duplicate lemma in candidates: %s", c.Lemma)
		}
		got[c.Lemma] = c.Reading

		// Spans must be valid rune offsets into the sentence.
		runes := []rune(sentence)
		if c.CharStart < 0 || c.CharEnd > len(runes) || c.CharStart >= c.CharEnd {
			t.Fatalf("bad span for %+v (sentence runes=%d)", c, len(runes))
		}
		span := string(runes[c.CharStart:c.CharEnd])
		if span != c.Surface {
			t.Fatalf("span %q != surface %q for lemma %s", span, c.Surface, c.Lemma)
		}
	}

	for lemma, reading := range wantLemmas {
		if got[lemma] != reading {
			t.Errorf("lemma %s: want reading %q, got %q (all=%v)", lemma, reading, got[lemma], got)
		}
	}
	for lemma := range got {
		if _, ok := wantLemmas[lemma]; !ok {
			t.Errorf("unexpected lemma %q", lemma)
		}
	}

	// Particles / pure kana must not appear.
	for _, c := range cands {
		if c.Surface == "を" || c.Surface == "し" || c.Surface == "た" || c.Surface == "。" {
			t.Errorf("unexpected non-candidate surface kept: %+v", c)
		}
	}
}

func TestIsWordCandidateEmptyReadingSkipped(t *testing.T) {
	if analyze.IsWordCandidate("漢字", "") {
		t.Fatal("empty reading must not be a Word candidate")
	}
	if analyze.IsWordCandidate("漢字", "   ") {
		t.Fatal("whitespace-only reading must not be a Word candidate")
	}
	if !analyze.IsWordCandidate("漢字", "カンジ") {
		t.Fatal("kanji + reading should be a Word candidate")
	}
	if analyze.IsWordCandidate("ひらがな", "ヒラガナ") {
		t.Fatal("pure kana surface must not be a Word candidate")
	}
}

func TestContainsKanji(t *testing.T) {
	if !analyze.ContainsKanji("経済") {
		t.Fatal("expected kanji")
	}
	if analyze.ContainsKanji("する") {
		t.Fatal("hiragana only")
	}
	if analyze.ContainsKanji("") {
		t.Fatal("empty")
	}
}

func TestSplitSentences(t *testing.T) {
	in := "一文目。二文目！三文目？"
	got := analyze.SplitSentences(in)
	want := []string{"一文目。", "二文目！", "三文目？"}
	if len(got) != len(want) {
		t.Fatalf("len: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %q want %q", i, got[i], want[i])
		}
	}

	// No terminator → single sentence
	got = analyze.SplitSentences("句点なし")
	if len(got) != 1 || got[0] != "句点なし" {
		t.Fatalf("no terminator: %v", got)
	}

	if analyze.SplitSentences("   ") != nil {
		t.Fatal("blank should yield nil/empty")
	}
}

func TestCandidatesEmptyInput(t *testing.T) {
	a, err := analyze.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cands, err := a.Candidates("")
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected no candidates, got %v", cands)
	}
}

func TestSpanRuneNotByte(t *testing.T) {
	// Ensure we treat offsets as runes: multi-byte chars must not break surface.
	a, err := analyze.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := "経済。"
	cands, err := a.Candidates(s)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if utf8.RuneCountInString(s) != 3 {
		t.Fatalf("fixture rune count")
	}
	for _, c := range cands {
		runes := []rune(s)
		if string(runes[c.CharStart:c.CharEnd]) != c.Surface {
			t.Fatalf("rune span mismatch: %+v", c)
		}
	}
}

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	// testdata/analyze relative to repo root
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/analyze -> repo root
	root := filepath.Join(filepath.Dir(file), "..", "..")
	path := filepath.Join(root, "testdata", "analyze", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}
