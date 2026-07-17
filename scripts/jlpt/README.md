# JLPT embed map generator

Builds `internal/jlpt/words.json` used at Sentence extract to skip N5–N3 Words.

## Source

Primary: [jamsinclair/open-anki-jlpt-decks](https://github.com/jamsinclair/open-anki-jlpt-decks) MIT  
(`src/n1.csv` … `n5.csv`, lineage: tanos.co.uk community lists via earlier Anki decks)

JLPT has no official post-2010 word list; these are community estimates.

## Regenerate (offline once; not in `go test`)

```bash
mkdir -p /tmp/jlpt-src
for n in 1 2 3 4 5; do
  curl -fsSL "https://raw.githubusercontent.com/jamsinclair/open-anki-jlpt-decks/main/src/n${n}.csv" \
    -o "/tmp/jlpt-src/n${n}.csv"
done
go run ./scripts/jlpt -src /tmp/jlpt-src -out internal/jlpt/words.json
```

Multi-level collisions keep the **easiest** level (n5 wins over n1).
