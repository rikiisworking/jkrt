# AGENTS.md — Japanese Kanji Reading Trainer

## Project overview

- **Name**: Japanese Kanji Reading Trainer (JKRT)
- **Goal**: Accelerate N2 → N1 **reading** via real Japanese news: Scrape RSS → Sentences → **Words** (lemma + reading) → Anki-like **Card** Review in sentence context.
- **Target user**: Self (N2, Tokyo-based backend engineer).
- **Core value**: Automated exposure to kanji-bearing **words** in news. **No default furigana**; reveal on demand.
- **Language**: Domain terms in [`CONTEXT.md`](CONTEXT.md). Do not invent parallel vocabulary.

## Document authority (conflict order)

1. [`CONTEXT.md`](CONTEXT.md) — domain language  
2. [`docs/adr/`](docs/adr/) — why decisions stuck  
3. [`docs/sm2-spec.md`](docs/sm2-spec.md) — scheduler math (normative)  
4. [`DEVELOPMENT_PLAN.md`](DEVELOPMENT_PLAN.md) — schema, phases, HTTP, acceptance  
5. [`README.md`](README.md) — human summary only  

If something is unspecified, **do not invent product behavior** — implement the smallest thing consistent with the plan and leave a clear TODO only for env secrets/URLs that cannot be known offline.

## Agent workflow

1. Read `CONTEXT.md` terms; use them in code names and UI copy where natural.  
2. Implement **only the current phase** in `DEVELOPMENT_PLAN.md` (see Status). Do not start the next phase in the same change set unless the user explicitly asks.  
3. Prefer pure functions + table-driven tests for `internal/schedule` and parsing.  
4. **No live network in `go test`.** RSS/analyzer tests use `testdata/`.  
5. Match HTTP routes and status codes in the plan’s HTTP surface table.  
6. On phase complete: check boxes in the plan, update **Status** in the plan and **Current status** here, ensure `go test ./...` passes, then **stop**.  
7. After schema/scheduler/scrape behavior changes: update plan and/or `docs/sm2-spec.md` / ADR first (or in the same PR).

### DO NOT

- Track progress by **single kanji character** or add `user_kanji`-style tables  
- Add **goquery** or HTML article fetch in v1  
- Implement **FSRS**, Anki sync, decks, or note types  
- Grade the **whole Sentence** as one Review  
- Scrape publishers other than configured NHK sources  
- Hit the network inside unit/integration tests  
- Scaffold an **npm/Tailwind build** before Phase 4 (use **Tailwind CDN** in Phase 0–3)  
- Skip schedule tests “until UI exists”  
- Proceed past Phase 0 without working auth when `JKRT_AUTH=on`  
- Commit secrets, `.env`, or real `*.db` files  

## Architecture

- **Backend**: Go (Fiber) — scrape, analyze, schedule, review, auth, HTML/HTMX.  
- **Frontend**: HTMX + Tailwind (**CDN through Phase 3**). Templates/static via Fiber. No heavy SPA.  
- **Database**: SQLite (`modernc.org/sqlite`). Schema: `DEVELOPMENT_PLAN.md`.  
- **Deploy**: `go run ./cmd/server`. Cloudflare Tunnel only with auth on.  
- **External**: RSS only — NHK main + NHK Easy (always both on Scrape). Analyzer: **Kagome** (IPA) pure-Go.  
- **Scheduler**: [`docs/sm2-spec.md`](docs/sm2-spec.md) only.  
- **Security**: Password + **signed session cookie**; `JKRT_AUTH=off` for local only.

## Tech stack

- Go 1.23+ · Fiber · SQLite · RSS/XML parser (no goquery in v1)  
- `github.com/ikawaha/kagome/v2` (or current Kagome v2 module path) + IPA dict  
- HTMX + Tailwind CDN · Noto Sans JP · primary `#3B82F6`  
- Password hash: **bcrypt**  
- Tests: `go test` + httptest; fixtures under `testdata/`

## Development rules

- **Phases**: Follow `DEVELOPMENT_PLAN.md` strictly.  
- **Domain**: Word = lemma+reading; Card = SM-2 state; grades Again/Hard/Good/Easy.  
- **Code style**: Idiomatic Go, small functions, clear errors, no global mutable state.  
- **Testing**: Unit + integration per feature. Strong coverage on `internal/schedule`, `internal/analyze`, RSS parse.  
- **UI**: Highlight Unfamiliar Words; Review focuses one Word: `<strong class="unfamiliar">…</strong>`.

## Commands

```bash
go run ./cmd/server
go test ./... -v
golangci-lint run   # when configured
```

- DB: `migrations/` applied on startup.  
- Tunnel: Phase 5 docs; never expose with auth off.

## Phases (high level)

| Phase | Focus |
|-------|--------|
| 0 | Setup, health, static, minimal auth |
| 1 | Schema, Kagome analyze, words/cards |
| 2 | Dual NHK RSS scrape |
| 3 | Review UI + SM-2 |
| 4 | HTMX dashboard polish |
| 5 | Auth harden + tunnel guide |
| 6 | Stats, export, perf |

## Current status

**Phase 0 (Setup) — complete.** Fiber server, health, static placeholder, password auth. **Next: Phase 1** (schema, Kagome, words/cards).

## Safety

- User-triggered RSS only; respect feed terms.  
- Local data; no secrets in git.  
- Change domain/schedule/schema via docs first when behavior shifts.
