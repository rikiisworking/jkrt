// Command gen builds internal/jlpt/words.json from open-anki JLPT CSVs.
// Offline only — not invoked by go test. See scripts/jlpt/README.md.
//
// Usage (from repo root):
//
//	go run ./scripts/jlpt -src /path/to/csvs -out internal/jlpt/words.json
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// entry is the compact embed row.
type entry struct {
	L string `json:"l"` // lemma / expression
	R string `json:"r"` // reading
	V string `json:"v"` // n5..n1
}

// easier ranks n5 highest (easiest).
func easier(a, b string) string {
	rank := map[string]int{"n5": 5, "n4": 4, "n3": 3, "n2": 2, "n1": 1}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}

func splitAlts(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// Prefer fullwidth semicolon used in the open-anki decks.
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '；' || r == ';'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{s}
	}
	return out
}

func hasKanji(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func main() {
	srcDir := flag.String("src", "", "directory with n1.csv..n5.csv")
	outPath := flag.String("out", "internal/jlpt/words.json", "output JSON path")
	flag.Parse()
	if *srcDir == "" {
		fmt.Fprintln(os.Stderr, "-src required")
		os.Exit(2)
	}

	// key lemma\x00reading → easiest level
	m := map[string]string{}

	levels := []struct {
		file  string
		level string
	}{
		{"n5.csv", "n5"},
		{"n4.csv", "n4"},
		{"n3.csv", "n3"},
		{"n2.csv", "n2"},
		{"n1.csv", "n1"},
	}

	for _, lv := range levels {
		path := filepath.Join(*srcDir, lv.file)
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open %s: %v\n", path, err)
			os.Exit(1)
		}
		r := csv.NewReader(f)
		r.LazyQuotes = true
		r.FieldsPerRecord = -1
		rows, err := r.ReadAll()
		_ = f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "csv %s: %v\n", path, err)
			os.Exit(1)
		}
		for i, row := range rows {
			if i == 0 && len(row) > 0 && row[0] == "expression" {
				continue
			}
			if len(row) < 2 {
				continue
			}
			exprs := splitAlts(row[0])
			reads := splitAlts(row[1])
			if len(exprs) == 0 || len(reads) == 0 {
				continue
			}
			// Cartesian for multi-expression / multi-reading rows is rare; pair by index when lengths match.
			pairs := [][2]string{}
			if len(exprs) == len(reads) {
				for i := range exprs {
					pairs = append(pairs, [2]string{exprs[i], reads[i]})
				}
			} else {
				for _, e := range exprs {
					for _, rd := range reads {
						pairs = append(pairs, [2]string{e, rd})
					}
				}
			}
			for _, pr := range pairs {
				lemma, reading := strings.TrimSpace(pr[0]), strings.TrimSpace(pr[1])
				if lemma == "" || reading == "" {
					continue
				}
				// Skip pure decorative prefix rows like "～円" only — keep if has kanji elsewhere.
				if strings.HasPrefix(lemma, "～") && !hasKanji(strings.TrimPrefix(lemma, "～")) {
					continue
				}
				add := func(l, r string) {
					key := l + "\x00" + r
					if prev, ok := m[key]; ok {
						m[key] = easier(prev, lv.level)
					} else {
						m[key] = lv.level
					}
				}
				add(lemma, reading)
				// Kagome often yields する-verb base without する.
				if strings.HasSuffix(lemma, "する") && len([]rune(lemma)) > 2 {
					base := strings.TrimSuffix(lemma, "する")
					if hasKanji(base) {
						add(base, strings.TrimSuffix(reading, "する"))
					}
				}
			}
		}
	}

	entries := make([]entry, 0, len(m))
	for k, v := range m {
		parts := strings.SplitN(k, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		entries = append(entries, entry{L: parts[0], R: parts[1], V: v})
	}
	// Stable-ish order by lemma then reading (optional — map iteration is fine for regen).
	// Encode compact.
	data, err := json.Marshal(entries)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d entries to %s\n", len(entries), *outPath)
}
