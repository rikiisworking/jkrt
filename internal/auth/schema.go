package auth

// UsersTableDDL is the canonical Phase 0 users table definition.
//
// Phase 1 migrations/001_init.sql MUST use the same columns and types for
// `users` (prefer CREATE TABLE IF NOT EXISTS) so existing Phase 0 databases
// keep working after upgrade.
//
// Matches DEVELOPMENT_PLAN.md: id INTEGER PK, password_hash TEXT, created_at TEXT.
const UsersTableDDL = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY,
	password_hash TEXT NOT NULL,
	created_at TEXT NOT NULL
);
`
