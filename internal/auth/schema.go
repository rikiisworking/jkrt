package auth

// UsersTableDDL documents the users table shape for reference.
// Runtime schema is applied by migrations/001_init.sql (same columns/types).
//
// Matches DEVELOPMENT_PLAN.md: id INTEGER PK, password_hash TEXT, created_at TEXT.
const UsersTableDDL = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY,
	password_hash TEXT NOT NULL,
	created_at TEXT NOT NULL
);
`
