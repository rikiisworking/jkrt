# AGENTS.md - Japanese Kanji Reading Trainer

## Project Overview
- **Name**: Japanese Kanji Reading Trainer (JKRT)
- **Goal**: Web app to accelerate N2 → N1 Japanese reading proficiency by extracting unfamiliar kanji from real Japanese news articles. Focus on sentence-level exposure, spaced review, and minimal friction.
- **Target Users**: Self (N2 level, Tokyo-based backend engineer).
- **Core Value**: Efficient, automated kanji familiarization via news content. No default furigana; reveal on review.

## Architecture
- **Backend**: Go (Fiber) – REST API for scraping, kanji analysis, user data, auth.
- **Frontend**: Recommended – HTMX + Tailwind CSS (or minimal Svelte/Astro for interactivity). Static HTML/JS where possible for simplicity. Serve via Fiber.
- **Database**: SQLite (embedded, file-based) for portability on Mac. Schema: users, kanji_vocab (with sentences, familiarity score), reviews, news_sources.
- **Deployment**: Local Mac (go run). Cloudflare Tunnel (free) for iPhone access (HTTPS, no port forwarding).
- **External Services**:
  - Japanese news RSS/API (e.g., NHK, Asahi, Yahoo Japan – respect robots.txt, rate limits).
  - Kanji decomposition / JLPT data: Use local dictionary (e.g., JMdict, KanjiDB JSON) or lightweight lib like `github.com/ikawaha/kana` + custom kanji parser.
- **Security**: Simple token-based auth (JWT or session cookie with password). No user accounts beyond single user.

## Tech Stack Details
- Go 1.23+ with Fiber, SQLite (modernc.org/sqlite), goquery for scraping.
- Frontend: Tailwind + HTMX for dynamic updates. Noto Sans JP via Google Fonts or local.
- Color: Primary #3B82F6 (blue), sans-serif.
- Testing: Go test with httptest. Integration tests for scraping/analysis.

## Development Rules
- **Phases**: Strict multi-phase. Complete one phase + tests before next. Use Plan Mode for complex changes.
- **Code Style**: Clean, idiomatic Go. Small functions, comprehensive error handling. RESTful routes. No global state.
- **Testing Mandate**: Unit + integration tests for every feature. 80%+ coverage. Mock external calls.
- **Performance**: Lightweight. Cache scraped articles. Efficient kanji detection (Unicode ranges + freq lists).
- **Japanese Handling**: Use libraries for kana/kanji. Bold unfamiliar kanji in UI (e.g., <strong class="unfamiliar">漢字</strong>).
- **Avoid**: Over-engineering. Heavy JS frameworks unless needed. Real-time without necessity.

## Commands
- Build/Run: `go run main.go`
- Test: `go test ./... -v`
- Lint: `golangci-lint run`
- DB Init: Migration script.
- Tunnel: `cloudflared tunnel run` (configure separately).

## Phases (High-Level)
See DEVELOPMENT_PLAN.md for detailed breakdown.

## Current Status
Phase 0 (Setup) – In Progress. Follow DEVELOPMENT_PLAN.md sequentially.

## Safety / Best Practices
- Respect news site terms (user-triggered scraping only).
- Local-only data storage.
- Simple password/token – rotate if exposed.
- Plan before touching auth, DB schema, or scraping logic.