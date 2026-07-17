package db

import (
	"fmt"
)

// LibraryCounts is aggregate library + Card phase breakdown for stats.
type LibraryCounts struct {
	Articles  int
	Sentences int
	Words     int
	Cards     int
	Reviews   int
	// ByPhase keys: new, learning, review, relearning (missing keys mean 0).
	ByPhase map[string]int
	// MatureCards: phase=review AND interval_days >= schedule.Params.ComfortableIntervalDays.
	MatureCards int
}

// LibraryCounts returns totals for the Learner library and Card phases.
// Mature threshold comes from SetScheduleParams / schedule.DefaultParams (not a forked constant).
func (d *DB) LibraryCounts(userID int64) (LibraryCounts, error) {
	if d == nil || d.sql == nil {
		return LibraryCounts{}, fmt.Errorf("db is nil")
	}
	var c LibraryCounts
	c.ByPhase = map[string]int{
		"new": 0, "learning": 0, "review": 0, "relearning": 0,
	}

	// Same fallback as schedule.IsUnfamiliar when ComfortableIntervalDays is 0.
	threshold := d.scheduleParams().ComfortableIntervalDays
	if threshold == 0 {
		threshold = 21
	}

	queries := []struct {
		dest *int
		sql  string
		args []any
	}{
		{&c.Articles, `SELECT COUNT(1) FROM articles`, nil},
		{&c.Sentences, `SELECT COUNT(1) FROM sentences`, nil},
		{&c.Words, `SELECT COUNT(1) FROM words`, nil},
		{&c.Cards, `SELECT COUNT(1) FROM cards WHERE user_id = ?`, []any{userID}},
		{&c.Reviews, `SELECT COUNT(1) FROM reviews WHERE user_id = ?`, []any{userID}},
		{&c.MatureCards, `SELECT COUNT(1) FROM cards WHERE user_id = ? AND phase = 'review' AND interval_days >= ?`, []any{userID, threshold}},
	}
	for _, q := range queries {
		if err := d.sql.QueryRow(q.sql, q.args...).Scan(q.dest); err != nil {
			return LibraryCounts{}, fmt.Errorf("count: %w", err)
		}
	}

	rows, err := d.sql.Query(
		`SELECT phase, COUNT(1) FROM cards WHERE user_id = ? GROUP BY phase`,
		userID,
	)
	if err != nil {
		return LibraryCounts{}, fmt.Errorf("phase counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var phase string
		var n int
		if err := rows.Scan(&phase, &n); err != nil {
			return LibraryCounts{}, err
		}
		c.ByPhase[phase] = n
	}
	if err := rows.Err(); err != nil {
		return LibraryCounts{}, err
	}
	return c, nil
}
