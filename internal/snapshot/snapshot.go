// Package snapshot assembles Learner queue + library numbers for one call site.
// HTTP dashboard/stats/export and export dumps all consume Load — no triple glue.
package snapshot

import (
	"fmt"
	"time"

	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/review"
)

// View is the composed Learner status (queue caps + library aggregates).
type View struct {
	AsOf    time.Time
	Queue   review.Stats
	Library db.LibraryCounts
}

// Load fetches Review queue stats and library counts at now (UTC).
// rev may be nil (queue fields stay zero; SessionLimit/NewPerDay unset).
// database is required.
func Load(rev *review.Service, database *db.DB, userID int64, now time.Time) (View, error) {
	if database == nil {
		return View{}, fmt.Errorf("db is nil")
	}
	now = now.UTC()
	v := View{AsOf: now}

	if rev != nil {
		st, err := rev.Stats(userID, now)
		if err != nil {
			return View{}, fmt.Errorf("queue stats: %w", err)
		}
		v.Queue = st
	}

	lib, err := database.LibraryCounts(userID)
	if err != nil {
		return View{}, fmt.Errorf("library counts: %w", err)
	}
	v.Library = lib
	return v, nil
}
