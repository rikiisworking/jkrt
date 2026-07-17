package review_test

import (
	"errors"
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

func TestNextEmptyQueue(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	svc := review.New(d, schedule.DefaultParams())
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Empty {
		t.Fatal("expected empty queue")
	}
}

// Store-only scrape path leaves Next empty until Sentence extract (ADR 0006).
func TestNextEmptyUntilExtract(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store, err := d.StoreArticle(db.LearnerUserID, db.SourceRef{Name: "s"}, db.ArticleInput{
		ExternalID: "e1",
		RawText:    "経済政策を発表した。市場は反応した。",
		FetchedAt:  now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Empty {
		t.Fatal("queue must be empty before extract")
	}

	var sid int64
	if err := d.SQL().QueryRow(
		`SELECT id FROM sentences WHERE article_id = ? ORDER BY order_index ASC LIMIT 1`,
		store.ArticleID,
	).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExtractSentence(db.LearnerUserID, sid, a, now); err != nil {
		t.Fatal(err)
	}
	res2, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res2.Empty {
		t.Fatalf("expected card after extract: empty=%v err=%v", res2.Empty, err)
	}
	if res2.Item.SentenceID != sid {
		t.Fatalf("context sentence: got %d want extracted %d", res2.Item.SentenceID, sid)
	}
	if res2.Item.Sentence == "" || res2.Item.Lemma == "" {
		t.Fatalf("item incomplete: %+v", res2.Item)
	}
}

func TestNextNilService(t *testing.T) {
	var svc *review.Service
	_, err := svc.Next(1, time.Now().UTC())
	if !errors.Is(err, review.ErrNilService) {
		t.Fatalf("got %v want ErrNilService", err)
	}
	svc = review.New(nil, schedule.DefaultParams())
	_, err = svc.Next(1, time.Now().UTC())
	if !errors.Is(err, review.ErrNilService) {
		t.Fatalf("nil db: got %v want ErrNilService", err)
	}
}

func TestNextPrefersDueOverNew(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	var dueID int64
	err := d.SQL().QueryRow(`SELECT id FROM cards WHERE user_id = 1 ORDER BY id ASC LIMIT 1`).Scan(&dueID)
	if err != nil {
		t.Fatal(err)
	}
	past := now.Add(-time.Hour).Format(time.RFC3339)
	_, err = d.SQL().Exec(
		`UPDATE cards SET phase = 'learning', due_at = ?, learning_step = 0 WHERE id = ?`,
		past, dueID,
	)
	if err != nil {
		t.Fatal(err)
	}

	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Empty {
		t.Fatal("expected a card")
	}
	if res.Item.CardID != dueID {
		t.Fatalf("expected due card %d, got %d (phase %s)", dueID, res.Item.CardID, res.Item.Phase)
	}
	if res.Item.SentenceID == 0 || res.Item.Sentence == "" {
		t.Fatal("expected sentence context")
	}
	if res.Item.WordID == 0 || res.Item.Lemma == "" || res.Item.Reading == "" {
		t.Fatalf("expected word identity on item: %+v", res.Item)
	}
	var focus bool
	for _, sp := range res.Item.Spans {
		if sp.Focus {
			focus = true
			if !sp.Unfamiliar {
				t.Fatal("focus learning card should be unfamiliar")
			}
			if sp.WordID != res.Item.WordID {
				t.Fatalf("focus span word %d != item word %d", sp.WordID, res.Item.WordID)
			}
		}
	}
	if !focus {
		t.Fatal("expected focus span")
	}
}

// Due cards are ordered by due_at ASC (then id).
func TestNextDueOrderEarliestFirst(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	var ids []int64
	rows, err := d.SQL().Query(`SELECT id FROM cards WHERE user_id = 1 ORDER BY id ASC`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) < 2 {
		t.Fatalf("need ≥2 cards, got %d", len(ids))
	}

	// ids[0] due later, ids[1] due earlier → Next should return ids[1]
	later := now.Add(-time.Hour).Format(time.RFC3339)
	earlier := now.Add(-2 * time.Hour).Format(time.RFC3339)
	_, _ = d.SQL().Exec(`UPDATE cards SET phase = 'learning', due_at = ? WHERE id = ?`, later, ids[0])
	_, _ = d.SQL().Exec(`UPDATE cards SET phase = 'learning', due_at = ? WHERE id = ?`, earlier, ids[1])

	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatalf("next: empty=%v err=%v", res.Empty, err)
	}
	if res.Item.CardID != ids[1] {
		t.Fatalf("want earliest due card %d, got %d", ids[1], res.Item.CardID)
	}
}

func TestNextNewUnderCap(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	p := schedule.DefaultParams()
	p.NewPerDay = 1
	p.SessionLimit = 40
	svc := review.New(d, p)

	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatalf("first next: empty=%v err=%v", res.Empty, err)
	}
	if res.Item.Phase != "new" {
		t.Fatalf("first card phase: %s want new", res.Item.Phase)
	}
	firstID := res.Item.CardID
	sid := res.Item.SentenceID

	if err := svc.Grade(db.LearnerUserID, firstID, sid, "good", res.Item.UpdatedAt, now); err != nil {
		t.Fatal(err)
	}

	// Learning due in 1m → not due yet; NewPerDay exhausted → empty
	res2, err := svc.Next(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Empty {
		t.Fatalf("want empty after NewPerDay=1; got card %d phase %s", res2.Item.CardID, res2.Item.Phase)
	}

	later := now.Add(time.Minute)
	res3, err := svc.Next(db.LearnerUserID, later)
	if err != nil || res3.Empty {
		t.Fatalf("due after step: empty=%v err=%v", res3.Empty, err)
	}
	if res3.Item.CardID != firstID {
		t.Fatalf("want same card due, got %d want %d", res3.Item.CardID, firstID)
	}
}

// NewPerDay uses UTC day of first review; next calendar day opens new quota.
func TestNewPerDayResetsNextUTCDay(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	day1 := time.Date(2026, 7, 17, 23, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 7, 18, 1, 0, 0, 0, time.UTC)

	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, day1); err != nil {
		t.Fatal(err)
	}

	p := schedule.DefaultParams()
	p.NewPerDay = 1
	svc := review.New(d, p)

	res, err := svc.Next(db.LearnerUserID, day1)
	if err != nil || res.Empty {
		t.Fatal("need new card day1")
	}
	if err := svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "easy", res.Item.UpdatedAt, day1); err != nil {
		t.Fatal(err)
	}
	// Easy → review due in 4d; not due on day2. NewPerDay should allow another new.
	res2, err := svc.Next(db.LearnerUserID, day2)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Empty {
		t.Fatal("want new card available on next UTC day")
	}
	if res2.Item.Phase != "new" {
		t.Fatalf("phase: %s want new", res2.Item.Phase)
	}
	if res2.Item.CardID == res.Item.CardID {
		t.Fatal("should be a different new card")
	}
}

func TestGradeUpdatesDueAndReviewRow(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatalf("next: %+v %v", res, err)
	}

	err = svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", res.Item.UpdatedAt, now)
	if err != nil {
		t.Fatal(err)
	}

	var phase string
	var dueAt string
	var step int
	err = d.SQL().QueryRow(
		`SELECT phase, due_at, learning_step FROM cards WHERE id = ?`, res.Item.CardID,
	).Scan(&phase, &dueAt, &step)
	if err != nil {
		t.Fatal(err)
	}
	if phase != "learning" || step != 0 {
		t.Fatalf("after good: phase=%s step=%d", phase, step)
	}
	wantDue := now.Add(time.Minute).Format(time.RFC3339)
	if dueAt != wantDue {
		t.Fatalf("due_at: got %q want %q", dueAt, wantDue)
	}

	var grade string
	var sentID int64
	var reviewedAt string
	err = d.SQL().QueryRow(
		`SELECT grade, sentence_id, reviewed_at FROM reviews WHERE card_id = ?`, res.Item.CardID,
	).Scan(&grade, &sentID, &reviewedAt)
	if err != nil {
		t.Fatal(err)
	}
	if grade != "good" || sentID != res.Item.SentenceID {
		t.Fatalf("review row: grade=%s sent=%d", grade, sentID)
	}
	if reviewedAt != now.Format(time.RFC3339) {
		t.Fatalf("reviewed_at: got %q want %q", reviewedAt, now.Format(time.RFC3339))
	}
}

// Grade persists full SM-2 state for a review-phase Again (lapse).
func TestGradeAgainLapsePersists(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	var cardID, wordID int64
	if err := d.SQL().QueryRow(`SELECT id, word_id FROM cards LIMIT 1`).Scan(&cardID, &wordID); err != nil {
		t.Fatal(err)
	}
	var sentenceID int64
	if err := d.SQL().QueryRow(
		`SELECT sentence_id FROM sentence_words WHERE word_id = ? LIMIT 1`, wordID,
	).Scan(&sentenceID); err != nil {
		t.Fatal(err)
	}

	// Put card in review with known ease/interval
	updatedAt := now.Add(-time.Hour).Format(time.RFC3339)
	_, err := d.SQL().Exec(
		`UPDATE cards SET phase = 'review', interval_days = 10, ease = 2.5,
		 learning_step = 0, due_at = ?, reps = 2, lapses = 0, updated_at = ? WHERE id = ?`,
		now.Format(time.RFC3339), updatedAt, cardID,
	)
	if err != nil {
		t.Fatal(err)
	}

	svc := review.New(d, schedule.DefaultParams())
	if err := svc.Grade(db.LearnerUserID, cardID, sentenceID, "again", updatedAt, now); err != nil {
		t.Fatal(err)
	}

	var phase string
	var ease float64
	var lapses, step, reps int
	var interval float64
	var dueAt string
	err = d.SQL().QueryRow(
		`SELECT phase, ease, lapses, learning_step, reps, interval_days, due_at FROM cards WHERE id = ?`,
		cardID,
	).Scan(&phase, &ease, &lapses, &step, &reps, &interval, &dueAt)
	if err != nil {
		t.Fatal(err)
	}
	if phase != "relearning" || step != 0 || lapses != 1 {
		t.Fatalf("lapse state: phase=%s step=%d lapses=%d", phase, step, lapses)
	}
	if !almostEqual(ease, 2.3) {
		t.Fatalf("ease: got %v want 2.3", ease)
	}
	if !almostEqual(interval, 10) {
		t.Fatalf("interval kept: got %v want 10", interval)
	}
	if dueAt != now.Add(time.Minute).Format(time.RFC3339) {
		t.Fatalf("due_at: %s", dueAt)
	}
	// reps unchanged on Again from review
	if reps != 2 {
		t.Fatalf("reps: got %d want 2", reps)
	}
}

func TestGradeAllWireValues(t *testing.T) {
	// Smoke: each wire grade is accepted without error (math covered by schedule G-tests).
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	// Enough kanji words for four distinct new cards.
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。政府は対策を検討する。", a, now); err != nil {
		t.Fatal(err)
	}
	svc := review.New(d, schedule.DefaultParams())

	// One new card per grade; grade far from learning due so Next keeps serving new cards.
	for _, g := range []string{"again", "hard", "good", "easy"} {
		res, err := svc.Next(db.LearnerUserID, now)
		if err != nil || res.Empty {
			t.Fatalf("grade %s: need card empty=%v err=%v", g, res.Empty, err)
		}
		if res.Item.Phase != "new" {
			t.Fatalf("grade %s: expected new card, got phase %s", g, res.Item.Phase)
		}
		if err := svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, g, res.Item.UpdatedAt, now); err != nil {
			t.Fatalf("grade %s: %v", g, err)
		}
	}
}

func TestGradeBadInput(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}
	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need a card")
	}

	err = svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "maybe", res.Item.UpdatedAt, now)
	if !errors.Is(err, review.ErrBadGrade) {
		t.Fatalf("bad grade: got %v want ErrBadGrade", err)
	}

	err = svc.Grade(db.LearnerUserID, 99999, res.Item.SentenceID, "good", res.Item.UpdatedAt, now)
	if !errors.Is(err, review.ErrNotFound) {
		t.Fatalf("missing card: got %v want ErrNotFound", err)
	}

	// Wrong user
	err = svc.Grade(2, res.Item.CardID, res.Item.SentenceID, "good", res.Item.UpdatedAt, now)
	if !errors.Is(err, review.ErrNotFound) {
		t.Fatalf("wrong user: got %v want ErrNotFound", err)
	}

	var articleID int64
	if err := d.SQL().QueryRow(`SELECT id FROM articles LIMIT 1`).Scan(&articleID); err != nil {
		t.Fatal(err)
	}
	r, err := d.SQL().Exec(`INSERT INTO sentences (article_id, text, order_index) VALUES (?, '別。', 99)`, articleID)
	if err != nil {
		t.Fatal(err)
	}
	badSID, _ := r.LastInsertId()
	err = svc.Grade(db.LearnerUserID, res.Item.CardID, badSID, "good", res.Item.UpdatedAt, now)
	if !errors.Is(err, review.ErrSentenceNotLinked) {
		t.Fatalf("bad sentence: got %v want ErrSentenceNotLinked", err)
	}
}

func TestSessionLimitBlocks(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	p := schedule.DefaultParams()
	p.SessionLimit = 1
	svc := review.New(d, p)

	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("expected card")
	}
	if err := svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", res.Item.UpdatedAt, now); err != nil {
		t.Fatal(err)
	}
	res2, err := svc.Next(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Empty {
		t.Fatal("session limit should yield empty")
	}
}

// Session limit is UTC-day scoped: next day is open again.
func TestSessionLimitResetsNextUTCDay(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	day1 := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, day1); err != nil {
		t.Fatal(err)
	}

	p := schedule.DefaultParams()
	p.SessionLimit = 1
	svc := review.New(d, p)

	res, err := svc.Next(db.LearnerUserID, day1)
	if err != nil || res.Empty {
		t.Fatal("need card day1")
	}
	if err := svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "easy", res.Item.UpdatedAt, day1); err != nil {
		t.Fatal(err)
	}
	if res2, err := svc.Next(db.LearnerUserID, day1); err != nil || !res2.Empty {
		t.Fatalf("day1 should be empty after session limit; empty=%v err=%v", res2.Empty, err)
	}

	res3, err := svc.Next(db.LearnerUserID, day2)
	if err != nil {
		t.Fatal(err)
	}
	if res3.Empty {
		t.Fatal("session should open on next UTC day")
	}
}

func TestNewestSentenceContext(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	t1 := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 17, 14, 0, 0, 0, time.UTC)

	if _, err := d.IngestText(db.LearnerUserID, "経済を見る。", a, t1); err != nil {
		t.Fatal(err)
	}
	if _, err := d.IngestText(db.LearnerUserID, "経済は重要だ。", a, t2); err != nil {
		t.Fatal(err)
	}

	var cardID int64
	err := d.SQL().QueryRow(
		`SELECT c.id FROM cards c JOIN words w ON w.id = c.word_id WHERE w.lemma = '経済'`,
	).Scan(&cardID)
	if err != nil {
		t.Fatalf("find 経済 card: %v", err)
	}

	// Only this card is due; block other new cards so Next is deterministic.
	_, err = d.SQL().Exec(
		`UPDATE cards SET phase = 'learning', due_at = ? WHERE id = ?`,
		t2.Add(-time.Minute).Format(time.RFC3339), cardID,
	)
	if err != nil {
		t.Fatal(err)
	}
	// Other new cards stay new but NewPerDay=0 → only due path
	p := schedule.DefaultParams()
	p.NewPerDay = 0
	svc := review.New(d, p)

	res, err := svc.Next(db.LearnerUserID, t2)
	if err != nil || res.Empty {
		t.Fatalf("next: empty=%v err=%v", res.Empty, err)
	}
	if res.Item.CardID != cardID {
		t.Fatalf("want 経済 card %d, got %d lemma=%q", cardID, res.Item.CardID, res.Item.Lemma)
	}
	if res.Item.Sentence != "経済は重要だ。" {
		t.Fatalf("want newest sentence, got %q", res.Item.Sentence)
	}
}

// Unfamiliar highlight: focus + young review words true; mature (interval ≥ 21) false.
func TestNextUnfamiliarHighlightMatrix(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	// One sentence with multiple kanji words so spans share context.
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	// Pick three distinct cards by lemma when present.
	type row struct {
		id, wordID int64
		lemma      string
	}
	rows, err := d.SQL().Query(`
		SELECT c.id, c.word_id, w.lemma FROM cards c
		JOIN words w ON w.id = c.word_id
		WHERE c.user_id = 1 ORDER BY c.id ASC`)
	if err != nil {
		t.Fatal(err)
	}
	var cards []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.wordID, &r.lemma); err != nil {
			t.Fatal(err)
		}
		cards = append(cards, r)
	}
	_ = rows.Close()
	if len(cards) < 2 {
		t.Fatalf("need ≥2 cards from fixture sentence, got %d", len(cards))
	}

	focus := cards[0]
	// Put focus in learning (due) so Next selects it.
	updatedAt := now.Add(-time.Hour).Format(time.RFC3339)
	_, err = d.SQL().Exec(
		`UPDATE cards SET phase = 'learning', due_at = ?, learning_step = 0,
		 interval_days = 0, ease = 2.5, reps = 0, updated_at = ? WHERE id = ?`,
		now.Add(-time.Minute).Format(time.RFC3339), updatedAt, focus.id,
	)
	if err != nil {
		t.Fatal(err)
	}

	// If we have a second card, make it mature review (not due, interval 21).
	var mature *row
	var young *row
	if len(cards) >= 2 {
		mature = &cards[1]
		_, err = d.SQL().Exec(
			`UPDATE cards SET phase = 'review', interval_days = 21, ease = 2.5,
			 due_at = ?, reps = 3, lapses = 0, updated_at = ? WHERE id = ?`,
			now.Add(48*time.Hour).Format(time.RFC3339), updatedAt, mature.id,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(cards) >= 3 {
		young = &cards[2]
		_, err = d.SQL().Exec(
			`UPDATE cards SET phase = 'review', interval_days = 5, ease = 2.5,
			 due_at = ?, reps = 2, lapses = 0, updated_at = ? WHERE id = ?`,
			now.Add(24*time.Hour).Format(time.RFC3339), updatedAt, young.id,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Freeze other new cards out of the queue.
	p := schedule.DefaultParams()
	p.NewPerDay = 0
	svc := review.New(d, p)

	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatalf("next: empty=%v err=%v", res.Empty, err)
	}
	if res.Item.CardID != focus.id {
		t.Fatalf("want focus card %d, got %d lemma=%q", focus.id, res.Item.CardID, res.Item.Lemma)
	}

	byWord := map[int64]review.Span{}
	for _, sp := range res.Item.Spans {
		if sp.WordID != 0 {
			byWord[sp.WordID] = sp
		}
	}
	fs, ok := byWord[focus.wordID]
	if !ok || !fs.Focus {
		t.Fatalf("focus span missing: %+v", byWord)
	}
	if !fs.Unfamiliar {
		t.Fatal("focus learning card must be unfamiliar")
	}
	if mature != nil {
		ms, ok := byWord[mature.wordID]
		if !ok {
			t.Fatalf("mature word not in spans: word_id=%d", mature.wordID)
		}
		if ms.Unfamiliar {
			t.Fatalf("mature interval=21 should not be unfamiliar: %+v", ms)
		}
		if ms.Focus {
			t.Fatal("mature must not be focus")
		}
	}
	if young != nil {
		ys, ok := byWord[young.wordID]
		if !ok {
			t.Fatalf("young word not in spans: word_id=%d", young.wordID)
		}
		if !ys.Unfamiliar {
			t.Fatalf("young review interval=5 should be unfamiliar: %+v", ys)
		}
		if ys.Focus {
			t.Fatal("young must not be focus")
		}
	}
}

// Grade records the Sentence that was shown, even if a newer occurrence exists.
func TestGradeKeepsShownSentence(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	t1 := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 17, 14, 0, 0, 0, time.UTC)

	if _, err := d.IngestText(db.LearnerUserID, "経済を見る。", a, t1); err != nil {
		t.Fatal(err)
	}
	if _, err := d.IngestText(db.LearnerUserID, "経済は重要だ。", a, t2); err != nil {
		t.Fatal(err)
	}

	var cardID, wordID int64
	var updatedAt string
	err := d.SQL().QueryRow(
		`SELECT c.id, c.word_id, c.updated_at FROM cards c
		 JOIN words w ON w.id = c.word_id WHERE w.lemma = '経済'`,
	).Scan(&cardID, &wordID, &updatedAt)
	if err != nil {
		t.Fatal(err)
	}

	// Older sentence containing 経済
	var olderSentID int64
	err = d.SQL().QueryRow(`
		SELECT s.id FROM sentences s
		JOIN sentence_words sw ON sw.sentence_id = s.id
		WHERE sw.word_id = ? AND s.text = '経済を見る。'`, wordID,
	).Scan(&olderSentID)
	if err != nil {
		t.Fatalf("older sentence: %v", err)
	}
	var newerSentID int64
	err = d.SQL().QueryRow(`
		SELECT s.id FROM sentences s
		JOIN sentence_words sw ON sw.sentence_id = s.id
		WHERE sw.word_id = ? AND s.text = '経済は重要だ。'`, wordID,
	).Scan(&newerSentID)
	if err != nil {
		t.Fatalf("newer sentence: %v", err)
	}
	if olderSentID == newerSentID {
		t.Fatal("expected two distinct sentences")
	}

	// Client showed older sentence (e.g. presentation before newer ingest, or sticky UI token).
	svc := review.New(d, schedule.DefaultParams())
	if err := svc.Grade(db.LearnerUserID, cardID, olderSentID, "good", updatedAt, t2); err != nil {
		t.Fatal(err)
	}

	var storedSent int64
	var grade string
	err = d.SQL().QueryRow(
		`SELECT sentence_id, grade FROM reviews WHERE card_id = ?`, cardID,
	).Scan(&storedSent, &grade)
	if err != nil {
		t.Fatal(err)
	}
	if grade != "good" {
		t.Fatalf("grade: %s", grade)
	}
	if storedSent != olderSentID {
		t.Fatalf("reviews.sentence_id: got %d want shown older %d (newer=%d)", storedSent, olderSentID, newerSentID)
	}
}

// Learning step 1 + Good graduates through review.Service (DB persist path of G3).
func TestGradeLearningStep1GoodGraduates(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	var cardID, wordID int64
	var updatedAt string
	if err := d.SQL().QueryRow(
		`SELECT id, word_id, updated_at FROM cards LIMIT 1`,
	).Scan(&cardID, &wordID, &updatedAt); err != nil {
		t.Fatal(err)
	}
	var sentenceID int64
	if err := d.SQL().QueryRow(
		`SELECT sentence_id FROM sentence_words WHERE word_id = ? LIMIT 1`, wordID,
	).Scan(&sentenceID); err != nil {
		t.Fatal(err)
	}

	_, err := d.SQL().Exec(
		`UPDATE cards SET phase = 'learning', learning_step = 1, interval_days = 0,
		 ease = 2.5, due_at = ?, reps = 0, lapses = 0, updated_at = ? WHERE id = ?`,
		now.Format(time.RFC3339), updatedAt, cardID,
	)
	if err != nil {
		t.Fatal(err)
	}

	svc := review.New(d, schedule.DefaultParams())
	if err := svc.Grade(db.LearnerUserID, cardID, sentenceID, "good", updatedAt, now); err != nil {
		t.Fatal(err)
	}

	var phase string
	var interval float64
	var reps, step int
	var dueAt string
	err = d.SQL().QueryRow(
		`SELECT phase, interval_days, reps, learning_step, due_at FROM cards WHERE id = ?`, cardID,
	).Scan(&phase, &interval, &reps, &step, &dueAt)
	if err != nil {
		t.Fatal(err)
	}
	if phase != "review" {
		t.Fatalf("phase: %s want review", phase)
	}
	if !almostEqual(interval, 1) {
		t.Fatalf("interval_days: %v want 1", interval)
	}
	if reps != 1 {
		t.Fatalf("reps: %d want 1", reps)
	}
	wantDue := now.Add(24 * time.Hour).Format(time.RFC3339)
	if dueAt != wantDue {
		t.Fatalf("due_at: got %q want %q", dueAt, wantDue)
	}
}

// Presentation spans reconstruct the full sentence (gaps + words).
func TestSpansCoverFullSentence(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sentence := "経済政策を発表した。"
	if _, err := d.IngestText(db.LearnerUserID, sentence, a, now); err != nil {
		t.Fatal(err)
	}

	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}

	var rebuilt strings.Builder
	for _, sp := range res.Item.Spans {
		rebuilt.WriteString(sp.Surface)
	}
	if rebuilt.String() != res.Item.Sentence {
		t.Fatalf("spans rebuild %q != sentence %q", rebuilt.String(), res.Item.Sentence)
	}
	// At least one plain gap (particles) expected between kanji words
	var hasWord, hasGap bool
	for _, sp := range res.Item.Spans {
		if sp.WordID != 0 {
			hasWord = true
			if sp.Reading == "" {
				t.Fatalf("word span missing reading: %+v", sp)
			}
		} else if sp.Surface != "" {
			hasGap = true
		}
	}
	if !hasWord {
		t.Fatal("expected word spans")
	}
	if !hasGap {
		t.Fatal("expected plain-text gap spans (particles/punctuation)")
	}
}


func TestGradeDoubleSubmitStale(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}
	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	tok := res.Item.UpdatedAt
	if err := svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", tok, now); err != nil {
		t.Fatal(err)
	}
	err = svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", tok, now)
	if !errors.Is(err, review.ErrStaleCard) {
		t.Fatalf("second grade: got %v want ErrStaleCard", err)
	}
	var n int
	_ = d.SQL().QueryRow(`SELECT COUNT(1) FROM reviews WHERE card_id = ?`, res.Item.CardID).Scan(&n)
	if n != 1 {
		t.Fatalf("reviews: %d", n)
	}
	var step int
	_ = d.SQL().QueryRow(`SELECT learning_step FROM cards WHERE id = ?`, res.Item.CardID).Scan(&step)
	if step != 0 {
		t.Fatalf("step advanced on double grade: %d", step)
	}
}

func TestNextSkipsCardWithoutSentence(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}
	// Orphan a card: delete its sentence_words so buildItem fails
	var badID int64
	if err := d.SQL().QueryRow(`SELECT id FROM cards ORDER BY id ASC LIMIT 1`).Scan(&badID); err != nil {
		t.Fatal(err)
	}
	var wordID int64
	_ = d.SQL().QueryRow(`SELECT word_id FROM cards WHERE id = ?`, badID).Scan(&wordID)
	_, _ = d.SQL().Exec(`DELETE FROM sentence_words WHERE word_id = ?`, wordID)
	// Force bad card due first
	_, _ = d.SQL().Exec(
		`UPDATE cards SET phase = 'learning', due_at = ? WHERE id = ?`,
		now.Add(-time.Hour).Format(time.RFC3339), badID,
	)

	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Empty {
		// Other cards may still be new and available
		t.Fatal("expected to skip bad card and return another (or empty only if no others)")
	}
	if res.Item.CardID == badID {
		t.Fatal("should not return unpresentable card")
	}
}

// Spec: NewPerDay advances on first grade, not presentation alone.
// Same new Card stays sticky until graded so re-open does not burn quota.
func TestNextStickyNewUntilGraded(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}

	p := schedule.DefaultParams()
	p.NewPerDay = 1
	svc := review.New(d, p)

	res1, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res1.Empty {
		t.Fatal("need first new card")
	}
	res2, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res2.Empty {
		t.Fatal("need sticky re-present")
	}
	if res2.Item.CardID != res1.Item.CardID {
		t.Fatalf("sticky: got card %d want %d", res2.Item.CardID, res1.Item.CardID)
	}
	var n int
	if err := d.SQL().QueryRow(`SELECT COUNT(1) FROM reviews`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("presentation alone must not insert reviews; got %d", n)
	}

	if err := svc.Grade(db.LearnerUserID, res1.Item.CardID, res1.Item.SentenceID, "good", res1.Item.UpdatedAt, now); err != nil {
		t.Fatal(err)
	}
	// Cap exhausted and learning card not yet due → empty
	res3, err := svc.Next(db.LearnerUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res3.Empty {
		t.Fatalf("after grade under NewPerDay=1 want empty, got card %d", res3.Item.CardID)
	}
}

func TestGradeMissingUpdatedAtIsStale(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}
	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	err = svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", "", now)
	if !errors.Is(err, review.ErrStaleCard) {
		t.Fatalf("empty updated_at: got %v want ErrStaleCard", err)
	}
	err = svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", "   ", now)
	if !errors.Is(err, review.ErrStaleCard) {
		t.Fatalf("blank updated_at: got %v want ErrStaleCard", err)
	}
}

func TestGradeNilService(t *testing.T) {
	var svc *review.Service
	err := svc.Grade(1, 1, 1, "good", "tok", time.Now().UTC())
	if !errors.Is(err, review.ErrNilService) {
		t.Fatalf("got %v want ErrNilService", err)
	}
	svc = review.New(nil, schedule.DefaultParams())
	err = svc.Grade(1, 1, 1, "good", "tok", time.Now().UTC())
	if !errors.Is(err, review.ErrNilService) {
		t.Fatalf("nil db: got %v want ErrNilService", err)
	}
}

func TestServiceParams(t *testing.T) {
	p := schedule.DefaultParams()
	p.NewPerDay = 7
	svc := review.New(nil, p)
	got := svc.Params()
	if got.NewPerDay != 7 {
		t.Fatalf("Params NewPerDay: %d", got.NewPerDay)
	}
}

// Grade tolerates SQLite-style due_at timestamps (space separator, no TZ).
func TestGradeAcceptsSQLiteStyleDueAt(t *testing.T) {
	d := openTestDB(t)
	seedUser(t, d)
	a := mustAnalyzer(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := d.IngestText(db.LearnerUserID, "経済政策を発表した。", a, now); err != nil {
		t.Fatal(err)
	}
	svc := review.New(d, schedule.DefaultParams())
	res, err := svc.Next(db.LearnerUserID, now)
	if err != nil || res.Empty {
		t.Fatal("need card")
	}
	// Some SQLite tooling stores datetime as "YYYY-MM-DD HH:MM:SS".
	_, err = d.SQL().Exec(
		`UPDATE cards SET due_at = '2026-07-17 12:00:00' WHERE id = ?`,
		res.Item.CardID,
	)
	if err != nil {
		t.Fatal(err)
	}
	// Re-read updated_at token (unchanged by due_at update).
	if err := svc.Grade(db.LearnerUserID, res.Item.CardID, res.Item.SentenceID, "good", res.Item.UpdatedAt, now); err != nil {
		t.Fatalf("grade with sqlite-style due_at: %v", err)
	}
}

func almostEqual(a, b float64) bool {
	const eps = 1e-9
	if a > b {
		return a-b < eps
	}
	return b-a < eps
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path, migrationsDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
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

func mustAnalyzer(t *testing.T) *analyze.Analyzer {
	t.Helper()
	a, err := analyze.New()
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}
