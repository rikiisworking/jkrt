// Package schedule implements pure SM-2 Card transitions (docs/sm2-spec.md).
// No I/O — DB and HTTP only persist or display results.
package schedule

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Phase is the Card learning phase.
type Phase string

const (
	PhaseNew         Phase = "new"
	PhaseLearning    Phase = "learning"
	PhaseReview      Phase = "review"
	PhaseRelearning  Phase = "relearning"
)

// Grade is a Review result wire value.
type Grade string

const (
	GradeAgain Grade = "again"
	GradeHard  Grade = "hard"
	GradeGood  Grade = "good"
	GradeEasy  Grade = "easy"
)

// ParseGrade accepts lowercase wire values again|hard|good|easy.
func ParseGrade(s string) (Grade, error) {
	g := Grade(strings.ToLower(strings.TrimSpace(s)))
	switch g {
	case GradeAgain, GradeHard, GradeGood, GradeEasy:
		return g, nil
	default:
		return "", fmt.Errorf("invalid grade %q", s)
	}
}

// Params holds all normative scheduler knobs (sm2-spec defaults).
type Params struct {
	LearningSteps          []time.Duration
	GraduatingInterval     float64 // days
	EasyInterval           float64 // days
	StartingEase           float64
	MinEase                float64
	EasyBonus              float64
	IntervalModifier       float64
	HardIntervalFactor     float64
	LapseNewIntervalFactor float64
	// NewPerDay is max Cards whose first review row falls on the UTC calendar day.
	// Cap advances on first grade (not presentation alone). v1 default 20.
	NewPerDay int
	// SessionLimit is the max grade rows per UTC calendar day (daily review cap,
	// not an ephemeral sitting). v1 default 40. Name kept for config/env continuity.
	SessionLimit int
	// ComfortableIntervalDays is the Unfamiliar highlight threshold for review phase.
	ComfortableIntervalDays float64
}

// DefaultParams returns sm2-spec defaults.
func DefaultParams() Params {
	return Params{
		LearningSteps:           []time.Duration{time.Minute, 10 * time.Minute},
		GraduatingInterval:      1,
		EasyInterval:            4,
		StartingEase:            2.5,
		MinEase:                 1.3,
		EasyBonus:               1.3,
		IntervalModifier:        1.0,
		HardIntervalFactor:      1.2,
		LapseNewIntervalFactor:  0.0,
		NewPerDay:               20,
		SessionLimit:            40,
		ComfortableIntervalDays: 21,
	}
}

// State is id-free Card scheduling state.
type State struct {
	Phase        Phase
	LearningStep int
	IntervalDays float64
	Ease         float64
	DueAt        time.Time
	Reps         int
	Lapses       int
}

// NewCard seeds fields for a newly extracted Card (phase=new, due_at=now).
func NewCard(p Params, now time.Time) State {
	return State{
		Phase:        PhaseNew,
		LearningStep: 0,
		IntervalDays: 0,
		Ease:         p.StartingEase,
		DueAt:        now.UTC(),
		Reps:         0,
		Lapses:       0,
	}
}

// Apply is the single pure SM-2 transition for one grade at now.
func Apply(p Params, s State, grade Grade, now time.Time) State {
	now = now.UTC()
	switch s.Phase {
	case PhaseNew:
		return applyNew(p, s, grade, now)
	case PhaseLearning, PhaseRelearning:
		return applyLearning(p, s, grade, now)
	case PhaseReview:
		return applyReview(p, s, grade, now)
	default:
		// Unknown phase: treat like review for safety (no panic); callers should not hit this.
		return applyReview(p, s, grade, now)
	}
}

func applyNew(p Params, s State, grade Grade, now time.Time) State {
	out := s
	switch grade {
	case GradeEasy:
		out.Phase = PhaseReview
		out.LearningStep = 0
		out.IntervalDays = p.EasyInterval
		out.DueAt = now.Add(daysToDuration(p.EasyInterval))
		// reps unchanged (sm2-spec new→Easy table does not increment)
		return out
	default:
		// Again / Hard / Good → enter learning at step 0
		out.Phase = PhaseLearning
		out.LearningStep = 0
		out.IntervalDays = 0
		out.DueAt = now.Add(stepDuration(p, 0))
		return out
	}
}

func applyLearning(p Params, s State, grade Grade, now time.Time) State {
	out := s
	steps := p.LearningSteps
	i := s.LearningStep
	if i < 0 {
		i = 0
	}
	if len(steps) == 0 {
		// Degenerate config: graduate on Good/Easy
		steps = []time.Duration{time.Minute}
	}
	if i >= len(steps) {
		i = len(steps) - 1
	}

	switch grade {
	case GradeAgain:
		out.LearningStep = 0
		out.DueAt = now.Add(stepDuration(p, 0))
		// stay learning/relearning; ease unchanged
		return out
	case GradeHard:
		out.LearningStep = i
		out.DueAt = now.Add(stepDuration(p, i))
		return out
	case GradeGood:
		if i+1 < len(steps) {
			out.LearningStep = i + 1
			out.DueAt = now.Add(stepDuration(p, i+1))
			return out
		}
		// Graduate
		return graduate(p, out, now, p.GraduatingInterval)
	case GradeEasy:
		return graduate(p, out, now, p.EasyInterval)
	default:
		return out
	}
}

func graduate(p Params, s State, now time.Time, intervalDays float64) State {
	out := s
	out.Phase = PhaseReview
	out.LearningStep = 0
	// Lapse relearn Good with factor 0: full interval reset (max(1, GraduatingInterval)).
	// Easy still uses EasyInterval passed by the learning step table.
	if s.Phase == PhaseRelearning && p.LapseNewIntervalFactor == 0 && intervalDays == p.GraduatingInterval {
		intervalDays = math.Max(1, p.GraduatingInterval)
	}
	out.IntervalDays = roundInterval(intervalDays)
	out.DueAt = now.Add(daysToDuration(out.IntervalDays))
	out.Reps = s.Reps + 1
	return out
}

func applyReview(p Params, s State, grade Grade, now time.Time) State {
	out := s
	I := s.IntervalDays
	E := s.Ease

	switch grade {
	case GradeAgain:
		out.Ease = math.Max(p.MinEase, E-0.2)
		// keep old I stored for reference
		out.Phase = PhaseRelearning
		out.LearningStep = 0
		out.Lapses = s.Lapses + 1
		out.DueAt = now.Add(stepDuration(p, 0))
		return out
	case GradeHard:
		out.Ease = math.Max(p.MinEase, E-0.15)
		newI := math.Max(1, I*p.HardIntervalFactor) * p.IntervalModifier
		out.IntervalDays = roundInterval(newI)
		out.DueAt = now.Add(daysToDuration(out.IntervalDays))
		out.Reps = s.Reps + 1
		return out
	case GradeGood:
		newI := math.Max(1, I*E*p.IntervalModifier)
		out.IntervalDays = roundInterval(newI)
		out.DueAt = now.Add(daysToDuration(out.IntervalDays))
		out.Reps = s.Reps + 1
		return out
	case GradeEasy:
		out.Ease = E + 0.15
		newI := math.Max(1, I*E*p.EasyBonus*p.IntervalModifier)
		out.IntervalDays = roundInterval(newI)
		out.DueAt = now.Add(daysToDuration(out.IntervalDays))
		out.Reps = s.Reps + 1
		return out
	default:
		return out
	}
}

// IsUnfamiliar reports whether a Card should be highlighted (locked predicate).
//
//	phase IN (new, learning, relearning)
//	OR due_at <= now
//	OR (phase == review AND interval_days < ComfortableIntervalDays)
func IsUnfamiliar(p Params, s State, now time.Time) bool {
	switch s.Phase {
	case PhaseNew, PhaseLearning, PhaseRelearning:
		return true
	}
	if !s.DueAt.After(now.UTC()) {
		return true
	}
	threshold := p.ComfortableIntervalDays
	if threshold == 0 {
		threshold = 21
	}
	if s.Phase == PhaseReview && s.IntervalDays < threshold {
		return true
	}
	return false
}

func stepDuration(p Params, i int) time.Duration {
	if len(p.LearningSteps) == 0 {
		return time.Minute
	}
	if i < 0 {
		i = 0
	}
	if i >= len(p.LearningSteps) {
		i = len(p.LearningSteps) - 1
	}
	return p.LearningSteps[i]
}

// daysToDuration converts interval days to a duration (interval_days * 24h).
func daysToDuration(days float64) time.Duration {
	// Use float hours to avoid truncating fractional days (e.g. 2.5d).
	return time.Duration(days * 24 * float64(time.Hour))
}

// roundInterval stores interval_days with at least 2 decimal places of precision.
func roundInterval(days float64) float64 {
	return math.Round(days*100) / 100
}
