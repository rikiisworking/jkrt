package review_test

import (
	"errors"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/review"
	"github.com/rikiisworking/jkrt/internal/schedule"
)

func TestStatsEmpty(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	svc := review.New(d, schedule.DefaultParams())
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	st, err := svc.Stats(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if st.DueCount != 0 || st.NewCount != 0 || st.ReviewsToday != 0 || st.NewIntroducedToday != 0 {
		t.Fatalf("empty stats: %+v", st)
	}
	if st.SessionLimit != schedule.DefaultParams().SessionLimit {
		t.Fatalf("session limit: %d", st.SessionLimit)
	}
	if st.NewPerDay != schedule.DefaultParams().NewPerDay {
		t.Fatalf("new per day: %d", st.NewPerDay)
	}
}

func TestStatsAfterIngestAndGrade(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	svc := review.New(d, schedule.DefaultParams())
	st, err := svc.Stats(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if st.NewCount < 1 {
		t.Fatalf("expected new cards, got %+v", st)
	}
	if st.DueCount != 0 {
		t.Fatalf("new cards are not due count: %+v", st)
	}
	newBefore := st.NewCount

	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	if err := svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", res.Item.UpdatedAt, now); err != nil {
		t.Fatal(err)
	}

	st, err = svc.Stats(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if st.ReviewsToday != 1 {
		t.Fatalf("reviews today: %d", st.ReviewsToday)
	}
	if st.NewIntroducedToday != 1 {
		t.Fatalf("new introduced: %d", st.NewIntroducedToday)
	}
	// Graded into learning with due in 1m — still not in DueCount at same now
	if st.DueCount != 0 {
		t.Fatalf("learning not yet due: %+v", st)
	}
	if st.NewCount != newBefore-1 {
		t.Fatalf("NewCount after grade: got %d want %d", st.NewCount, newBefore-1)
	}
	newAfter := st.NewCount

	// Make a learning card due and confirm DueCount.
	var learningID int64
	if err := d.SQL().QueryRow(
		`SELECT id FROM cards WHERE user_id = 1 AND phase = 'learning' LIMIT 1`,
	).Scan(&learningID); err != nil {
		t.Fatal(err)
	}
	past := now.Add(-time.Minute).Format(time.RFC3339)
	if _, err := d.SQL().Exec(`UPDATE cards SET due_at = ? WHERE id = ?`, past, learningID); err != nil {
		t.Fatal(err)
	}
	st, err = svc.Stats(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if st.DueCount != 1 {
		t.Fatalf("due after past due_at: got %d want 1 (%+v)", st.DueCount, st)
	}
	if st.NewCount != newAfter {
		t.Fatalf("new count should be stable when only due_at changes: %d vs %d", st.NewCount, newAfter)
	}
}

func TestStatsNilService(t *testing.T) {
	var svc *review.Service
	_, err := svc.Stats(1, time.Now().UTC())
	if !errors.Is(err, review.ErrNilService) {
		t.Fatalf("got %v want ErrNilService", err)
	}
}
