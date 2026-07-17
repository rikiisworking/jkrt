-- Phase 1 schema — canonical (DEVELOPMENT_PLAN.md).
-- users matches internal/auth UsersTableDDL for Phase 0 upgrade compatibility.

CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY,
	password_hash TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS news_sources (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	feed_url TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	notes TEXT
);

CREATE TABLE IF NOT EXISTS articles (
	id INTEGER PRIMARY KEY,
	source_id INTEGER NOT NULL REFERENCES news_sources(id),
	external_id TEXT NOT NULL,
	title TEXT NOT NULL,
	url TEXT NOT NULL,
	fetched_at TEXT NOT NULL,
	raw_text TEXT NOT NULL,
	UNIQUE(source_id, external_id)
);

CREATE TABLE IF NOT EXISTS sentences (
	id INTEGER PRIMARY KEY,
	article_id INTEGER NOT NULL REFERENCES articles(id),
	text TEXT NOT NULL,
	order_index INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS words (
	id INTEGER PRIMARY KEY,
	lemma TEXT NOT NULL,
	reading TEXT NOT NULL,
	UNIQUE(lemma, reading)
);

CREATE TABLE IF NOT EXISTS sentence_words (
	id INTEGER PRIMARY KEY,
	sentence_id INTEGER NOT NULL REFERENCES sentences(id),
	word_id INTEGER NOT NULL REFERENCES words(id),
	surface TEXT NOT NULL,
	char_start INTEGER NOT NULL,
	char_end INTEGER NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cards (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id),
	word_id INTEGER NOT NULL REFERENCES words(id),
	phase TEXT NOT NULL,
	learning_step INTEGER NOT NULL,
	interval_days REAL NOT NULL,
	ease REAL NOT NULL,
	due_at TEXT NOT NULL,
	reps INTEGER NOT NULL,
	lapses INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(user_id, word_id)
);

CREATE TABLE IF NOT EXISTS reviews (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id),
	card_id INTEGER NOT NULL REFERENCES cards(id),
	sentence_id INTEGER NOT NULL REFERENCES sentences(id),
	grade TEXT NOT NULL,
	reviewed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sentence_words_sentence ON sentence_words(sentence_id);
CREATE INDEX IF NOT EXISTS idx_sentence_words_word ON sentence_words(word_id);
CREATE INDEX IF NOT EXISTS idx_cards_user_due ON cards(user_id, due_at);
CREATE INDEX IF NOT EXISTS idx_sentences_article ON sentences(article_id);
