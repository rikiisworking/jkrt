# Japanese Kanji Reading Trainer

Web app for N2 → N1 reading improvement via real Japanese news.

## Features
- Trigger news scrape.
- Auto-detect unfamiliar kanji.
- Sentence reviews with bold kanji.
- Simple SRS-style tracking.
- Local + iPhone access via Cloudflare.

## Quick Start
1. `go mod tidy`
2. `go run main.go`
3. Access http://localhost:8080
4. Cloudflare Tunnel for remote.

See DEVELOPMENT_PLAN.md and AGENTS.md.

## Tech
- Backend: Go + Fiber
- DB: SQLite
- Frontend: HTMX + Tailwind