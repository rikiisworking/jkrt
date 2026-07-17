# Morphological analyzer extracts Words

Japanese has no spaces; Word boundaries and readings must come from somewhere. v1 uses a **morphological analyzer** (pure-Go preferred, e.g. Kagome) to emit Tokens; Tokens with ≥1 kanji become Word candidates with lemma and reading.

**Considered:** dictionary longest-match only (misses/splits badly); manual tagging only (kills automation); analyzer (chosen); hybrid correction UI later if quality hurts.

**Consequences:** Dictionary version is part of behavioral compatibility; wrong lemma/reading creates wrong Card identity until we add merge/edit tools.
