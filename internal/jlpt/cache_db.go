package jlpt

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SQLCache stores classified levels in SQLite (word_jlpt_levels).
type SQLCache struct {
	SQL *sql.DB
}

// Get implements Cache.
func (c *SQLCache) Get(lemma, reading string) (Level, bool, error) {
	if c == nil || c.SQL == nil {
		return "", false, fmt.Errorf("jlpt cache: nil")
	}
	var lv string
	err := c.SQL.QueryRow(
		`SELECT level FROM word_jlpt_levels WHERE lemma = ? AND reading = ?`,
		lemma, reading,
	).Scan(&lv)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	parsed, ok := ParseLevel(lv)
	if !ok {
		return "", false, nil
	}
	return parsed, true, nil
}

// Put implements Cache.
func (c *SQLCache) Put(lemma, reading string, level Level, source string) error {
	if c == nil || c.SQL == nil {
		return fmt.Errorf("jlpt cache: nil")
	}
	lemma = strings.TrimSpace(lemma)
	reading = strings.TrimSpace(reading)
	if lemma == "" || reading == "" {
		return fmt.Errorf("jlpt cache: empty key")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := c.SQL.Exec(
		`INSERT INTO word_jlpt_levels (lemma, reading, level, source, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(lemma, reading) DO UPDATE SET
		   level = excluded.level,
		   source = excluded.source,
		   updated_at = excluded.updated_at`,
		lemma, reading, string(level), source, now,
	)
	return err
}
