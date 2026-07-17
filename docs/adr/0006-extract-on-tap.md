# Extract-on-tap: Cards only after learner opts a Sentence into study

**Status:** Accepted  
**Date:** 2026-07-17

## Context

v1 initially created **Words + Cards for every Word candidate on Scrape**. That floods the Review queue with news volume and removes learner control over what enters the deck.

Soft opt-in (tap sentence only to jump into an already-full queue) was rejected: Cards would still exist for every scraped sentence.

## Decision

1. **Scrape** stores **Articles + Sentences only** (reading library). No Cards, no `sentence_words` from scrape.
2. **Sentence extract** (user tap on article detail) runs Kagome on that Sentence and creates Words / `sentence_words` / Cards (`schedule.NewCard`).
3. **Re-extract** is idempotent; existing Card SM-2 state is never reset (`ON CONFLICT DO NOTHING` on cards).
4. Review still grades **one Word/Card** at a time (not the whole Sentence).

## Consequences

- After scrape, due/new can stay empty until the learner adds sentences from **Articles**.
- Dashboard/empty-queue copy must point to Articles, not only “scrape then review.”
- `sentences.extracted_at` marks study opt-in (including kana-only sentences with zero candidates).
- Manual/test helper `IngestText` may still store + extract all sentences for convenience; the **user scrape path does not**.

## Alternatives considered

| Option | Why not |
|--------|---------|
| Soft opt-in only | Does not stop deck flood |
| Extract whole article on one button | Larger blast radius; can add later |
| Pre-analyze all sentences without Cards | Extra work; not required for v1 tap |
