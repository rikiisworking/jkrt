-- Extract-on-tap: mark Sentences the learner opted into study.
-- NULL extracted_at = not in review deck yet.

ALTER TABLE sentences ADD COLUMN extracted_at TEXT;

CREATE INDEX IF NOT EXISTS idx_sentences_extracted ON sentences(article_id, extracted_at);

-- Backfill: prior full-extract data already has sentence_words.
UPDATE sentences SET extracted_at = (
	SELECT MIN(sw.created_at) FROM sentence_words sw WHERE sw.sentence_id = sentences.id
)
WHERE EXISTS (SELECT 1 FROM sentence_words sw WHERE sw.sentence_id = sentences.id)
  AND extracted_at IS NULL;
