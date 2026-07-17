# RSS-only ingest from NHK main and NHK Easy

v1 content comes **only from RSS** (title + feed description/content fields). One Scrape always targets **both** NHK main and NHK Easy sources. No HTML article fetch and no goquery in v1 — scrapers against full pages are fragile and expand ToS/ops surface.

**Defaults:** Main feed URL pinned in `DEVELOPMENT_PLAN.md` (`news.web.nhk` cat0 RSS, verified 2026-07-17). Easy had **no verified public RSS** at that date; tests use fixtures; live Easy URL is config when available; soft-fail Easy without inventing HTML/JSON substitutes.

**Considered:** single feed; HTML body fetch; multi-publisher; RSS-only dual NHK (chosen).

**Consequences:** Sentence text may be shorter than full articles; Easy may lag until a feed URL is confirmed; reopening HTML or non-RSS JSON is a new decision, not a silent expansion.
