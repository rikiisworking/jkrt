package snapshot_test

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/review"
	"github.com/rikiisworking/jkrt/internal/schedule"
	"github.com/rikiisworking/jkrt/internal/snapshot"
)

func TestLoadEmpty(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	rev := review.New(d, schedule.DefaultParams())

	v, err := snapshot.Load(rev, d, db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !v.AsOf.Equal(now) {
		t.Fatalf("AsOf: %v", v.AsOf)
	}
	if v.Queue.NewCount != 0 || v.Library.Articles != 0 {
		t.Fatalf("empty view: %+v", v)
	}
	if v.Queue.SessionLimit != schedule.DefaultParams().SessionLimit {
		t.Fatalf("session limit: %d", v.Queue.SessionLimit)
	}
}

func TestLoadAfterIngest(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}
	rev := review.New(d, schedule.DefaultParams())
	v, err := snapshot.Load(rev, d, db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Library.Articles != 1 || v.Library.Cards < 1 {
		t.Fatalf("library: %+v", v.Library)
	}
	if v.Queue.NewCount < 1 {
		t.Fatalf("queue new: %+v", v.Queue)
	}
}

func TestLoadNilDB(t *testing.T) {
	_, err := snapshot.Load(nil, nil, 1, time.Now().UTC())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadNilReviewStillLoadsLibrary(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済。", a, now); err != nil {
		t.Fatal(err)
	}
	v, err := snapshot.Load(nil, d, db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Library.Cards < 1 {
		t.Fatalf("library: %+v", v.Library)
	}
	if v.Queue.NewCount != 0 {
		t.Fatalf("nil review should leave queue zero: %+v", v.Queue)
	}
}

func TestLoadNormalizesAsOfToUTC(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	loc := time.FixedZone("JST", 9*3600)
	now := time.Date(2026, 7, 17, 21, 0, 0, 0, loc)
	rev := review.New(d, schedule.DefaultParams())
	v, err := snapshot.Load(rev, d, db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if v.AsOf.Location() != time.UTC {
		t.Fatalf("AsOf location: %v", v.AsOf.Location())
	}
	if !v.AsOf.Equal(now.UTC()) {
		t.Fatalf("AsOf: got %v want %v", v.AsOf, now.UTC())
	}
}

// Load queue fields must match review.Stats (single composition source).
func TestLoadQueueMatchesReviewStats(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}
	rev := review.New(d, schedule.DefaultParams())
	res, err := rev.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	if err := rev.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", res.Item.UpdatedAt, now); err != nil {
		t.Fatal(err)
	}

	st, err := rev.Stats(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	v, err := snapshot.Load(rev, d, db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Queue != st {
		t.Fatalf("queue mismatch:\n  Load:  %+v\n  Stats: %+v", v.Queue, st)
	}
	if v.Queue.ReviewsToday != 1 || v.Library.Reviews != 1 {
		t.Fatalf("expected one review: queue=%+v lib=%+v", v.Queue, v.Library)
	}
	// After one grade out of new, queue NewCount and ByPhase["new"] should agree.
	if v.Queue.NewCount != v.Library.ByPhase["new"] {
		t.Fatalf("new count queue=%d phase=%d", v.Queue.NewCount, v.Library.ByPhase["new"])
	}
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	d, err := db.Open(path, migrationsDir(t))
	if err != nil {
		t.Fatal(err)
	}
	d.AllowAllWords()
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func seedUser(t *testing.T, d *db.DB) {
	t.Helper()
	_, err := d.SQL().Exec(
		`INSERT INTO users (id, password_hash, created_at) VALUES (1, 'x', ?)`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}
