-- Phase 6: indexes for queue/stats/export and browse order.
-- Idempotent (IF NOT EXISTS).

CREATE INDEX IF NOT EXISTS idx_reviews_user_reviewed ON reviews(user_id, reviewed_at);
CREATE INDEX IF NOT EXISTS idx_cards_user_phase ON cards(user_id, phase);
CREATE INDEX IF NOT EXISTS idx_cards_user_due_phase ON cards(user_id, phase, due_at);
CREATE INDEX IF NOT EXISTS idx_articles_fetched ON articles(fetched_at DESC, id DESC);
-- words(lemma, reading) already UNIQUE; sentence_words / sentences indexes in 001.
