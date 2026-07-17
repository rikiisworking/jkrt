package review_test

import (
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
}
