// Package review implements the deep Review module (ADR 0005): next + grade.
// Queue selection and persist live here; SM-2 math is pure internal/schedule.
package review

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/schedule"
)

// maxNextSkips limits how many unpresentable Cards Next will skip before giving up.
const maxNextSkips = 32

// Service owns Review SQL on a concrete SQLite handle.
type Service struct {
	sql    *sql.DB
	params schedule.Params
}

// New builds a Review service. params typically schedule.DefaultParams().
func New(d *db.DB, params schedule.Params) *Service {
	var sqlDB *sql.DB
	if d != nil {
		sqlDB = d.SQL()
	}
	return &Service{sql: sqlDB, params: params}
}

// Params returns the scheduler params used for queue caps and Apply.
func (s *Service) Params() schedule.Params {
	return s.params
}

// Span is one presentation segment of a Sentence (word or plain gap).
type Span struct {
	Surface    string
	CharStart  int
	CharEnd    int
	WordID     int64 // 0 for plain text gaps
	Lemma      string
	Reading    string
	Unfamiliar bool
	Focus      bool // true when this span is the Card under Review
}

// Item is the next presentation payload (no HTML).
type Item struct {
	CardID     int64
	WordID     int64
	Lemma      string
	Reading    string
	Phase      string
	// UpdatedAt is the Card's updated_at (RFC3339) for optimistic grade locking.
	UpdatedAt  string
	SentenceID int64
	Sentence   string
	Spans      []Span
}

// Result is the outcome of Next. Empty is a normal empty queue (not an error).
type Result struct {
	Empty bool
	Item  Item
}

// Sentinel errors for grade validation.
var (
	ErrNotFound          = errors.New("card not found")
	ErrBadGrade          = errors.New("invalid grade")
	ErrSentenceNotLinked = errors.New("sentence not linked to card word")
	ErrNilService        = errors.New("review service not configured")
	// ErrStaleCard means the Card was already graded (or updated) since presentation.
	// Callers should re-next rather than treating this as a hard failure.
	ErrStaleCard = errors.New("card state changed since presentation")
)

// Next returns the next Card for Review: due first, then new under caps.
// Empty queue → Result{Empty: true}, nil error.
//
// Caps (v1, UTC calendar day):
//   - SessionLimit: max reviews (grades) per UTC day (daily review cap).
//   - NewPerDay: max cards whose first review row falls on this UTC day
//     (cap advances on grade, not on presentation alone).
//
// Unpresentable Cards (no sentence context) are skipped so one bad row
// does not brick the whole queue.
func (s *Service) Next(userID int64, now time.Time) (Result, error) {
	if s == nil || s.sql == nil {
		return Result{}, ErrNilService
	}
	now = now.UTC()

	reviewsToday, err := s.countReviewsToday(userID, now)
	if err != nil {
		return Result{}, err
	}
	if reviewsToday >= s.params.SessionLimit {
		return Result{Empty: true}, nil
	}

	var skip []int64
	for attempt := 0; attempt < maxNextSkips; attempt++ {
		cardID, ok, err := s.pickDueCard(userID, now, skip)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			introduced, err := s.countNewIntroducedToday(userID, now)
			if err != nil {
				return Result{}, err
			}
			if introduced >= s.params.NewPerDay {
				return Result{Empty: true}, nil
			}
			cardID, ok, err = s.pickNewCard(userID, skip)
			if err != nil {
				return Result{}, err
			}
			if !ok {
				return Result{Empty: true}, nil
			}
		}

		item, err := s.buildItem(userID, cardID, now)
		if err != nil {
			log.Printf("review: skip unpresentable card %d: %v", cardID, err)
			skip = append(skip, cardID)
			continue
		}
		return Result{Empty: false, Item: item}, nil
	}
	return Result{Empty: true}, nil
}

// Grade applies a grade to a Card, persists schedule state, and inserts a reviews row.
// expectedUpdatedAt must match cards.updated_at from Next (optimistic lock); on mismatch
// returns ErrStaleCard so a double-submit does not advance the schedule twice.
// Does not return the next Card — caller calls Next again.
func (s *Service) Grade(userID, cardID, sentenceID int64, gradeStr, expectedUpdatedAt string, now time.Time) error {
	if s == nil || s.sql == nil {
		return ErrNilService
	}
	now = now.UTC()
	expectedUpdatedAt = strings.TrimSpace(expectedUpdatedAt)
	if expectedUpdatedAt == "" {
		return fmt.Errorf("%w: missing card_updated_at", ErrStaleCard)
	}

	grade, err := schedule.ParseGrade(gradeStr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadGrade, err)
	}

	tx, err := s.sql.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		wordID       int64
		phase        string
		learningStep int
		intervalDays float64
		ease         float64
		dueAtStr     string
		updatedAtStr string
		reps         int
		lapses       int
	)
	err = tx.QueryRow(
		`SELECT word_id, phase, learning_step, interval_days, ease, due_at, updated_at, reps, lapses
		 FROM cards WHERE id = ? AND user_id = ?`,
		cardID, userID,
	).Scan(&wordID, &phase, &learningStep, &intervalDays, &ease, &dueAtStr, &updatedAtStr, &reps, &lapses)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("select card: %w", err)
	}

	if updatedAtStr != expectedUpdatedAt {
		return ErrStaleCard
	}

	// Sentence must be linked to this Card's Word.
	var link int
	err = tx.QueryRow(
		`SELECT 1 FROM sentence_words WHERE sentence_id = ? AND word_id = ? LIMIT 1`,
		sentenceID, wordID,
	).Scan(&link)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSentenceNotLinked
	}
	if err != nil {
		return fmt.Errorf("check sentence link: %w", err)
	}

	dueAt, err := parseTime(dueAtStr)
	if err != nil {
		return fmt.Errorf("parse due_at: %w", err)
	}

	st := schedule.State{
		Phase:        schedule.Phase(phase),
		LearningStep: learningStep,
		IntervalDays: intervalDays,
		Ease:         ease,
		DueAt:        dueAt.UTC(),
		Reps:         reps,
		Lapses:       lapses,
	}
	next := schedule.Apply(s.params, st, grade, now)
	// Always advance updated_at past the previous token so same-second grades
	// still invalidate the presentation lock (double-submit safety).
	prevUpdated, perr := parseTime(updatedAtStr)
	if perr != nil {
		prevUpdated = now
	}
	newUpdated := now
	if !newUpdated.After(prevUpdated) {
		newUpdated = prevUpdated.Add(time.Millisecond)
	}
	nowStr := newUpdated.UTC().Format(time.RFC3339Nano)
	reviewedAt := now.Format(time.RFC3339)

	// Optimistic lock: only update if updated_at still matches presentation token.
	res, err := tx.Exec(
		`UPDATE cards SET
			phase = ?, learning_step = ?, interval_days = ?, ease = ?,
			due_at = ?, reps = ?, lapses = ?, updated_at = ?
		 WHERE id = ? AND user_id = ? AND updated_at = ?`,
		string(next.Phase), next.LearningStep, next.IntervalDays, next.Ease,
		next.DueAt.UTC().Format(time.RFC3339), next.Reps, next.Lapses, nowStr,
		cardID, userID, expectedUpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update card: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrStaleCard
	}

	_, err = tx.Exec(
		`INSERT INTO reviews (user_id, card_id, sentence_id, grade, reviewed_at)
		 VALUES (?, ?, ?, ?, ?)`,
		userID, cardID, sentenceID, string(grade), reviewedAt,
	)
	if err != nil {
		return fmt.Errorf("insert review: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02 15:04:05", s)
}

func (s *Service) pickDueCard(userID int64, now time.Time, skip []int64) (id int64, ok bool, err error) {
	nowStr := now.Format(time.RFC3339)
	q, args := pickQuery(
		`SELECT id FROM cards
		 WHERE user_id = ? AND phase != 'new' AND due_at <= ?`,
		[]any{userID, nowStr},
		skip,
		` ORDER BY due_at ASC, id ASC LIMIT 1`,
	)
	err = s.sql.QueryRow(q, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("pick due: %w", err)
	}
	return id, true, nil
}

func (s *Service) pickNewCard(userID int64, skip []int64) (id int64, ok bool, err error) {
	q, args := pickQuery(
		`SELECT id FROM cards
		 WHERE user_id = ? AND phase = 'new'`,
		[]any{userID},
		skip,
		` ORDER BY id ASC LIMIT 1`,
	)
	err = s.sql.QueryRow(q, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("pick new: %w", err)
	}
	return id, true, nil
}

func pickQuery(base string, args []any, skip []int64, order string) (string, []any) {
	if len(skip) == 0 {
		return base + order, args
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString(` AND id NOT IN (`)
	for i, id := range skip {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('?')
		args = append(args, id)
	}
	b.WriteByte(')')
	b.WriteString(order)
	return b.String(), args
}

// countNewIntroducedToday counts cards whose first review is on the UTC calendar day of now.
// v1 NewPerDay advances when a card is graded for the first time (not on presentation alone).
func (s *Service) countNewIntroducedToday(userID int64, now time.Time) (int, error) {
	dayStart, dayEnd := utcDayBounds(now)
	var n int
	err := s.sql.QueryRow(
		`SELECT COUNT(1) FROM (
			SELECT card_id, MIN(reviewed_at) AS first_at
			FROM reviews
			WHERE user_id = ?
			GROUP BY card_id
		) WHERE first_at >= ? AND first_at < ?`,
		userID,
		dayStart.Format(time.RFC3339),
		dayEnd.Format(time.RFC3339),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count new introduced: %w", err)
	}
	return n, nil
}

// countReviewsToday counts grade rows on the UTC calendar day (daily SessionLimit).
func (s *Service) countReviewsToday(userID int64, now time.Time) (int, error) {
	dayStart, dayEnd := utcDayBounds(now)
	var n int
	err := s.sql.QueryRow(
		`SELECT COUNT(1) FROM reviews
		 WHERE user_id = ? AND reviewed_at >= ? AND reviewed_at < ?`,
		userID,
		dayStart.Format(time.RFC3339),
		dayEnd.Format(time.RFC3339),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count reviews today: %w", err)
	}
	return n, nil
}

func utcDayBounds(now time.Time) (start, end time.Time) {
	y, m, d := now.UTC().Date()
	start = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	end = start.Add(24 * time.Hour)
	return start, end
}

func (s *Service) buildItem(userID, cardID int64, now time.Time) (Item, error) {
	var (
		wordID       int64
		lemma        string
		reading      string
		phase        string
		updatedAtStr string
	)
	err := s.sql.QueryRow(
		`SELECT c.word_id, w.lemma, w.reading, c.phase, c.updated_at
		 FROM cards c
		 JOIN words w ON w.id = c.word_id
		 WHERE c.id = ? AND c.user_id = ?`,
		cardID, userID,
	).Scan(&wordID, &lemma, &reading, &phase, &updatedAtStr)
	if err != nil {
		return Item{}, fmt.Errorf("load card: %w", err)
	}

	// Newest occurrence: max(created_at), tie-break max(id)
	var sentenceID int64
	var sentenceText string
	err = s.sql.QueryRow(
		`SELECT s.id, s.text
		 FROM sentence_words sw
		 JOIN sentences s ON s.id = sw.sentence_id
		 WHERE sw.word_id = ?
		 ORDER BY sw.created_at DESC, sw.id DESC
		 LIMIT 1`,
		wordID,
	).Scan(&sentenceID, &sentenceText)
	if err != nil {
		return Item{}, fmt.Errorf("newest sentence: %w", err)
	}

	spans, err := s.loadSpans(userID, sentenceID, sentenceText, wordID, now)
	if err != nil {
		return Item{}, err
	}

	return Item{
		CardID:     cardID,
		WordID:     wordID,
		Lemma:      lemma,
		Reading:    reading,
		Phase:      phase,
		UpdatedAt:  updatedAtStr,
		SentenceID: sentenceID,
		Sentence:   sentenceText,
		Spans:      spans,
	}, nil
}

type wordOcc struct {
	wordID    int64
	surface   string
	charStart int
	charEnd   int
	lemma     string
	reading   string
	phase     string
	interval  float64
	dueAt     time.Time
}

func (s *Service) loadSpans(userID, sentenceID int64, sentenceText string, focusWordID int64, now time.Time) ([]Span, error) {
	rows, err := s.sql.Query(
		`SELECT sw.word_id, sw.surface, sw.char_start, sw.char_end, w.lemma, w.reading,
		        COALESCE(c.phase, 'new'), COALESCE(c.interval_days, 0), COALESCE(c.due_at, ?)
		 FROM sentence_words sw
		 JOIN words w ON w.id = sw.word_id
		 LEFT JOIN cards c ON c.word_id = sw.word_id AND c.user_id = ?
		 WHERE sw.sentence_id = ?
		 ORDER BY sw.char_start ASC, sw.id ASC`,
		now.UTC().Format(time.RFC3339), userID, sentenceID,
	)
	if err != nil {
		return nil, fmt.Errorf("load spans: %w", err)
	}
	defer rows.Close()

	var occs []wordOcc
	for rows.Next() {
		var o wordOcc
		var dueStr string
		if err := rows.Scan(&o.wordID, &o.surface, &o.charStart, &o.charEnd, &o.lemma, &o.reading,
			&o.phase, &o.interval, &dueStr); err != nil {
			return nil, fmt.Errorf("scan span: %w", err)
		}
		dueAt, perr := parseTime(dueStr)
		if perr != nil {
			dueAt = now
		}
		o.dueAt = dueAt.UTC()
		occs = append(occs, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return buildSpans(sentenceText, occs, focusWordID, s.params, now), nil
}

// buildSpans fills the sentence with word spans and plain-text gaps (rune offsets).
func buildSpans(sentence string, occs []wordOcc, focusWordID int64, p schedule.Params, now time.Time) []Span {
	runes := []rune(sentence)
	n := len(runes)
	var spans []Span
	cursor := 0

	for _, o := range occs {
		start := o.charStart
		end := o.charEnd
		if start < 0 {
			start = 0
		}
		if end > n {
			end = n
		}
		if start < cursor {
			if end <= cursor {
				continue
			}
			start = cursor
		}
		if start > cursor {
			spans = append(spans, Span{
				Surface:   string(runes[cursor:start]),
				CharStart: cursor,
				CharEnd:   start,
			})
		}
		st := schedule.State{
			Phase:        schedule.Phase(o.phase),
			IntervalDays: o.interval,
			DueAt:        o.dueAt,
		}
		unfamiliar := schedule.IsUnfamiliar(p, st, now)
		surface := o.surface
		if surface == "" && start < end {
			surface = string(runes[start:end])
		}
		spans = append(spans, Span{
			Surface:    surface,
			CharStart:  start,
			CharEnd:    end,
			WordID:     o.wordID,
			Lemma:      o.lemma,
			Reading:    o.reading,
			Unfamiliar: unfamiliar,
			Focus:      o.wordID == focusWordID,
		})
		cursor = end
	}
	if cursor < n {
		spans = append(spans, Span{
			Surface:   string(runes[cursor:n]),
			CharStart: cursor,
			CharEnd:   n,
		})
	}
	if len(spans) == 0 && sentence != "" {
		spans = []Span{{
			Surface:   sentence,
			CharStart: 0,
			CharEnd:   utf8.RuneCountInString(sentence),
		}}
	}
	return spans
}

// FocusReading returns the trimmed Reading for the focus Word.
func (i Item) FocusReading() string {
	return strings.TrimSpace(i.Reading)
}
