# Anki-like SM-2 scheduler for Cards

Each Word has a **Card** scheduled with **Again / Hard / Good / Easy** and SM-2-ish state (learning steps, ease, interval, due time). Queue caps (20 new/day, 40/session) prevent scrape floods.

**Normative algorithm:** [`../sm2-spec.md`](../sm2-spec.md) — implement exactly; do not improvise formulas.

**Considered:** three-button fixed ladder (too weak); full FSRS (heavy before data); lightweight interval ladder; SM-2-ish four-button (chosen). Not building Anki sync, decks, or note types.

**Consequences:** Schema holds SM-2 fields not a 0–5 familiarity ladder; schedule logic must be pure and unit-tested against golden examples in the spec.
