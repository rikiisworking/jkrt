package db

import "time"

// IsUnfamiliar reports whether a Word/Card should be highlighted as unfamiliar
// in sentence context (DEVELOPMENT_PLAN locked predicate).
//
//	phase IN (new, learning, relearning)
//	OR due_at <= now
//	OR (phase == review AND interval_days < 21)
func IsUnfamiliar(phase string, intervalDays float64, dueAt, now time.Time) bool {
	switch phase {
	case "new", "learning", "relearning":
		return true
	}
	if !dueAt.After(now) {
		return true
	}
	if phase == "review" && intervalDays < 21 {
		return true
	}
	return false
}
