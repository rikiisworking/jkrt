# Development Plan - Japanese Kanji Reading Trainer

## Overall Goals
- Local Mac web app (Fiber) accessible via iPhone (Cloudflare Tunnel).
- Automated news scraping → sentence extraction → unfamiliar kanji detection → review system.
- N2+ content, N1 target.
- Minimal UI: Modern, blue theme, simple.

## Phases (Sequential, with Milestones & Tests)

### Phase 0: Project Setup (Current)
- [ ] Initialize Go module, Fiber skeleton.
- [ ] Add dependencies: fiber, sqlite, goquery, etc.
- [ ] Basic server with /health, static file serving.
- **Tests**: go test for basic routes.
- **Deliverable**: Running server on localhost:8080.

### Phase 1: Data Layer & Kanji Detection
- SQLite schema: kanji (char, readings, jlpt), user_kanji (familiarity), sentences (text, source, kanji_ids).
- Kanji analyzer: Function to detect "unfamiliar" (not in user known set or low freq).
- Local dictionary seed (JSON embed or file).
- **Tests**: Unit tests for kanji parsing, familiarity logic. Integration for DB CRUD.

### Phase 2: News Scraping
- User-triggered endpoint (/scrape) to fetch recent news (RSS + goquery fallback).
- Parse into sentences (simple splitter).
- Filter N2+ level (heuristic: kanji density).
- Store raw articles/sentences.
- **Tests**: Mocked scraping tests. Rate limit handling. Error on failure.

### Phase 3: Review & Learning Core
- /review endpoint: Show sentences with bold unfamiliar kanji.
- Mark as "known" / "review later" → update familiarity, store for SRS-like review.
- Review queue: Spaced (simple interval) or manual.
- Toggle furigana on review.
- **Tests**: Full flow tests (scrape → detect → review → DB update).

### Phase 4: Frontend & UX
- HTMX-driven UI: Dashboard, news feed, review sessions.
- Design: Blue theme, Noto Sans JP, responsive (mobile-first for iPhone).
- Sentence view with bold kanji, click for details.
- **Tests**: Basic UI rendering tests if using templates.

### Phase 5: Auth & Security
- Simple password login → token.
- Protect all routes.
- Cloudflare Tunnel setup guide.
- **Tests**: Auth middleware tests.

### Phase 6: Polish, Export, Polish
- Stats (kanji learned, reading speed estimate).
- Export reviews.
- Performance tuning, mobile optimizations.
- Documentation.

**Milestone Criteria**: Each phase ends with passing tests + manual verification. Commit after each.

**Risks & Mitigations**:
- Scraping changes: Fallbacks + user manual input option.
- Kanji accuracy: Seed with reliable JLPT lists.
- Performance: Limit article size.

Update this file with progress.