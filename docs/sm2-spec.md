# SM-2 scheduler specification (normative)

**This file is the source of truth for Card scheduling.**  
Implement as pure functions in `internal/schedule`. UI and DB only persist results.  
ADR: [`0004-sm2-anki-like-scheduler.md`](adr/0004-sm2-anki-like-scheduler.md).  
Package seams: [`0005-pure-schedule-deep-review.md`](adr/0005-pure-schedule-deep-review.md) — schedule pure (`NewCard`, `Apply`, `IsUnfamiliar`, `Params`); Review queue/persist in `internal/review` (next/grade).

## Grades

| Grade | Wire value | Meaning |
|-------|------------|---------|
| Again | `again` | Failed; reset toward learning |
| Hard | `hard` | Passed with difficulty |
| Good | `good` | Normal pass |
| Easy | `easy` | Trivial; longer interval |

## Defaults (config-overridable later)

| Name | Value |
|------|--------|
| `LearningSteps` | `[1m, 10m]` (duration) |
| `GraduatingInterval` | `1` day (Good from learning) |
| `EasyInterval` | `4` days (Easy from learning/new) |
| `StartingEase` | `2.5` |
| `MinEase` | `1.3` |
| `EasyBonus` | `1.3` (multiplier on Easy in review) |
| `IntervalModifier` | `1.0` |
| `HardIntervalFactor` | `1.2` (review Hard: `interval * 1.2`) |
| `LapseNewIntervalFactor` | `0.0` (after lapse graduate, interval starts over from graduating path; see lapse) |
| `NewPerDay` | `20` |
| `SessionLimit` | `40` |

Time: use a single `now time.Time` parameter in pure functions (inject clock in tests).

## Card fields used by scheduler

| Field | Role |
|-------|------|
| `phase` | `new` \| `learning` \| `review` \| `relearning` |
| `learning_step` | Index into `LearningSteps` (0-based); meaningful in learning/relearning |
| `interval_days` | Last successful review interval in **days** (float OK) |
| `ease` | Ease factor |
| `due_at` | Next show time |
| `reps` | Successful review count (increment on Hard/Good/Easy that leave a stable step) |
| `lapses` | Count of Again while in `review` |

### New card creation (on extract)

When a Word is first extracted for the Learner, **`schedule.NewCard`** produces this state; **`db` only persists** it (no parallel ease/phase constants in extract):

| Field | Value |
|-------|--------|
| `phase` | `new` |
| `learning_step` | `0` |
| `interval_days` | `0` |
| `ease` | `StartingEase` (2.5) |
| `due_at` | `now` (eligible immediately; **queue caps** decide if shown) |
| `reps` | `0` |
| `lapses` | `0` |

**New for daily cap:** `phase == new` OR (`reps == 0` and never left `new` via a grade). Simpler rule: **`phase == new`**. After first non-Again graduation into learning/review, not new.

## Queue selection (not SM-2 math, but normative)

Implemented by **`internal/review` next** (SQL), using **`NewPerDay` / `SessionLimit` from `schedule.Params`**. Not pure schedule transitions.

1. Load due cards: `due_at <= now` and `phase != new`, order by `due_at` ASC.
2. If under `SessionLimit`, fill with **new** cards (`phase == new`) up to remaining session slots and not exceeding **new introduced today** vs `NewPerDay`.
3. “Introduced today” = count of cards that left `phase=new` today (first grade applied), or count of new cards shown today — implement **count of cards whose first review row is today**; until first review, showing a new card counts toward both session and new-per-day when **presented**.

**Presentation rule:** when a `new` card is **shown** in a session, it counts toward `NewPerDay` for that calendar day (app local timezone or UTC — **use UTC** for v1, document in config).

## Transitions

### From `new`

| Grade | Next phase | learning_step | interval_days | ease | due_at |
|-------|------------|---------------|---------------|------|--------|
| Again | `learning` | `0` | `0` | unchanged | `now + LearningSteps[0]` |
| Hard | `learning` | `0` | `0` | unchanged | `now + LearningSteps[0]` |
| Good | `learning` | `0` | `0` | unchanged | `now + LearningSteps[0]` |
| Easy | `review` | `0` | `EasyInterval` | unchanged | `now + EasyInterval` |

Notes: First non-Easy grades enter learning at step 0. Easy graduates immediately to review with 4-day interval (classic easy-over-new behavior).

### From `learning` or `relearning`

Let `steps = LearningSteps`, `i = learning_step`.

| Grade | Behavior |
|-------|----------|
| **Again** | `learning_step = 0`; `due_at = now + steps[0]`; stay in current learning/relearning phase; ease unchanged |
| **Hard** | Stay at same step; `due_at = now + steps[i]` (repeat step); ease unchanged |
| **Good** | If `i+1 < len(steps)`: `learning_step = i+1`; `due_at = now + steps[i+1]`. Else **graduate**: `phase=review`; `interval_days = GraduatingInterval` (1d); `due_at = now + 1d`; `reps += 1` |
| **Easy** | **Graduate** immediately: `phase=review`; `interval_days = EasyInterval` (4d); `due_at = now + 4d`; `reps += 1`; ease unchanged on graduate from learning |

### From `review`

Let `I = interval_days`, `E = ease`.

| Grade | ease | interval_days | due_at | phase / other |
|-------|------|---------------|--------|----------------|
| **Again** | `max(MinEase, E - 0.2)` | keep old `I` stored for reference but scheduling uses relearn | `now + LearningSteps[0]` | `phase=relearning`; `learning_step=0`; `lapses += 1` |
| **Hard** | `max(MinEase, E - 0.15)` | `max(1, I * HardIntervalFactor)` → then `* IntervalModifier` | `now + newInterval` | stay `review`; `reps += 1` |
| **Good** | unchanged | `max(1, I * E * IntervalModifier)` ; if first review interval was graduating, next uses ease as normal | `now + newInterval` | stay `review`; `reps += 1` |
| **Easy** | `E + 0.15` | `max(1, I * E * EasyBonus * IntervalModifier)` | `now + newInterval` | stay `review`; `reps += 1` |

**First Good in review after graduate:** previous interval is already `GraduatingInterval` (1); next Good → `1 * 2.5 * 1.0 = 2.5` days (round display OK; store float; due_at uses duration from float days).

**Rounding:** store `interval_days` with at least 2 decimal places; `due_at = now + interval_days * 24h` (use `time.Duration` from float hours = `interval_days * 24`).

### From `relearning` after lapse

Same step table as learning. On **Good** graduate from last step (or Easy):

- `phase = review`
- `interval_days = max(1, GraduatingInterval)` when `LapseNewIntervalFactor == 0` → use **1 day** (full reset of interval growth)
- `due_at = now + interval_days`
- ease already reduced on the Again that caused lapse

## Unfamiliar Word (display highlight)

A Word is **Unfamiliar** for highlighting iff its Card satisfies (`schedule.IsUnfamiliar`):

```text
phase IN (new, learning, relearning)
OR due_at <= now
OR (phase == review AND interval_days < 21)
```

`21` days = “comfortable” threshold for bolding in article/sentence browse. Review queue still uses due/new rules above, not this predicate alone. **next** applies this when building span flags for the Sentence payload.

## Empty reading

If analyzer yields no reading (empty/whitespace): **do not create** a Word or Card. Skip that Token.

## Golden examples (unit tests must pass)

Clock starts at `T0`. Steps = 1m, 10m. Ease start 2.5.

### G1 — New + Good enters learning

- Card: new @ T0  
- Grade Good @ T0  
- Expect: phase=learning, step=0, due=T0+1m  

### G2 — Learning Good advances step

- learning step=0 @ T0  
- Good @ T0  
- Expect: step=1, due=T0+10m  

### G3 — Learning Good graduates

- learning step=1 @ T0  
- Good @ T0  
- Expect: phase=review, interval_days=1, due=T0+1d, reps=1  

### G4 — Learning Easy graduates to 4d

- learning step=0 @ T0  
- Easy @ T0  
- Expect: phase=review, interval_days=4, due=T0+4d  

### G5 — Review Good multiplies ease

- review, interval=1, ease=2.5 @ T0  
- Good @ T0  
- Expect: interval_days=2.5, due=T0+2.5d, ease=2.5  

### G6 — Review Again lapses

- review, interval=10, ease=2.5 @ T0  
- Again @ T0  
- Expect: phase=relearning, step=0, lapses=1, ease=2.3, due=T0+1m  

### G7 — Review Hard

- review, interval=10, ease=2.5 @ T0  
- Hard @ T0  
- Expect: phase=review, interval_days=12, ease=2.35, due=T0+12d  

### G8 — Review Easy

- review, interval=10, ease=2.5 @ T0  
- Easy @ T0  
- Expect: ease=2.65, interval_days=10*2.5*1.3=32.5, due=T0+32.5d  

### G9 — Min ease floor

- review, ease=1.35 @ T0  
- Again @ T0  
- Expect: ease=1.3 (not below MinEase)  

## Non-goals

- FSRS, Anki collection sync, filtered decks, leech thresholds, fuzz, timezone-aware “day” beyond UTC v1.
