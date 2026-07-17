// Package export builds Learner data dumps (JSON / CSV) for Phase 6 backup.
package export

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/review"
	"github.com/rikiisworking/jkrt/internal/snapshot"
)

// Format is the wire export format.
type Format string

const (
	FormatJSON Format = "json"
	FormatCSV  Format = "csv"
)

// ParseFormat accepts json|csv (default json).
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case "", FormatJSON:
		return FormatJSON, nil
	case FormatCSV:
		return FormatCSV, nil
	default:
		return "", fmt.Errorf("format must be json or csv, got %q", s)
	}
}

// CardRow is one Card + Word identity for export.
type CardRow struct {
	WordID       int64   `json:"word_id"`
	Lemma        string  `json:"lemma"`
	Reading      string  `json:"reading"`
	Phase        string  `json:"phase"`
	LearningStep int     `json:"learning_step"`
	IntervalDays float64 `json:"interval_days"`
	Ease         float64 `json:"ease"`
	DueAt        string  `json:"due_at"`
	Reps         int     `json:"reps"`
	Lapses       int     `json:"lapses"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// ReviewRow is one persisted Review (grade history).
type ReviewRow struct {
	CardID     int64  `json:"card_id"`
	SentenceID int64  `json:"sentence_id"`
	Grade      string `json:"grade"`
	ReviewedAt string `json:"reviewed_at"`
	Lemma      string `json:"lemma,omitempty"`
	Reading    string `json:"reading,omitempty"`
}

// Snapshot is the JSON export body.
type Snapshot struct {
	ExportedAt string         `json:"exported_at"`
	UserID     int64          `json:"user_id"`
	Queue      review.Stats   `json:"queue"`
	Library    db.LibraryCounts `json:"library"`
	Cards      []CardRow      `json:"cards"`
	Reviews    []ReviewRow    `json:"reviews"`
	// Truncated is true when card or review caps were hit.
	Truncated bool `json:"truncated,omitempty"`
}

// Service reads export data from SQLite.
type Service struct {
	sql *sql.DB
}

// New builds an export service. d may be nil (methods return errors).
func New(d *db.DB) *Service {
	var sqlDB *sql.DB
	if d != nil {
		sqlDB = d.SQL()
	}
	return &Service{sql: sqlDB}
}

// BuildSnapshot loads cards + reviews for the Learner (capped), using a preloaded
// queue+library View from snapshot.Load (single composition seam).
func (s *Service) BuildSnapshot(userID int64, view snapshot.View, now time.Time) (Snapshot, error) {
	if s == nil || s.sql == nil {
		return Snapshot{}, fmt.Errorf("export service not configured")
	}
	if now.IsZero() {
		now = view.AsOf
	}
	now = now.UTC()
	cards, cTrunc, err := s.loadCards(userID, db.MaxExportCards)
	if err != nil {
		return Snapshot{}, err
	}
	reviews, rTrunc, err := s.loadReviews(userID, db.MaxExportReviews)
	if err != nil {
		return Snapshot{}, err
	}
	if cards == nil {
		cards = []CardRow{}
	}
	if reviews == nil {
		reviews = []ReviewRow{}
	}
	return Snapshot{
		ExportedAt: now.Format(time.RFC3339),
		UserID:     userID,
		Queue:      view.Queue,
		Library:    view.Library,
		Cards:      cards,
		Reviews:    reviews,
		Truncated:  cTrunc || rTrunc,
	}, nil
}

func (s *Service) loadCards(userID int64, limit int) ([]CardRow, bool, error) {
	rows, err := s.sql.Query(
		`SELECT w.id, w.lemma, w.reading, c.phase, c.learning_step, c.interval_days, c.ease,
		        c.due_at, c.reps, c.lapses, c.created_at, c.updated_at
		 FROM cards c
		 JOIN words w ON w.id = c.word_id
		 WHERE c.user_id = ?
		 ORDER BY c.id ASC
		 LIMIT ?`,
		userID, limit+1,
	)
	if err != nil {
		return nil, false, fmt.Errorf("export cards: %w", err)
	}
	defer rows.Close()

	var out []CardRow
	for rows.Next() {
		var r CardRow
		if err := rows.Scan(
			&r.WordID, &r.Lemma, &r.Reading, &r.Phase, &r.LearningStep, &r.IntervalDays, &r.Ease,
			&r.DueAt, &r.Reps, &r.Lapses, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, false, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	trunc := false
	if len(out) > limit {
		out = out[:limit]
		trunc = true
	}
	return out, trunc, nil
}

func (s *Service) loadReviews(userID int64, limit int) ([]ReviewRow, bool, error) {
	rows, err := s.sql.Query(
		`SELECT r.card_id, r.sentence_id, r.grade, r.reviewed_at, w.lemma, w.reading
		 FROM reviews r
		 JOIN cards c ON c.id = r.card_id
		 JOIN words w ON w.id = c.word_id
		 WHERE r.user_id = ?
		 ORDER BY r.id ASC
		 LIMIT ?`,
		userID, limit+1,
	)
	if err != nil {
		return nil, false, fmt.Errorf("export reviews: %w", err)
	}
	defer rows.Close()

	var out []ReviewRow
	for rows.Next() {
		var r ReviewRow
		if err := rows.Scan(&r.CardID, &r.SentenceID, &r.Grade, &r.ReviewedAt, &r.Lemma, &r.Reading); err != nil {
			return nil, false, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	trunc := false
	if len(out) > limit {
		out = out[:limit]
		trunc = true
	}
	return out, trunc, nil
}

// WriteJSON encodes the snapshot as indented JSON.
func WriteJSON(w io.Writer, snap Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snap); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}

// WriteCardsCSV writes Card rows as CSV (UTF-8, header row).
func WriteCardsCSV(w io.Writer, cards []CardRow) error {
	cw := csv.NewWriter(w)
	header := []string{
		"lemma", "reading", "phase", "learning_step", "interval_days", "ease",
		"due_at", "reps", "lapses", "created_at", "updated_at",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range cards {
		row := []string{
			r.Lemma,
			r.Reading,
			r.Phase,
			fmt.Sprintf("%d", r.LearningStep),
			fmt.Sprintf("%g", r.IntervalDays),
			fmt.Sprintf("%g", r.Ease),
			r.DueAt,
			fmt.Sprintf("%d", r.Reps),
			fmt.Sprintf("%d", r.Lapses),
			r.CreatedAt,
			r.UpdatedAt,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
