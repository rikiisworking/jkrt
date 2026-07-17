# Japanese Kanji Reading Trainer (JKRT)

Personal web app for **N2 → N1 reading**: pull **NHK main + NHK Easy RSS**, extract **words** (lemma + reading), and review them with **Anki-like SM-2** scheduling in real sentence context.

> **Status:** Phase 2 complete — dual NHK RSS scrape + ingest.  
> Phase 3 architecture locked ([ADR 0005](docs/adr/0005-pure-schedule-deep-review.md)): pure `schedule` + deep `review` — implementation next.  
> See [`DEVELOPMENT_PLAN.md`](DEVELOPMENT_PLAN.md) and [`CONTEXT.md`](CONTEXT.md).

## Features (shipped / planned)

- [x] Local Go server with password auth (HMAC session cookie)
- [x] Morphological analysis → kanji-bearing **Words** + Card rows
- [x] User-triggered scrape of **both** NHK feeds (RSS only — no HTML page scrape)
- [ ] Review one Word at a time (Again / Hard / Good / Easy)
- [ ] Sentence context with unfamiliar words highlighted; furigana on toggle
- [ ] iPhone via Cloudflare Tunnel (auth required)

## Tech

| Layer | Choice |
|-------|--------|
| Backend | Go + Fiber |
| DB | SQLite (`modernc.org/sqlite`) |
| Frontend | HTMX + Tailwind CDN (Phase 0–3) |
| Schedule | SM-2-ish (not Anki sync) |
| Deploy | Local + cloudflared |

## Docs

| File | Role |
|------|------|
| [`CONTEXT.md`](CONTEXT.md) | Domain glossary |
| [`DEVELOPMENT_PLAN.md`](DEVELOPMENT_PLAN.md) | Phases, schema, HTTP, acceptance |
| [`docs/sm2-spec.md`](docs/sm2-spec.md) | SM-2 scheduler (normative) |
| [`docs/adr/`](docs/adr/) | Architecture decisions |
| [`AGENTS.md`](AGENTS.md) | Agent workflow + conventions |

## Quick start

### Local dev (auth off)

```bash
export JKRT_AUTH=off
go run ./cmd/server
# http://localhost:8080/health  → {"status":"ok"}
# http://localhost:8080/        → placeholder HTML
```

### Auth on (required before any public tunnel)

```bash
export JKRT_AUTH=on
export JKRT_PASSWORD='your-password'
export JKRT_SESSION_SECRET="$(openssl rand -hex 32)"  # ≥32 bytes
go run ./cmd/server
# GET /        → 302 /login without cookie
# POST /login  → sets jkrt_session cookie
```

Copy [`.env.example`](.env.example) for the full env list. Do not commit `.env` or `*.db`.

### Tests

```bash
go test ./... -count=1
```

## Config

| Env | Default | Notes |
|-----|---------|--------|
| `JKRT_ADDR` | `:8080` | Listen address |
| `JKRT_DB_PATH` | `./jkrt.db` | SQLite (full schema via `migrations/`) |
| `JKRT_AUTH` | `on` | `off` only for local dev |
| `JKRT_PASSWORD` | — | Bootstrap user 1 if no row yet |
| `JKRT_SESSION_SECRET` | — | Required when auth on (≥32 bytes) |
| `JKRT_SESSION_TTL` | `168h` | Cookie lifetime |
| `JKRT_NHK_MAIN_RSS_URL` | NHK main cat0 | Override main feed |
| `JKRT_NHK_EASY_RSS_URL` | *(empty)* | Set when a live Easy RSS is known |

### Scrape (auth off, local)

```bash
export JKRT_AUTH=off
go run ./cmd/server
curl -sS -X POST http://127.0.0.1:8080/api/scrape
# → {"sources":[{"name":"nhk_main","ok":true,"items_new":N},{"name":"nhk_easy","ok":false,"items_new":0,"error":"feed URL not configured"}]}
# Easy soft-fails until JKRT_NHK_EASY_RSS_URL is set. Main needs network.
```
