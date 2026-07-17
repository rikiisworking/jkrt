# Architecture improvement plan — JKRT

**Date:** 2026-07-17  
**Scope:** Overall structure after extract-on-tap (ADR 0006) landed on `main`  
**Vocabulary:** *module*, *interface*, *depth*, *seam*, *adapter*, *leverage*, *locality* (codebase-design)  
**Domain language:** CONTEXT.md (Scrape, Sentence extract, Word, Card, Review, …)  
**Do not re-litigate:** ADR 0005 (pure `schedule` + deep `review`), ADR 0006 (extract-on-tap)

**C1 + C2:** decisions locked in [§4](#4-decided-plan-c1--c2-grilled-2026-07-17); **implement later**.

Visual HTML report (same candidates):  
`/tmp/architecture-review-20260717170837.html`

---

## 1. Current structure (as-is)

### Package graph (production)

```
cmd/server
  → config, analyze, auth, db, http, review, schedule

http (composition root)
  → auth, analyze, config, db, export, review, schedule, scrape, snapshot

scrape → db
review → db, schedule
db → analyze, schedule
export → db, review, snapshot
snapshot → db, review

schedule  (stdlib only)     ← deep pure core
analyze   (Kagome)          ← deep pure core
```

Acyclic. Leaves are strong.

### Learning pipeline (domain)

| Learner action | Module path |
|----------------|-------------|
| **Scrape** | `http` → `scrape.Scraper` → `db.StoreArticle` (library only) |
| **Sentence extract** | `http` → `db.ExtractSentence*` + `analyze` → Words/Cards |
| **Review** next/grade | `http` → `review.Service` → `schedule` + SQL |
| Dashboard / stats / export header | `http` → `snapshot.Load` → `review.Stats` + `db.LibraryCounts` |
| Export body | `http` → `export` |

### Depth snapshot

| Module | Assessment | Deletion test |
|--------|------------|---------------|
| `schedule` | **Deep** | Math would reappear in SQL/handlers |
| `analyze` | **Deep** | Candidate rule + Kagome would scatter |
| `review` | **Deep** (ADR 0005) | next/grade hide queue + grade + spans |
| `scrape` | Mixed — parse deep; Run is glue | Parse earns keep; Run is thin |
| `db` | **Wide multi-concern** | Package is essential; *files* fail deletion independently |
| `snapshot` | Shallow composition seam | Delete → three handlers re-glue Stats+Library (intentional) |
| `http` + `html.go` | Thin control flow + **wide presentation** | Templates have no domain leverage; high locality tax |
| `export` | Medium | Codecs real; composes snapshot for headers |

### What is already healthy

- Pure schedule + deep Review (ADR 0005) — do not invert.
- Extract-on-tap (ADR 0006) — Scrape ≠ Cards; opt-in Sentence extract.
- `snapshot.Load` single composition for dashboard/stats/export headers.
- `scrape.HTTPDoer` real seam (fixture adapter in tests).
- Strong pure tests for schedule/analyze/RSS parse; no network in `go test`.

### Friction hotspots (recent change weight)

1. **`internal/db/extract.go` (~530 LOC)** — library store + Sentence extract + Word/Card SQL + `IngestText` test helper on one type.
2. **`internal/http/html.go` (~657 LOC)** — all Learner UI strings; imports concrete `db` / `review` / `scrape` DTOs.
3. **`internal/http/app_test.go` (~2.3k LOC)** — Fiber integration only path for UI behaviour.
4. **`SetScheduleParams` side channel** on `db` so extract + mature counts share knobs with Review.
5. **Split SQL ownership** — Card *create* in `db` extract; Card *grade* SQL in `review` (correct per ADR 0005, but AI-navigability cost).
6. **Naming:** `export.Snapshot` vs package `snapshot.View`.

---

## 2. Improvement candidates

### C1 — Deepen Sentence extract as its own module  
**Strength: Strong** · dependency category: in-process

**Files:** `internal/db/extract.go`, `internal/db/db.go`, `internal/http/handlers.go`, tests under `internal/db/extract_test.go`, ADR 0006

**Problem:** `db.DB` is a wide interface (open/migrate/store/extract/browse/counts/SQL/params). Sentence extract (study opt-in) is domain orchestration, not “just persistence,” yet hangs Kagome off `*DB`.

**Solution:** Introduce a dedicated **Sentence extract module** (e.g. `internal/extract` service, or at minimum extract-only files with a tiny constructor) whose external interface is essentially:

- extract one Sentence (with article ownership check) → `ExtractResult`
- optionally: bulk extract for test helper only

Implementation keeps: Kagome candidates, `sentence_words`, `schedule.NewCard`, idempotent re-tap, `extracted_at`.  
Library **Scrape** path remains `StoreArticle` only (ADR 0006).

**Benefits:**

- **Locality** — all extract-on-tap rules in one place.
- **Leverage** — HTTP + tests call one deep seam.
- **AI-navigability** — “where do Cards get created?” → one module.
- Deletion test: removing the extract module would force handlers to re-implement study opt-in.

**Out of scope:** ReviewStore interface (ADR 0005 deferred until second storage adapter).

---

### C2 — Split presentation locality (`html.go`)  
**Strength: Strong** · in-process

**Files:** `internal/http/html.go`, handlers, possibly `internal/http/ui_*.go` or package-local files

**Problem:** One string monolith owns shell, dashboard, Review, Articles/extract row, login, scrape HTMX fragment. `remainingNew` re-implements queue presentation math outside Review. UI changes require scanning ~650 lines; no unit tests for pure render helpers.

**Solution:** Split by Learner surface without npm/Tailwind build (AGENTS.md):

| File (example) | Owns |
|----------------|------|
| `shell.go` | doc head, nav, pageShell |
| `dashboard.go` | home stats / scrape CTA |
| `review_ui.go` | next Card HTML + grade form + spans |
| `articles_ui.go` | list, detail, sentence row / extract feedback |
| `login.go` | auth forms |

Extract pure helpers (e.g. span → HTML, remaining-new from `review.Stats`) with table tests.

**Benefits:** locality of UI edits · fragment tests without full Fiber · handlers stay thin adapters · no ADR conflict.

---

### C3 — Single ownership for `schedule.Params`  
**Strength: Worth exploring** · in-process

**Files:** `cmd/server`, `http.New`, `db.SetScheduleParams`, extract NewCard path, `LibraryCounts` mature threshold

**Problem:** Params dual path — `review.Service` owns knobs for grade; `DB.SetScheduleParams` side-channels the same knobs for extract + mature counts. Composition root usually syncs (`http.New`); forget once → silent drift.

**Solution options (pick one later):**

1. Pass `schedule.Params` into extract module constructor (preferred with C1).  
2. Keep side channel but document “only composition root mutates” + one test that extract ease matches Review params.

**Benefits:** linear mental model · fewer hidden invariants · safer tests.

---

### C4 — Promote pure Review presentation helpers  
**Strength: Worth exploring** · in-process  
**Aligned with ADR 0005** (“pure queue helpers can be promoted later”)

**Files:** `internal/review/review.go` (`buildSpans` / loadSpans), `html.go` remaining-new / ruby rendering

**Problem:** Pure logic only covered via SQLite + Kagome + Fiber integration.

**Solution:** Export or package-test pure span assembly and remaining-new (or move remaining-new beside `Stats`). Table tests, no I/O.

**Benefits:** locality of presentation bugs · cheap regression nets.

---

### C5 — `ArticleStore` seam for Scrape  
**Strength: Speculative** · ports & adapters  

**Files:** `internal/scrape/scrape.go`, `db.StoreArticle`

**Problem:** Scraper takes concrete `*db.DB`. Multi-source partial-success tests pay full migrate cost.

**Solution:** One-method store interface only if a second adapter (fake in-memory store) is written and used.  
**Principle:** one adapter = hypothetical seam; two = real. Do not introduce interface “for purity.”

---

### C6 — Naming and micro-locality  
**Strength: Speculative** (cheap polish)

| Item | Action |
|------|--------|
| `export.Snapshot` vs `snapshot.View` | Rename export type to `Document` / `Bundle` (keep JSON fields) |
| Export row caps in `db.limits` | Move next to `export` |
| `db.LearnerUserID` vs `auth.UserID` | Single constant, re-export |
| UI “Scrape NHK” | Multi-publisher label |

---

## 3. What not to do

| Temptation | Why not |
|------------|---------|
| Invert ADR 0005 (put Review SQL back in `db` only) | Loses deep Review interface |
| Auto-Cards on Scrape | Contradicts ADR 0006 |
| Repository interface for Review “for testability” | No second adapter; raw SQL inside deep review is the I/O adapter |
| Deepen `snapshot.Load` with export card dumps | Mixes dashboard composition with backup |
| npm/Tailwind build for v1 | AGENTS.md / plan: CDN through phases |
| FSRS / multi-user / goquery HTML scrape | Explicit non-goals |

---

## 4. Decided plan: C1 + C2 (grilled 2026-07-17)

Status: **decisions locked; implement later** (markdown only — no code in this step).

### 4.1 Locked decisions

| # | Topic | Decision |
|---|--------|----------|
| 1 | Order | **C1 first**, then **C2** |
| 2 | C1 shape | **New package** `internal/extract` (not hybrid, not file-only) |
| 3 | Product interface | **`Extract(...)`** (article ownership + study opt-in) |
| 4 | Test/manual helper | **`IngestText(...)`** on same service, clearly non-product (store via `db.StoreArticle` + extract-all) |
| 5 | Wide API | Do **not** mirror full `PersistCandidates` as public product surface (unexport or drop if unused after move) |
| 6 | Params | **Constructor only:** `extract.New(db, analyzer, params)` — freeze `schedule.Params`; no extract path via `db.SetScheduleParams` |
| 7 | Types / errors | **extract owns** `ExtractResult`, `ErrSentenceNotFound`, `ErrArticleMismatch` (and related); **db = library only** |
| 8 | SQLite I/O | extract holds `*db.DB`, uses `SQL()` + **private SQL in extract** for Words/Cards/`sentence_words` (like review owns grade SQL) |
| 9 | Library path | **`db.StoreArticle`** stays on `db`; Scrape unchanged (ADR 0006) |
| 10 | C2 shape | **Same package `http`**, multiple files (not `internal/ui`) |
| 11 | Shipping | **Two PRs:** PR1 = C1, PR2 = C2 |
| 12 | Pure UI tests | **Third PR later** (C4) — not in C2 |

### 4.2 Target package layout (after C1 + C2)

```text
internal/
  extract/           # NEW — Sentence extract (study opt-in)
    extract.go       # Service, New, Extract, IngestText
    persist.go       # private: candidates → words / sentence_words / cards
    extract_test.go  # moved/adapted from db/extract_test.go
  db/                # library + infrastructure
    db.go            # open, migrate, SQL(), SetScheduleParams (mature counts only if still needed)
    store.go         # StoreArticle, EnsureSource, Source/Article SQL  (from extract.go leftovers)
    browse.go
    counts.go
    limits.go
  http/
    handlers.go      # thin: calls extract.Service
    shell.go         # from html.go
    dashboard.go
    review_ui.go
    articles_ui.go   # list, detail, sentenceRowHTML
    login.go
    app.go
    app_test.go
  review/            # unchanged deep next/grade (ADR 0005)
  scrape/            # still → db.StoreArticle
  schedule/          # pure (ADR 0005)
  analyze/           # pure-ish Kagome
```

### 4.3 Suggested public interface (C1) — sketch only

Not implemented yet; for implementers:

```go
// Package extract: Sentence extract (ADR 0006) — Words/Cards only after learner opt-in.
package extract

type Service struct { /* db *db.DB; ana *analyze.Analyzer; params schedule.Params */ }

func New(database *db.DB, ana *analyze.Analyzer, params schedule.Params) *Service

// Extract runs Kagome on one Sentence owned by articleID; creates Words/Cards; sets extracted_at.
// Idempotent re-tap does not reset Card SM-2 state.
func (s *Service) Extract(userID, articleID, sentenceID int64, now time.Time) (Result, error)

// IngestText stores text under Source "manual" then extracts every Sentence.
// Test/manual convenience only — not the product Scrape path.
func (s *Service) IngestText(userID int64, text string, now time.Time) (db.IngestResult, error)
```

HTTP today:

```text
a.DB.ExtractSentenceForArticle(...)  →  a.Extract.Extract(...)
a.Analyzer wired into extract.New at composition root (not per-call)
```

Composition root (`http.New` / `cmd/server`):

```text
params := schedule.DefaultParams() // or from review
ext := extract.New(database, ana, params)
// Review still review.New(database, params)
// LibraryCounts mature: either keep db.SetScheduleParams(params) for counts only,
// or later pass params into counts — out of C1 minimum if mature threshold still uses DB knobs
```

### 4.4 What moves vs stays

| Symbol / concern | After C1 |
|------------------|----------|
| `StoreArticle`, dedupe, split Sentences | `db` |
| `ExtractSentence*` orchestration | `extract` |
| `persistCandidatesTx`, `upsertWord`, `upsertNewCard` | `extract` (private) |
| `ExtractResult`, extract sentinel errors | `extract` |
| `IngestText` | `extract` |
| `EnsureSource` | `db` (library) |
| Browse / `GetSentence` | `db` (HTTP still needs GetSentence after extract for row render) |
| `LibraryCounts`, migrate | `db` |
| Grade / queue SQL | `review` (unchanged) |

### 4.5 PR1 — C1 `internal/extract` (implement later)

**Goal:** behaviour-identical ADR 0006; Cards only created via extract package.

| Step | Work |
|------|------|
| 1 | Create `internal/extract` with `Service`, `New(db, analyzer, params)`, `Extract`, `IngestText` |
| 2 | Move study-write SQL + types from `internal/db/extract.go` |
| 3 | Leave `StoreArticle` (+ helpers it needs) on `db`; rename file to `store.go` if helpful |
| 4 | Wire `http.App`: hold `*extract.Service`; `handleSentenceExtract` calls `Extract` |
| 5 | `http.New` / tests: construct extract with same `schedule.Params` as Review |
| 6 | Migrate tests: `db/extract_test.go` → `extract/`; replace `d.ExtractSentence` / `IngestText` call sites across packages |
| 7 | Remove extract methods from `db` public API |
| 8 | Acceptance: `go test ./... -count=1`; no network; ADR 0006 cases still green |

**Explicit non-goals for PR1:** UI file split, pure span tests, `ArticleStore` interface, FSRS, rename export.Snapshot.

### 4.6 PR2 — C2 `html.go` split (after PR1)

**Goal:** locality only; zero behaviour change.

| File | Contents (from current `html.go`) |
|------|-----------------------------------|
| `shell.go` | `docHead`, `navHTML`, `pageShell`, shared CSS/const |
| `dashboard.go` | `DashboardData`, `dashboardHTML`, `remainingNew`, `progressBar`, `dashboardEmptyHint`, `scrapeResultHTML` |
| `articles_ui.go` | `articlesListHTML`, `articleDetailHTML`, `sentenceRowHTML`, `articleNotFoundHTML`, `formatFetchedDisplay` (if only used here, or keep in shell) |
| `review_ui.go` | `reviewPageShell`, empty/full/partial, `renderSentenceSpans` |
| `login.go` | `loginHTML` |

| Step | Work |
|------|------|
| 1 | Mechanical move; package stays `http` |
| 2 | Update imports only if needed (`extract.Result` for sentence row — after PR1) |
| 3 | No new pure tests in this PR |
| 4 | Acceptance: `go test ./internal/http/ ./...`; HTML strings unchanged |

### 4.7 PR3 — optional (later)

- Table tests for `remainingNew` / `renderSentenceSpans` (plan C4).
- Narrow or remove `db.SetScheduleParams` if mature counts take params another way.
- C5/C6 polish as needed.

### 4.8 Execution order (locked)

```text
PR1  C1 internal/extract   →   PR2  C2 html file split   →   PR3  pure UI helpers (optional)
```

### 4.9 Risks & mitigations

| Risk | Mitigation |
|------|------------|
| Large test renames (`IngestText` everywhere) | Keep method name on `extract.Service`; global replace call sites |
| Mature counts still need Params on `db` | PR1 may keep `SetScheduleParams` **only** for `LibraryCounts`; extract must not read it |
| Circular imports | `extract` → `db`; `http` → `extract`+`db`; `db` must not import `extract` |
| Behaviour drift | No product logic changes; full suite is gate |

### 4.9bis When implementing

1. Re-read this section + ADR 0006.  
2. Implement PR1 only; merge.  
3. Implement PR2 only; merge.  
4. Do not combine C1+C2 in one PR unless explicitly reopened.

---

## 5. Broader phased backlog (other candidates)

Still valid but **not** in the locked C1+C2 track:

| ID | Item | When |
|----|------|------|
| C3 remainder | Mature counts without `SetScheduleParams` | After PR1 if still awkward |
| C4 | Pure span / remaining-new tests | PR3 |
| C5 | Scrape `ArticleStore` | Only if scrape tests hurt |
| C6 | export rename, UI “Scrape NHK” label | Anytime small PR |

---

## 6. What not to do

| Temptation | Why not |
|------------|---------|
| Invert ADR 0005 (put Review SQL back in `db` only) | Loses deep Review interface |
| Auto-Cards on Scrape | Contradicts ADR 0006 |
| Repository interface for Review “for testability” | No second adapter; raw SQL inside deep review is the I/O adapter |
| Deepen `snapshot.Load` with export card dumps | Mixes dashboard composition with backup |
| npm/Tailwind build for v1 | AGENTS.md / plan: CDN through phases |
| FSRS / multi-user / goquery HTML scrape | Explicit non-goals |
| Implement C1+C2 without re-reading §4 | Decisions above are the contract |

---

## 7. Success metrics (C1 + C2)

- Answer in &lt;2 minutes: *Where are Cards created?* → **`internal/extract` only**.  
- *Where is “Add to review” row HTML?* → **`articles_ui.go`**.  
- Extract ease matches Review params via **constructor**, not forgotten DB sync for extract.  
- `go test ./...` network-free and green; product behaviour unchanged.

---

## 8. Status

| Item | State |
|------|--------|
| Architecture candidates | Documented (§2) |
| C1 + C2 decisions | **Locked** (§4) |
| Implementation | **Deferred** — markdown plan only |
| Next action when ready | Implement **PR1** per §4.5 |

---

## 5. Success metrics

- New agent/human can answer in &lt;2 minutes: *Where are Cards created?* → extract module only.  
- UI change to “Add to review” row does not require opening dashboard/login strings.  
- Extract ease / mature threshold cannot diverge from Review params without a compile-time or single constructor change.  
- `go test ./...` stays network-free and green; no new product behaviour.

---

## 6. Next step (grilling)

This plan lists candidates only — **no new public interfaces designed yet**.

Which candidate do you want to explore next?

1. **C1** Sentence extract module  
2. **C2** UI file split  
3. **C3** Params ownership  
4. **C4** Pure Review helpers  
5. **C5 / C6** optional seams and renames  

Reply with a number (or combination); then we walk constraints, seams, and surviving tests before coding.
