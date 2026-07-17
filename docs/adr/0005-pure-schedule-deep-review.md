# Pure schedule + deep Review module

SM-2 math and new-Card seed values live in **`internal/schedule`** as pure functions (`Params`/`DefaultParams`, `NewCard`, `Apply`, `IsUnfamiliar`; id-free card state; G1–G9). Learner-facing Review (queue, newest Sentence context, presentation payload, grade + persist) lives in **`internal/review`** behind a small interface: **next** and **grade**. HTTP stays thin.

**Scrape vs Card write path (updated by ADR 0006):** Scrape uses **`db.StoreArticle`** (Articles + Sentences only). **`schedule.NewCard`** is applied when the learner opts a Sentence into study via **`db.ExtractSentence`** / `persistCandidatesTx` (or test helper `IngestText` which store+extracts all). Do not re-fork Card defaults in SQL.

**Considered:** (1) schedule pure + review package (chosen); (2) Review methods on `db` only; (3) one package with schedule unexported inside review. Rejected a `ReviewStore` interface until a second adapter exists. Queue **selection** stays SQL inside review (caps/defaults still from `schedule.Params`); pure queue helpers can be promoted later without changing next/grade.

**Consequences:** `db` must not re-fork ease/phase/unfamiliar rules; extract imports schedule. Mature Card counts use `schedule.Params.ComfortableIntervalDays` via `DB.SetScheduleParams` (same knobs as Unfamiliar). HTTP dashboard/stats/export compose queue+library once through `snapshot.Load` — not three copy-pasted handler blocks. grade accepts `card_id` + `grade` + `sentence_id` (context shown). grade does not return the next Card — caller calls next again. Empty queue is a normal empty result, not an error.
