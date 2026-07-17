# Japanese Kanji Reading Trainer (JKRT)

Personal web app for **N2 → N1 reading**: pull **NHK main + NHK Easy RSS**, extract **words** (lemma + reading), and review them with **Anki-like SM-2** scheduling in real sentence context.

> **Status:** Phase 0 (setup). Docs and domain model only — Go server not implemented yet.  
> See [`DEVELOPMENT_PLAN.md`](DEVELOPMENT_PLAN.md) and [`CONTEXT.md`](CONTEXT.md).

## Planned features

- User-triggered scrape of **both** NHK feeds (RSS only — no HTML page scrape)
- Morphological analysis → kanji-bearing **Words**
- Review one Word at a time (Again / Hard / Good / Easy)
- Sentence context with unfamiliar words highlighted; furigana on toggle
- Local use + iPhone via Cloudflare Tunnel (password auth)

## Tech

| Layer | Choice |
|-------|--------|
| Backend | Go + Fiber |
| DB | SQLite |
| Frontend | HTMX + Tailwind |
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

## Quick start (after Phase 0)

```bash
go run ./cmd/server
# http://localhost:8080/health
```

Until Phase 0 is done, there is nothing to run.
