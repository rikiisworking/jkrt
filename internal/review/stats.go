package review

import (
	"fmt"
	"time"
)

// Stats is dashboard queue / session progress (not a Review presentation).
type Stats struct {
	// DueCount: Cards past phase=new with due_at <= now (ready Review work).
	DueCount int
	// NewCount: Cards still in phase=new (subject to NewPerDay when shown).
	NewCount int
	// ReviewsToday: grade rows on this UTC calendar day.
	ReviewsToday int
	// NewIntroducedToday: Cards whose first grade fell on this UTC day.
	NewIntroducedToday int
	SessionLimit       int
	NewPerDay          int
}

// Stats returns due/new counts and today's progress under the same caps as Next.
func (s *Service) Stats(userID int64, now time.Time) (Stats, error) {
	if s == nil || s.sql == nil {
		return Stats{}, ErrNilService
	}
	now = now.UTC()
	nowStr := now.Format(time.RFC3339)

	var due, newN int
	err := s.sql.QueryRow(
		`SELECT COUNT(1) FROM cards
		 WHERE user_id = ? AND phase != 'new' AND due_at <= ?`,
		userID, nowStr,
	).Scan(&due)
	if err != nil {
		return Stats{}, fmt.Errorf("count due: %w", err)
	}
	err = s.sql.QueryRow(
		`SELECT COUNT(1) FROM cards WHERE user_id = ? AND phase = 'new'`,
		userID,
	).Scan(&newN)
	if err != nil {
		return Stats{}, fmt.Errorf("count new: %w", err)
	}

	reviewsToday, err := s.countReviewsToday(userID, now)
	if err != nil {
		return Stats{}, err
	}
	introduced, err := s.countNewIntroducedToday(userID, now)
	if err != nil {
		return Stats{}, err
	}

	return Stats{
		DueCount:           due,
		NewCount:           newN,
		ReviewsToday:       reviewsToday,
		NewIntroducedToday: introduced,
		SessionLimit:       s.params.SessionLimit,
		NewPerDay:          s.params.NewPerDay,
	}, nil
}
