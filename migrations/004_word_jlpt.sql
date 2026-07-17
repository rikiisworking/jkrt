-- Cache of JLPT-ish levels for Words (embed misses classified via headless Grok).
CREATE TABLE IF NOT EXISTS word_jlpt_levels (
  lemma      TEXT NOT NULL,
  reading    TEXT NOT NULL,
  level      TEXT NOT NULL,
  source     TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (lemma, reading)
);
