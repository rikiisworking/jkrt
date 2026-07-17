package export_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/export"
	"github.com/rikiisworking/jkrt/internal/review"
	"github.com/rikiisworking/jkrt/internal/schedule"
)

func TestParseFormat(t *testing.T) {
	for _, s := range []string{"", "json", "csv"} {
		if _, err := export.ParseFormat(s); err != nil {
			t.Fatalf("%q: %v", s, err)
		}
	}
	if _, err := export.ParseFormat("xml"); err == nil {
		t.Fatal("expected error for xml")
	}
}

func TestExportJSONAndCSVFixture(t *testing.T) {
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

	// One grade so reviews export is non-empty.
	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	if err := svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", res.Item.UpdatedAt, now); err != nil {
		t.Fatal(err)
	}

	queue, err := svc.Stats(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.LibraryCounts(db.LearnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if lib.Words < 1 || lib.Cards < 1 || lib.Reviews != 1 {
		t.Fatalf("library counts: %+v", lib)
	}

	exp := export.New(d)
	snap, err := exp.BuildSnapshot(db.LearnerUserID, queue, lib, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Cards) < 1 {
		t.Fatal("expected cards in snapshot")
	}
	if len(snap.Reviews) != 1 {
		t.Fatalf("reviews: %d", len(snap.Reviews))
	}
	// Fixture word present
	found := false
	for _, c := range snap.Cards {
		if c.Lemma == "経済" && c.Reading != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 経済 in export cards: %+v", snap.Cards)
	}

	var jbuf bytes.Buffer
	if err := export.WriteJSON(&jbuf, snap); err != nil {
		t.Fatal(err)
	}
	var decoded export.Snapshot
	if err := json.Unmarshal(jbuf.Bytes(), &decoded); err != nil {
		t.Fatalf("json: %v body=%s", err, jbuf.String())
	}
	if len(decoded.Cards) != len(snap.Cards) {
		t.Fatal("round-trip card count mismatch")
	}
	if !strings.Contains(jbuf.String(), "経済") {
		t.Fatal("json missing lemma")
	}

	var cbuf bytes.Buffer
	if err := export.WriteCardsCSV(&cbuf, snap.Cards); err != nil {
		t.Fatal(err)
	}
	csv := cbuf.String()
	if !strings.Contains(csv, "lemma,reading,phase") {
		t.Fatalf("csv header: %s", csv)
	}
	if !strings.Contains(csv, "経済") {
		t.Fatalf("csv missing lemma: %s", csv)
	}
}

func TestExportEmpty(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	exp := export.New(d)
	snap, err := exp.BuildSnapshot(db.LearnerUserID, review.Stats{}, db.LibraryCounts{ByPhase: map[string]int{}}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Cards == nil || snap.Reviews == nil {
		t.Fatal("want non-nil empty slices")
	}
	if len(snap.Cards) != 0 {
		t.Fatalf("cards: %d", len(snap.Cards))
	}
}

func TestExportNilService(t *testing.T) {
	exp := export.New(nil)
	_, err := exp.BuildSnapshot(1, review.Stats{}, db.LibraryCounts{}, time.Now().UTC())
	if err == nil {
		t.Fatal("expected error")
	}
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path, migrationsDir(t))
	if err != nil {
		t.Fatal(err)
	}
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
