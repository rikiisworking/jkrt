# Development Plan — Japanese Kanji Reading Trainer (JKRT)

## Overall goals

- Local Mac web app (Go + Fiber) usable on iPhone via Cloudflare Tunnel (HTTPS).
- Pipeline: user-triggered **RSS Scrape** (library only) → Articles/Sentences → learner **Sentence extract** → **Word**/Card → sentence-context Review.
- Target: real Japanese news density from several public RSS feeds; learning goal N1-oriented **reading** of kanji-bearing words.
- UX: minimal friction, mobile-first, blue theme, **no default furigana** (reveal on demand).
- Single Learner. Local SQLite. User-triggered scrape only. **RSS only** (no HTML article fetch).

| Doc | Role |
|-----|------|
| [`CONTEXT.md`](CONTEXT.md) | Domain language |
| [`docs/adr/`](docs/adr/) | Hard decisions |
| [`docs/sm2-spec.md`](docs/sm2-spec.md) | **Normative** SM-2 math + golden tests |
| [`AGENTS.md`](AGENTS.md) | Agent workflow + DO NOT |

---

## Principles

1. **One phase at a time** — finish checklist + acceptance before the next.
2. **Word-based learning** — never track progress by single kanji character (ADR 0001).
3. **Thin UI early** — Phase 3 ships usable Review; Phase 4 polishes dashboard.
4. **Mock external I/O** — RSS tests use `testdata/`; no live network in CI.
5. **Update this file** — check boxes and bump Status when a phase completes.
6. **Schema here is canonical** — code must match.
7. **Scheduler only from `docs/sm2-spec.md`** — no improvised intervals.

---

## Status

| Field | Value |
|-------|--------|
| **Current phase** | Complete through Phase 6 (v1 milestone) |
| **Repo state** | Phase 6 **complete**: `GET /api/stats`, `GET /api/export?format=json\|csv`, dashboard library/phase numbers + export links, `migrations/002_perf.sql` indexes, raw_text size limit, export fixture tests. Acceptance: `go test ./...` green. |
| **Last updated** | 2026-07-17 |
| **Agent-ready** | Yes — no further planned phase; only explicit user asks for new work |

---

## Domain summary (normative)

Full definitions: `CONTEXT.md`. Do not contradict:

| Concept | Rule |
|---------|------|
| **Word** | Identity = **lemma + reading** |
| **Word candidate** | Analyzer Token with ≥1 kanji **and non-empty reading** |
| **Empty reading** | **Skip** Token — no Word, no Card |
| **Card** | One per Word; created **on extract**; fields per sm2-spec |
| **New card due** | `phase=new`, `due_at=now` on create; **queue caps** limit showing |
| **Review** | One Word/Card; Sentence context = **newest** occurrence |
| **Grades** | `again` \| `hard` \| `good` \| `easy` |
| **Scheduler** | [`docs/sm2-spec.md`](docs/sm2-spec.md) only |
| **Unfamiliar highlight** | See locked predicate below |
| **Queue** | Due first, then new; 20 new/day (first grade), 40 reviews/UTC day (`SessionLimit`) |
| **Sources** | Multi-publisher RSS defaults (`DefaultSources`) |
| **Scrape** | Always all configured; RSS → Articles/Sentences only (no Cards) |
| **Sentence extract** | Tap on Articles UI → Words/Cards (ADR 0006) |
| **Lexicon** | Analyzer only (no gloss/JLPT seed) |
| **Auth** | Password + signed session before public tunnel |
| **Analyzer lib** | **Kagome** (IPA), pure-Go |
| **Password hash** | **bcrypt** |

### Unfamiliar Word (highlight) — locked

```text
phase IN (new, learning, relearning)
OR due_at <= now
OR (phase == review AND interval_days < 21)
```

### Frontend through phases

| Phases | Frontend |
|--------|----------|
| 0–4 | Static HTML/templates + **HTMX** + **Tailwind CDN** + minimal custom CSS |
| 5+ | May keep CDN or add a build step only if needed |

---

## Canonical data model

Single Learner; `users.id = 1` for v1.

### Tables

**users**

| Column | Type | Notes |
|--------|------|--------|
| id | INTEGER PK | Always `1` |
| password_hash | TEXT | bcrypt |
| created_at | TEXT | RFC3339 |

**news_sources**

| Column | Type | Notes |
|--------|------|--------|
| id | INTEGER PK | |
| name | TEXT | e.g. `nhk_main`, `yahoo_topics`, … |
| feed_url | TEXT | See seed URLs below |
| enabled | INTEGER | 1 for both in v1 |
| notes | TEXT | |

**articles**

| Column | Type | Notes |
|--------|------|--------|
| id | INTEGER PK | |
| source_id | INTEGER FK | |
| external_id | TEXT | guid or link; UNIQUE(source_id, external_id) |
| title | TEXT | |
| url | TEXT | Feed link (do not fetch HTML) |
| fetched_at | TEXT | |
| raw_text | TEXT | title + description/content from RSS only |

**sentences**

| Column | Type | Notes |
|--------|------|--------|
| id | INTEGER PK | |
| article_id | INTEGER FK | |
| text | TEXT | |
| order_index | INTEGER | |
| extracted_at | TEXT | NULL until Sentence extract; RFC3339 when opted into study (`003_sentence_extract.sql`) |

**words**

| Column | Type | Notes |
|--------|------|--------|
| id | INTEGER PK | |
| lemma | TEXT | |
| reading | TEXT | non-empty kana |
| UNIQUE(lemma, reading) | | |

**sentence_words**

| Column | Type | Notes |
|--------|------|--------|
| id | INTEGER PK | |
| sentence_id | INTEGER FK | |
| word_id | INTEGER FK | |
| surface | TEXT | |
| char_start | INTEGER | **Required** when analyzer provides byte/rune offsets; use rune offsets into sentence text |
| char_end | INTEGER | Exclusive end, runes |
| created_at | TEXT | newest occurrence = max(created_at), tie-break max(id) |

**cards**

| Column | Type | Notes |
|--------|------|--------|
| id | INTEGER PK | |
| user_id | INTEGER FK | |
| word_id | INTEGER FK | |
| phase | TEXT | `new` \| `learning` \| `review` \| `relearning` |
| learning_step | INTEGER | |
| interval_days | REAL | |
| ease | REAL | default 2.5 |
| due_at | TEXT | RFC3339 |
| reps | INTEGER | |
| lapses | INTEGER | |
| UNIQUE(user_id, word_id) | | |
| created_at | TEXT | |
| updated_at | TEXT | |

**reviews**

| Column | Type | Notes |
|--------|------|--------|
| id | INTEGER PK | |
| user_id | INTEGER FK | |
| card_id | INTEGER FK | |
| sentence_id | INTEGER FK | context shown |
| grade | TEXT | `again` \| `hard` \| `good` \| `easy` |
| reviewed_at | TEXT | |

Migrations: `migrations/001_init.sql`, applied on startup.

---

## Scheduler

**Normative:** [`docs/sm2-spec.md`](docs/sm2-spec.md). ADR: [`docs/adr/0005-pure-schedule-deep-review.md`](docs/adr/0005-pure-schedule-deep-review.md).

### `internal/schedule` (pure)

- **No I/O.** Golden tests G1–G9 from the spec.
- **`Params` / `DefaultParams()`** — all normative knobs (learning steps, ease, intervals, `NewPerDay`, `SessionLimit`, …). v1: construct review with `DefaultParams()`; env overrides (`JKRT_NEW_PER_DAY`, etc.) optional later without changing next/grade.
- **`NewCard(params, now) → state`** — seed fields for Sentence extract (`phase=new`, ease, `due_at=now`, …). **`db.ExtractSentence` / `persistCandidatesTx` must use this** (not Scrape/`StoreArticle`) — do not re-fork defaults in SQL.
- **`Apply(params, state, grade, now) → state`** — single pure transition; id-free card state (phase, learning_step, interval_days, ease, due_at, reps, lapses).
- **`IsUnfamiliar(state, now) bool`** — locked highlight predicate (spec). Lives here, not in `db`.

### `internal/review` (deep)

Small external interface (HTTP + tests cross the same seam):

| Op | Behaviour |
|----|-----------|
| **next**(learner, now) | Review queue: due first (`due_at <= now`, `phase != new`, order `due_at` ASC), then new under `SessionLimit` (grades today UTC) + `NewPerDay` (cards whose **first review row** is today UTC). Skip unpresentable Cards. Empty queue → **empty result, not an error**. |
| **grade**(card_id, sentence_id, grade, **card_updated_at**, now) | Validate grade; validate Sentence linked to Card’s Word; **optimistic lock** on `cards.updated_at` (stale → no second Apply); `schedule.Apply`; update `cards`; insert `reviews` with **that** `sentence_id`. **Does not** return the next Card — caller calls **next** again. |

- **Owns Review SQL** on concrete SQLite (`*sql.DB` / `*db.DB` handle). No `ReviewStore` interface until a second adapter exists.
- **next presentation payload** (no HTML): Sentence text; ordered spans (surface, char range, Word identity, Reading, unfamiliar?); focus Card/Word. Templates own furigana CSS/toggle.
- Queue **selection** implemented in SQL inside review; only caps/defaults come from `schedule.Params`.

### `internal/http` (shallow)

- GET/POST `/review` call review next/grade only. No queue SQL in handlers.

### `internal/db` (ingest unchanged shape)

- Keep deep **`StoreArticle`** + **`ExtractSentence`** (ADR 0006) — do not re-fork Card defaults outside `schedule.NewCard`.
- New Card rows: persist **`schedule.NewCard`** output. Remove package-local forked ease/unfamiliar once schedule exists.

---

## Morphological analysis

- Library: **Kagome v2 + IPA dictionary** (pin module version in go.mod when added).
- Tokenize Sentence → lemma, reading, surface, span.
- Keep Token iff contains ≥1 kanji **and** reading non-empty.
- Upsert `words`; insert `sentence_words` with spans; upsert `cards` for user 1 from **`schedule.NewCard`** (sm2-spec new-card row).
- Fixture Japanese strings in `testdata/analyze/` for unit tests.

Example fixture sentence (for tests):

```text
経済政策を発表した。
```

---

## RSS ingest

### Seed sources (defaults)

Hardcoded in `internal/scrape.DefaultSources` (same pattern as original NHK main). NHK main/easy URLs overridable via env.

| name | Default feed_url | Notes |
|------|------------------|--------|
| `nhk_main` | `https://news.web.nhk/n-data/conf/na/rss/cat0.xml` | Verified 2026-07-17 (XML RSS; title+description). Override: `JKRT_NHK_MAIN_RSS_URL`. |
| `nhk_easy` | *(empty until configured)* | No stable public **RSS** verified 2026-07-17. Live URL via `JKRT_NHK_EASY_RSS_URL`; soft-fail when empty. Tests use fixtures. |
| `yahoo_topics` | `https://news.yahoo.co.jp/rss/topics/top-picks.xml` | Yahoo!ニュース major topics (JA). Often title-heavy. |
| `itmedia_news` | `https://rss.itmedia.co.jp/rss/2.0/news_bursts.xml` | ITmedia NEWS latest (JA). |
| `bbc_japanese` | `https://feeds.bbci.co.uk/japanese/rss.xml` | BBC News 日本語. |

**Agent rules for feeds:**

1. Ship fixtures under `testdata/rss/` for parse tests; no live network in `go test`.  
2. Do **not** add HTML scrape or goquery to fill short RSS bodies.  
3. Do **not** use non-RSS JSON as a silent substitute without updating this plan + ADR 0003.  
4. Adding another Source: append to `DefaultSources` with a stable `name` + public RSS 2.0 URL; keep partial success.  
5. When a real Easy RSS URL is known, put it in env and note the date in Progress log.

### Behavior

- One Scrape fetches **all** configured sources sequentially (timeout per feed, e.g. 15s).  
- Parse RSS 2.0; `raw_text = title + "\n" + description` (and content:encoded if present).  
- Dedupe `(source_id, external_id)`.  
- Sentence split on `。！？` and fullwidth variants.  
- Then analyze pipeline.  
- Partial success OK if one feed fails (return 200 JSON with per-source errors — see HTTP table).

---

## Project layout (target)

```text
jkrt/
  cmd/server/main.go
  internal/
    config/
    db/
    analyze/
    scrape/
    schedule/
    review/
    auth/
    http/
  migrations/
  testdata/
    rss/
    analyze/
  web/
    static/
    templates/    # when needed
  docs/
    adr/
    sm2-spec.md
  CONTEXT.md
  DEVELOPMENT_PLAN.md
  AGENTS.md
  README.md
```

### Config (env)

| Env | Default | Notes |
|-----|---------|--------|
| `JKRT_ADDR` | `:8080` | |
| `JKRT_DB_PATH` | `./jkrt.db` | |
| `JKRT_PASSWORD` | *(required if auth on and no user yet)* | Bootstrap user 1 |
| `JKRT_AUTH` | `on` | `off` only for local dev |
| `JKRT_SESSION_SECRET` | *(required if auth on)* | ≥32 bytes random |
| `JKRT_SESSION_TTL` | `168h` | 7 days |
| `JKRT_NHK_MAIN_RSS_URL` | seed default above | override |
| `JKRT_NHK_EASY_RSS_URL` | empty | set when known |
| `JKRT_NEW_PER_DAY` | `20` | |
| `JKRT_SESSION_LIMIT` | `40` | |

### Auth (Phase 0 + Phase 5)

- Algorithm: **bcrypt** cost 10+  
- Session: **HMAC-signed cookie** (no server session table in v1)  
- Cookie name: `jkrt_session`  
- Flags: `HttpOnly`, `SameSite=Lax`; `Secure` when request is HTTPS / `X-Forwarded-Proto=https`  
- TTL: `JKRT_SESSION_TTL` (default `168h`); cookie `Expires` + `MaxAge`; server rejects expired payload  
- Bootstrap: on startup, if no user row and `JKRT_PASSWORD` set, create user 1 with hash  
- Password **rotate**: `auth.SetPassword` / `go run ./cmd/setpassword` (does not wipe Cards); optional new `JKRT_SESSION_SECRET` to invalidate sessions  
- If auth on and no password/user: **refuse to start** with clear error  
- `JKRT_AUTH=off`: skip middleware (**local only** — never with Cloudflare Tunnel)  
- Ops guide: [`docs/auth-and-tunnel.md`](docs/auth-and-tunnel.md)

### .gitignore (Phase 0 must include)

```gitignore
/bin/
/tmp/
*.db
*.db-journal
.env
.env.*
!.env.example
dist/
node_modules/
.DS_Store
coverage.out
```

---

## HTTP surface (Phase 0–6)

All times JSON errors: `{"error":"..."}` unless noted.  
When `JKRT_AUTH=on`, unauthenticated requests to protected routes → **401** (API) or **302** to `/login` (HTML).

| Method | Path | Auth | Phase | Request | Success |
|--------|------|------|-------|---------|---------|
| GET | `/health` | no | 0 | — | `200` `{"status":"ok"}` |
| GET | `/` | yes* | 0/4/6 | — | `200` HTML **dashboard** (due/new, session progress, library/phase numbers, last scrape, export links; empty library hint) |
| GET | `/login` | no | 0 | — | `200` HTML form |
| POST | `/login` | no | 0 | form `password` | `302` `/` + Set-Cookie; bad → `401` HTML/form error |
| POST | `/logout` | yes | 0 | — | `302` `/login` clear cookie |
| POST | `/api/scrape` | yes | 2/4 | empty body | `200` JSON `{ "sources": [ { "name", "ok", "items_new", "error?" } ] }`; **HTMX** → `200` HTML summary fragment |
| GET | `/api/stats` | yes | 6 | — | `200` JSON `{ "queue": review.Stats, "library": LibraryCounts, "as_of" }` |
| GET | `/api/export` | yes | 6 | `?format=json\|csv` (default json) | `200` attachment JSON snapshot or CSV of Cards; bad format → `400` |
| GET | `/review` | yes | 3 | — | `200` HTML from **next** payload (focus Word + Sentence spans) or empty state |
| POST | `/review` | yes | 3 | form `card_id`, `grade`, `sentence_id`, **`card_updated_at`** | `302` `/review` (re-**next**) or `200` HTMX **partial** (`#review-main`); stale double-submit re-nexts; bad input → 4xx |
| GET | `/articles` | yes | 4 | — | `200` HTML article list (newest first) or empty state |
| GET | `/articles/:id` | yes | 4 | path id | `200` HTML Article + Sentences (Add to review / In queue); missing → `404` HTML |
| POST | `/articles/:id/sentences/:sid/extract` | yes | ADR 0006 | empty body | Extract sentence → Words/Cards; **HTMX** → `200` sentence row; else `302` `/articles/:id`; wrong ownership/`sid` → `404` |

\*When auth off, `/` is open.

**Grade values:** `again`, `hard`, `good`, `easy` (lowercase).  
**`sentence_id`:** the Sentence shown with the Card (from **next**); stored on the `reviews` row so history matches on-screen context.

Do not invent routes beyond this table without updating this file.

---

## Phases

### Phase 0: Project setup — **done**

- [x] `go mod init github.com/rikiisworking/jkrt`
- [x] Layout: `cmd/server`, `internal/config`, `internal/auth`, `internal/http`, `web/static`
- [x] Fiber server + env config
- [x] `GET /health`
- [x] Static placeholder + Tailwind CDN + Noto Sans JP link
- [x] `.gitignore` as above; optional `.env.example`
- [x] Auth: bcrypt bootstrap, signed cookie session, login/logout, middleware
- [x] README quick start matches reality

**Tests:** health 200; auth off open; auth on 401/302 without cookie; login success sets cookie.

**Acceptance (copy-paste):**

```bash
# terminal 1
export JKRT_AUTH=off
go run ./cmd/server

# terminal 2
curl -sS http://127.0.0.1:8080/health
# expect: {"status":"ok"}

curl -sS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/
# expect: 200
```

```bash
# auth on
export JKRT_AUTH=on
export JKRT_PASSWORD='test-password-change-me'
export JKRT_SESSION_SECRET='0123456789abcdef0123456789abcdef'
go run ./cmd/server

curl -sS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/
# expect: 302 (to login)

curl -sS -c /tmp/jkrt-cj -X POST -d 'password=test-password-change-me' \
  -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/login
# expect: 302

curl -sS -b /tmp/jkrt-cj -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/
# expect: 200
```

**Deliverable:** server runs; health + login work as above.

---

### Phase 1: Data layer, analyzer, Cards — **done**

- [x] `migrations/001_init.sql` + apply on startup
- [x] Kagome wrapper → candidates + spans
- [x] Persist words, sentence_words, cards (new-card defaults from sm2-spec)
- [x] Unfamiliar helper implements locked predicate
- [x] Empty reading skipped
- [x] No live RSS (string/fixture analyze only)
- [x] Deep Article ingest interface (pre–Phase 2):
  - `db.StoreArticle` — Source + Article → Sentences only; returns `IngestCreated` \| `IngestExists` (Words/Cards via `ExtractSentence`, ADR 0006)
  - `db.IngestText` — library/manual path (unique external_id; always Created)
  - Dedupe on `(source_id, external_id)`: Exists skips re-extract (no analyze)
  - Single SQL path via shared querier helpers (no public/tx twin pairs)

**Tests:** analyze fixture sentence; UNIQUE(lemma, reading); skip empty reading; card row on extract; StoreArticle dedupe.

**Acceptance:**

```bash
go test ./internal/analyze/... ./internal/db/... -count=1
# all pass; includes Japanese fixture 経済政策を発表した。
```

**Deliverable (historical Phase 1):** library ingest + extract path; today: `StoreArticle` + `ExtractSentence` / test helper `IngestText`.

---

### Phase 2: RSS Scrape (both sources) — **done**

- [x] Seed `news_sources` rows (or `EnsureSource` at scrape time)
- [x] `POST /api/scrape` all feeds; parse; call **`StoreArticle`** per item (not ad-hoc SQL; no Cards — ADR 0006)
- [x] Fixtures `testdata/rss/nhk_main_sample.xml`, `nhk_easy_sample.xml`
- [x] Mockable HTTP client; timeouts; partial success JSON
- [x] Dedupe stable via `IngestExists` (count `items_new` from Created only)

**Tests:** offline ingest from fixtures; **zero** network dials in tests.

**Acceptance:**

```bash
go test ./internal/scrape/... ./... -count=1
# with auth + real network (manual only):
# curl -b cookie -X POST http://127.0.0.1:8080/api/scrape
```

**Deliverable:** fixture tests green; manual live scrape works for main at minimum.

---

### Phase 3: Review + SM-2 — **done**

**Architecture locked (2026-07-17):** ADR 0005 + Scheduler/Review sections above. Implement in this order.

- [x] Lock pure `schedule` + deep `review` (next/grade) seams; doc + ADR
- [x] `internal/schedule`: `Params`/`DefaultParams`, `NewCard`, `Apply`, `IsUnfamiliar`; G1–G9
- [x] `db` extract: new Cards from `schedule.NewCard`; drop forked `StartingEase` / `db.IsUnfamiliar`
- [x] `internal/review`: next (queue SQL + newest Sentence + presentation payload) + grade (TX + `reviews` row)
- [x] Wire `review` into `http` / `main` with `schedule.DefaultParams()`
- [x] `GET/POST /review` HTML; focus one Word; four grade buttons; post `card_id` + `grade` + `sentence_id` + `card_updated_at` (optimistic lock)
- [x] Furigana toggle (default off; sessionStorage); Unfamiliar spans in Sentence; HTMX `#review-main` partial
- [x] Tailwind CDN styling good enough for phone
- [x] Phase 3 hardening: skip unpresentable Cards; shared `schedule.Params` on DB extract; UTC-day caps documented

**Tests:** schedule pure G1–G9; review integration (temp DB): next order/caps, grade updates `due_at`, empty queue, stale double-submit; HTTP smoke for `/review`.

**Acceptance (verified 2026-07-17):**

```bash
go test ./internal/schedule/... ./internal/review/... -count=1
go test ./... -count=1
# manual: login → scrape → Articles → Add to review (extract) → /review → grade again/hard/good/easy → next card
```

**Deliverable:** daily Review loop usable on phone-width browser.

---

### Phase 4: Frontend polish — **done**

- [x] Dashboard: due count, last scrape, links
- [x] Browse articles/sentences
- [x] Theme polish `#3B82F6`, empty states, session progress
- [x] Keep CDN unless build is clearly needed

**Acceptance:** mobile browser checklist (manual) + handler smoke tests — `go test ./...` green (dashboard, articles list/detail, scrape HTMX HTML, auth gates).

---

### Phase 5: Auth harden + tunnel docs — **done**

- [x] Secure cookie / TTL documented (`docs/auth-and-tunnel.md`, README, `.env.example`; login sets `MaxAge` + `Expires` from `JKRT_SESSION_TTL`)
- [x] Password rotate procedure (`auth.SetPassword` / `Store.UpdatePasswordHash`, `go run ./cmd/setpassword`)
- [x] Tunnel README section: never auth off (`README.md` + `docs/auth-and-tunnel.md`)

**Acceptance:** auth tests for expiry/unauthorized; docs present.

```bash
go test ./internal/auth/... ./internal/http/... -count=1
go test ./... -count=1
# docs: docs/auth-and-tunnel.md + README tunnel section
```

**Deliverable:** safe phone access via tunnel only with auth on; password rotate without wiping Cards.

---

### Phase 6: Stats, export, performance — **done**

- [x] Stats endpoints or dashboard numbers (`GET /api/stats`; dashboard library/phase + mature counts)
- [x] Export JSON/CSV (`GET /api/export?format=json|csv` via `internal/export`)
- [x] Indexes, size limits (`migrations/002_perf.sql`; `MaxRawTextRunes` truncate on ingest; export row caps)

**Acceptance:** export fixture test; `go test ./...` green.

```bash
go test ./internal/export/... ./internal/db/... ./internal/http/... -count=1
go test ./... -count=1
```

**Deliverable:** backup export + library stats API; safer indexes and body caps.

---

## Milestone criteria (every phase)

1. Checklist done  
2. `go test ./...` passes  
3. Phase **Acceptance** commands succeed  
4. Commit with clear message  
5. Status updated here + `AGENTS.md`  

---

## Risks & mitigations

| Risk | Mitigation |
|------|------------|
| Thin RSS bodies | Accept; measure; HTML needs new ADR |
| Easy feed URL missing | Fixtures + soft-fail; env when found |
| Analyzer identity drift | Pin Kagome/dict version |
| SM-2 ambiguity | Only `docs/sm2-spec.md` |
| Queue flood | Caps 20/40 |
| Tunnel without auth | Phase 0 auth; Phase 5 docs |

---

## Explicit non-goals (v1)

- Multi-user product  
- HTML/goquery article scrape  
- Other publishers  
- JLPT/gloss seed  
- Anki sync / FSRS  
- Character-level progress  
- Heavy SPA / early npm Tailwind pipeline  
- Background auto-scrape  

---

## Progress log

| Date | Note |
|------|------|
| 2026-07-17 | **Extract-on-tap (ADR 0006):** Scrape = Articles/Sentences only; `ExtractSentence` + `POST /articles/:id/sentences/:sid/extract`; `sentences.extracted_at`; IngestText still store+extract-all for tests. |
| 2026-07-17 | Multi-publisher RSS: `DefaultSources` adds `yahoo_topics`, `itmedia_news`, `bbc_japanese` (hardcoded public RSS 2.0 URLs); scrape still sequential + partial success; docs/ADR 0003/CONTEXT updated. |
| 2026-07-17 | Architecture deepen: `snapshot.Load` composes queue+library for dashboard/stats/export; mature counts use `schedule.Params` (no forked 21); `http.New` syncs Review.Params onto DB (single boot path). |
| 2026-07-17 | Phase 6 **complete**: `/api/stats`, `/api/export` JSON/CSV, dashboard library/export links, `002_perf.sql` indexes, raw_text truncate + export caps; export fixture tests. v1 phase plan complete. |
| 2026-07-17 | Phase 5 **complete**: cookie/TTL docs, password rotate (`cmd/setpassword`), tunnel guide (never auth off), expired-session HTTP 302/401 tests, login `MaxAge`. Next: Phase 6 only. |
| 2026-07-17 | Phase 4 **complete**: live dashboard (`GET /`) with due/new, UTC session progress bars, last scrape, HTMX Scrape button; `GET /articles` + `GET /articles/:id` browse; shared nav/theme; empty states; `review.Stats` + `db` browse reads; handler smoke tests. Next: Phase 5 only. |
| 2026-07-17 | Phase 3 **closed**: checklist + acceptance re-verified (`go test ./...`); README/AGENTS/plan status aligned; next work is Phase 4 only. |
| 2026-07-17 | Phase 3 hardening: optimistic grade lock (`card_updated_at`), skip unpresentable Cards, HTMX `#review-main` partial + furigana sessionStorage, shared `schedule.Params` on DB extract, docs clarify UTC-day SessionLimit + NewPerDay on first grade. |
| 2026-07-17 | Phase 3 complete: pure `internal/schedule` (NewCard/Apply/IsUnfamiliar, G1–G9), deep `internal/review` (next/grade, queue caps, newest Sentence spans), extract via `schedule.NewCard`, `GET/POST /review` HTML (4 grades, furigana toggle off by default, unfamiliar highlight). |
| 2026-07-17 | Pre–Phase 3 architecture: pure `internal/schedule` + deep `internal/review` (next/grade); extract uses `schedule.NewCard`; ADR 0005; plan/HTTP/`sentence_id` locked. Implementation still Phase 3 checklist. |
| 2026-07-17 | Phase 2 complete: `internal/scrape` RSS 2.0 parse + dual NHK fetch, `POST /api/scrape` (partial success JSON), fixtures under `testdata/rss/`, mock HTTP client (no network in tests), store/ingest per item with `items_new` dedupe (today: `StoreArticle`; historical name was `IngestArticle`). Easy URL optional (`JKRT_NHK_EASY_RSS_URL`); soft-fail when empty. |
| 2026-07-17 | Phase 1 complete: `migrations/001_init.sql`, `internal/db` (migrate + extract + unfamiliar), Kagome IPA analyze, words/sentence_words/cards on extract, fixture tests green. |
| 2026-07-17 | Phase 0 complete: go module, Fiber, `/health`, static placeholder (Tailwind CDN + Noto Sans JP), bcrypt + HMAC session auth, acceptance curls green. |
| 2026-07-17 | Agent-hardening: sm2-spec, HTTP table, locked pick-ones, acceptance curls, CDN rule, feed URL notes. |
| 2026-07-17 | Grill: Word unit, Kagome path, dual NHK RSS-only, SM-2 4-button. |
| 2026-07-17 | Character-based plan superseded. |
