# Japanese Kanji Reading Trainer

Personal app that turns real Japanese news into sentence-level review of **unfamiliar word readings**, not isolated characters.

## Language

**Word**:
A vocabulary unit the learner trains. Identity is **lemma + Reading** (not surface form alone). The atomic unit of progress and review outcomes. Words are proposed by a morphological analyzer, not by per-character scans.
_Avoid_: Kanji (as the progress unit), character, surface form (as the identity)

**Lemma**:
The dictionary / base form of a Word as produced by the analyzer (e.g. 発表する vs inflected 発表した). Part of Word identity with Reading.
_Avoid_: Surface form, stem (informal)

**Surface form**:
The exact text span as it appears in a Sentence (e.g. 発表した). Stored on the occurrence in context; not the Card/Word identity key.
_Avoid_: Lemma, Word identity

**Token**:
A single morphological unit emitted by the analyzer for a span of a Sentence. A Token may become a Word candidate (e.g. content words with kanji); particles and pure-kana function words usually do not.
_Avoid_: Word (Token is the raw analyzer output; Word is the learning unit)

**Reading**:
How a Word is pronounced (kana). Part of Word identity with Lemma. Comes from the morphological analyzer in v1. The skill being trained; not shown by default in the UI (furigana toggle reveals it).
_Avoid_: Furigana (UI mechanism for showing a Reading), romanization, separate JLPT dictionary (not required for v1 identity or gloss)

**Sentence**:
A single clause/unit of article text used as review context. Contains one or more Words.
_Avoid_: Article (whole piece), passage

**Article**:
One RSS item turned into stored text (title + feed body/summary fields available), then split into Sentences.
_Avoid_: Story, full webpage, HTML document

**Source**:
A configured **RSS feed** the learner may Scrape (user-triggered). Built-in Sources include **NHK main**, **NHK Easy** (optional URL), plus other public Japanese RSS defaults (Yahoo! major topics, ITmedia NEWS, BBC Japanese) with hardcoded feed URLs in `internal/scrape`. Scrape always pulls **all** configured Sources. Still **RSS only** — no HTML page crawl.
_Avoid_: Scraper (the mechanism), HTML site crawl, paywalled full-article fetch

**Word candidate**:
A Token that contains at least one kanji **and** has a non-empty Reading. Only Word candidates enter the learning model as Words. Pure-kana Tokens and Tokens with empty readings are ignored in v1.
_Avoid_: Every token, vocabulary list item (pre-seeded lists are optional metadata, not the candidate rule)

**Card**:
The schedulable learning record for one Word (lemma + Reading) for the Learner. Holds SM-2-ish state (phase, interval, ease, due time). Created when a Sentence is **extracted** (learner opt-in), not on Scrape; queue caps still limit how many new Cards enter a session.
_Avoid_: Anki note (we have no multi-field note types), character progress row, familiarity ladder (retired)

**Sentence extract** (opt-in):
Learner action on an Article’s Sentence (tap “Add to review”) that analyzes that Sentence and creates Words / occurrences / Cards for its Word candidates. Idempotent re-tap does not reset existing Card schedules. Untapped Sentences never enter the Review queue.
_Avoid_: Grade whole Sentence, auto-extract on Scrape, soft link into a full pre-built deck

**Learning phase**:
Early Card state: short re-shows the same day (learning steps) before the Card graduates to review intervals. v1 default steps: **1 minute, then 10 minutes**.
_Avoid_: New only, graduated (use Review phase for post-graduation)

**Review phase**:
Card state after graduating from learning steps; intervals are in days and grow with successful grades. v1 defaults: Good graduates to **1 day**; Easy graduating interval **4 days**; starting ease **2.5**. Again in review = lapse back through learning steps with interval reduction.
_Avoid_: Learning phase

**Ease**:
SM-2 ease factor on a Card; adjusted by grade (Hard/Good/Easy). Affects how fast intervals grow. v1 starting ease **2.5** (classic Anki-ish).
_Avoid_: Familiarity score (v1 scheduling uses SM-2 fields, not a 0–5 ladder)

**Unfamiliar Word**:
A Word highlighted in Sentence context when its Card matches: phase is new/learning/relearning, or due now, or (review phase and interval under 21 days). Not defined by JLPT level. Review still grades one Word/Card at a time.
_Avoid_: Unknown kanji, hard character, N1 word

**Review queue**:
The ordered list of upcoming Card Reviews. Due Cards first, then new Cards, subject to **UTC-day** caps. Cards exist only after **Sentence extract**; a fresh Scrape alone cannot flood the queue. v1 defaults: **20 new Cards per day** (first grade introduces a new Card), **40 Reviews per UTC day** (`SessionLimit` name kept for config continuity; not an ephemeral sitting id).
_Avoid_: All unfamiliar words at once, deck (product metaphor only — we are not Anki)

**Review result** (grade):
One of **Again**, **Hard**, **Good**, **Easy** on a single Card/Word. Drives Anki-like SM-2 scheduling (learning steps + graduating intervals + ease).
_Avoid_: later, known (retired simple ladder), pass/fail, letter grade

**Review**:
One learner judgment on a **single Word** (its Card), shown in the context of a Sentence. Updates that Card’s schedule only — not every Unfamiliar Word in the Sentence. When multiple Sentences contain the Word, v1 shows the **newest** occurrence as context. The persisted Review records the **Sentence that was shown** (not re-resolved later if a newer occurrence appears).
_Avoid_: Study session (the whole sitting), quiz, sentence grade (v1 does not grade the whole Sentence)

**Scrape**:
A user-triggered fetch that **always pulls every configured Source** (built-in multi-publisher RSS list) into **Articles and Sentences only** (reading library). Does **not** create Words/Cards. RSS only — title/description/content fields from each feed item. No HTML page fetch, no goquery article scrape. No per-feed picker in v1 (partial success per source is OK). Study entry is **Sentence extract** from the Articles UI.
_Avoid_: Crawl, HTML scrape, auto Card creation, sync, import (unless manual paste), selective single-feed scrape (v1)

**Learner**:
The single person using the app (you). Access is password-gated before any public Tunnel; local dev may disable auth explicitly.
_Avoid_: User account (multi-user product), team

## Explicit non-terms (v1 direction)

Progress is **not** tracked per single kanji character. Single-character Words may exist when the surface form is one kanji, but the model is Word-based.
