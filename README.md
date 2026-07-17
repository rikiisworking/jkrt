# Japanese Kanji Reading Trainer (JKRT)

Personal web app for **N2 → N1 reading**: pull **NHK main + NHK Easy RSS**, extract **words** (lemma + reading), and review them with **Anki-like SM-2** scheduling in real sentence context.

> **Status:** Phase 6 complete — stats, export, performance (v1 phases 0–6 done).  
> See [`DEVELOPMENT_PLAN.md`](DEVELOPMENT_PLAN.md) and [`CONTEXT.md`](CONTEXT.md).  
> Architecture: pure `schedule` + deep `review` ([ADR 0005](docs/adr/0005-pure-schedule-deep-review.md)).  
> Ops: [Auth, cookies, Cloudflare Tunnel](docs/auth-and-tunnel.md).

## Features (shipped / planned)

- [x] Local Go server with password auth (HMAC session cookie)
- [x] Morphological analysis → kanji-bearing **Words** + Card rows
- [x] User-triggered scrape of **both** NHK feeds (RSS only — no HTML page scrape)
- [x] Review one Word at a time (Again / Hard / Good / Easy) with SM-2 scheduling
- [x] Sentence context with unfamiliar words highlighted; furigana on toggle (default off)
- [x] Dashboard / browse polish (Phase 4)
- [x] Auth harden + password rotate + tunnel guide (Phase 5)
- [x] Stats API, JSON/CSV export, indexes + size limits (Phase 6)

## Tech

| Layer | Choice |
|-------|--------|
| Backend | Go + Fiber |
| DB | SQLite (`modernc.org/sqlite`) |
| Frontend | HTMX + Tailwind CDN (Phase 0–4) |
| Schedule | SM-2-ish (not Anki sync) |
| Deploy | Local + cloudflared (**auth on**) |

## Docs

| File | Role |
|------|------|
| [`CONTEXT.md`](CONTEXT.md) | Domain glossary |
| [`DEVELOPMENT_PLAN.md`](DEVELOPMENT_PLAN.md) | Phases, schema, HTTP, acceptance |
| [`docs/sm2-spec.md`](docs/sm2-spec.md) | SM-2 scheduler (normative) |
| [`docs/auth-and-tunnel.md`](docs/auth-and-tunnel.md) | Cookie/TTL, password rotate, tunnel |
| [`docs/adr/`](docs/adr/) | Architecture decisions |
| [`AGENTS.md`](AGENTS.md) | Agent workflow + conventions |

## Quick start

### Local dev (auth off)

```bash
export JKRT_AUTH=off
go run ./cmd/server
# http://localhost:8080/health     → {"status":"ok"}
# http://localhost:8080/           → dashboard (due/new, scrape, links)
# http://localhost:8080/review     → next Card or empty queue
# http://localhost:8080/articles   → browse Articles / Sentences
# POST /api/scrape                 → dual NHK RSS ingest (live network)
# GET  /api/stats                  → queue + library JSON
# GET  /api/export?format=json     → full snapshot download
# GET  /api/export?format=csv      → Cards CSV download
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
| `JKRT_ADDR` | `:8080` | Prefer `127.0.0.1:8080` when tunneling |
| `JKRT_DB_PATH` | `./jkrt.db` | SQLite (full schema via `migrations/`) |
| `JKRT_AUTH` | `on` | **`off` only for local dev — never with a tunnel** |
| `JKRT_PASSWORD` | — | Bootstrap user 1 if no row yet |
| `JKRT_SESSION_SECRET` | — | Required when auth on (≥32 bytes) |
| `JKRT_SESSION_TTL` | `168h` | Cookie + signed payload lifetime |
| `JKRT_NHK_MAIN_RSS_URL` | NHK main cat0 | Override main feed |
| `JKRT_NHK_EASY_RSS_URL` | *(empty)* | Set when a live Easy RSS is known |

### Session cookie (summary)

- Name: `jkrt_session` — HMAC-signed; **HttpOnly**, **SameSite=Lax**
- **Secure** when HTTPS / `X-Forwarded-Proto: https` (Tunnel)
- TTL: `JKRT_SESSION_TTL` (default 7 days); expired cookies → 302/401 like no cookie  
Full detail: [`docs/auth-and-tunnel.md`](docs/auth-and-tunnel.md).

### Password rotate

Bootstrap does not change an existing password. Rotate without wiping Cards:

```bash
go run ./cmd/setpassword -db ./jkrt.db
```

To invalidate all sessions: change `JKRT_SESSION_SECRET` and restart. See [`docs/auth-and-tunnel.md`](docs/auth-and-tunnel.md).

### Scrape (auth off, local)

```bash
export JKRT_AUTH=off
go run ./cmd/server
curl -sS -X POST http://127.0.0.1:8080/api/scrape
# → {"sources":[{"name":"nhk_main","ok":true,"items_new":N},{"name":"nhk_easy","ok":false,"items_new":0,"error":"feed URL not configured"}]}
# Easy soft-fails until JKRT_NHK_EASY_RSS_URL is set. Main needs network.
```

## Cloudflare Tunnel (phone)

**Never run a tunnel with `JKRT_AUTH=off`.**

```bash
export JKRT_AUTH=on
export JKRT_PASSWORD='…'   # first bootstrap only if needed
export JKRT_SESSION_SECRET="$(openssl rand -hex 32)"
export JKRT_ADDR=127.0.0.1:8080
go run ./cmd/server

# separate terminal — quick ephemeral HTTPS URL (still requires login)
cloudflared tunnel --url http://127.0.0.1:8080
```

Use a named Cloudflare tunnel + your hostname for daily iPhone access. Checklist and rotate notes: [`docs/auth-and-tunnel.md`](docs/auth-and-tunnel.md).
