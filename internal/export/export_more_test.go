package export

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/review"
	"github.com/rikiisworking/jkrt/internal/schedule"
)

// Truncation uses LIMIT limit+1 then slices — caps must set Truncated=true.
func TestLoadCardsAndReviewsTruncation(t *testing.T) {
	d := openDB(t)
	seedUser(t, d)
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	// Enough kanji words for multiple cards.
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。政府は対策を検討する。", a, now); err != nil {
		t.Fatal(err)
	}
	var cardN int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM cards WHERE user_id = 1`).Scan(&cardN); err != nil {
		t.Fatal(err)
	}
	if cardN < 2 {
		t.Fatalf("need ≥2 cards for truncation test, got %d", cardN)
	}

	svc := New(d)
	// limit 1 → at least 2 rows available → truncated
	cards, trunc, err := svc.loadCards(db.LearnerUserID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !trunc {
		t.Fatal("expected cards truncated")
	}
	if len(cards) != 1 {
		t.Fatalf("cards len: %d", len(cards))
	}

	// Grade a few cards so we have review history.
	rev := review.New(d, schedule.DefaultParams())
	for i := 0; i < 2; i++ {
		res, err := rev.Next(db.LearnerUserID, now)
		if err != nil || res.Empty {
			t.Fatalf("next %d: empty=%v err=%v", i, res.Empty, err)
		}
		if err := rev.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "easy", res.Item.UpdatedAt, now); err != nil {
			t.Fatal(err)
		}
	}
	reviews, rTrunc, err := svc.loadReviews(db.LearnerUserID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !rTrunc {
		t.Fatal("expected reviews truncated")
	}
	if len(reviews) != 1 {
		t.Fatalf("reviews len: %d", len(reviews))
	}
}

func TestWriteCardsCSVEmptyAndSpecialChars(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCardsCSV(&buf, nil); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "lemma,reading,phase") {
		t.Fatalf("header: %q", got)
	}
	// Only header line (+ possible trailing newline)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 1 {
		t.Fatalf("empty cards should be header only: %v", lines)
	}

	buf.Reset()
	rows := []CardRow{{
		Lemma:   "経済,政策",
		Reading: "けいざい",
		Phase:   "new",
		Ease:    2.5,
	}}
	if err := WriteCardsCSV(&buf, rows); err != nil {
		t.Fatal(err)
	}
	// encoding/csv quotes fields that contain commas
	if !strings.Contains(buf.String(), `"経済,政策"`) {
		t.Fatalf("csv should quote comma in lemma: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "けいざい") {
		t.Fatalf("missing reading: %s", buf.String())
	}
}

func TestWriteJSONFailingWriter(t *testing.T) {
	err := WriteJSON(failWriter{}, Snapshot{UserID: 1, Cards: []CardRow{}, Reviews: []ReviewRow{}})
	if err == nil {
		t.Fatal("expected encode error")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errWrite
}

var errWrite = errString("write failed")

type errString string

func (e errString) Error() string { return string(e) }

func openDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	d, err := db.Open(path, migDir(t))
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

func migDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}
