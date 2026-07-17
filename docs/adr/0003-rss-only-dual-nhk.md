# RSS-only ingest (multi-publisher defaults)

Content comes **only from RSS** (title + feed description/content fields). One Scrape always targets **every configured Source** (no per-feed picker). No HTML article fetch and no goquery — scrapers against full pages are fragile and expand ToS/ops surface.

**History:** v1 shipped as dual NHK (main + Easy). Later expansion added more public Japanese RSS defaults with the same hardcoded-URL pattern as NHK main (`yahoo_topics`, `itmedia_news`, `bbc_japanese` in `internal/scrape.DefaultSources`).

**Defaults:** NHK main URL pinned (`news.web.nhk` cat0 RSS, verified 2026-07-17); Easy optional via env; other publishers use hardcoded public RSS URLs in code. Partial success per source is OK. Soft-fail empty Easy without inventing HTML/JSON substitutes.

**Considered:** single feed; HTML body fetch; dual NHK only; multi-publisher RSS (current).

**Consequences:** Sentence text may be shorter than full articles; some feeds are title-heavy; feed outages are isolated; adding/removing a Source is a small code change in `DefaultSources` (or later env list if needed).
