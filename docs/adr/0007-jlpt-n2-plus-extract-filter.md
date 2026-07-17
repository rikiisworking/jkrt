# JLPT N2+ filter on Sentence extract

**Status:** Accepted  
**Date:** 2026-07-17

## Context

Extract-on-tap (ADR 0006) still created Cards for every kanji Word candidate. For an N2→N1 reading goal, N5–N3 volume floods the Review queue.

## Decision

1. At **Sentence extract**, create `sentence_words` + **Cards** only for Words whose vocab level is **N2 or N1**.
2. **Word-level** filter (lemma + reading), not character-level `user_kanji` progress.
3. **Embedded map** from community open-anki JLPT CSVs (MIT; lineage tanos.co.uk estimates — JLPT has no official post-2010 word list). Generator: `scripts/jlpt/`. Multi-level collisions keep the **easiest** level.
4. **Unlisted** Words: optional **Grok Build headless** classifier (`composer-2.5` by default), results cached in `word_jlpt_levels`. Fail closed (skip) when classifier off, missing, or error.
5. Resolve map/cache/classifier **before** the extract transaction (SQLite `MaxOpenConns(1)` deadlock otherwise).

## Consequences

- Common easy Words no longer enter the deck automatically.
- News jargon often unlisted → classifier or skip depending on `JKRT_JLPT_CLASSIFY`.
- Tests that need every candidate as a Card call `DB.AllowAllWords()`.

## Non-goals

- Official JLPT syllabus (does not exist as a free list)
- Runtime network inside `go test`
- Character progress tables
