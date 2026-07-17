package schedule_test

import (
	"math"
	"testing"
	"time"

	"github.com/rikiisworking/jkrt/internal/schedule"
)

func t0() time.Time {
	return time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
}

func params() schedule.Params {
	return schedule.DefaultParams()
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// G1 — New + Good enters learning
func TestG1_NewGoodEntersLearning(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.NewCard(p, T0)
	got := schedule.Apply(p, s, schedule.GradeGood, T0)

	if got.Phase != schedule.PhaseLearning {
		t.Fatalf("phase: got %s want learning", got.Phase)
	}
	if got.LearningStep != 0 {
		t.Fatalf("step: got %d want 0", got.LearningStep)
	}
	wantDue := T0.Add(time.Minute)
	if !got.DueAt.Equal(wantDue) {
		t.Fatalf("due: got %v want %v", got.DueAt, wantDue)
	}
}

// G2 — Learning Good advances step
func TestG2_LearningGoodAdvancesStep(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseLearning,
		LearningStep: 0,
		IntervalDays: 0,
		Ease:         2.5,
		DueAt:        T0,
	}
	got := schedule.Apply(p, s, schedule.GradeGood, T0)

	if got.LearningStep != 1 {
		t.Fatalf("step: got %d want 1", got.LearningStep)
	}
	wantDue := T0.Add(10 * time.Minute)
	if !got.DueAt.Equal(wantDue) {
		t.Fatalf("due: got %v want %v", got.DueAt, wantDue)
	}
	if got.Phase != schedule.PhaseLearning {
		t.Fatalf("phase: got %s want learning", got.Phase)
	}
}

// G3 — Learning Good graduates
func TestG3_LearningGoodGraduates(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseLearning,
		LearningStep: 1,
		IntervalDays: 0,
		Ease:         2.5,
		DueAt:        T0,
		Reps:         0,
	}
	got := schedule.Apply(p, s, schedule.GradeGood, T0)

	if got.Phase != schedule.PhaseReview {
		t.Fatalf("phase: got %s want review", got.Phase)
	}
	if !almostEqual(got.IntervalDays, 1) {
		t.Fatalf("interval: got %v want 1", got.IntervalDays)
	}
	wantDue := T0.Add(24 * time.Hour)
	if !got.DueAt.Equal(wantDue) {
		t.Fatalf("due: got %v want %v", got.DueAt, wantDue)
	}
	if got.Reps != 1 {
		t.Fatalf("reps: got %d want 1", got.Reps)
	}
}

// G4 — Learning Easy graduates to 4d
func TestG4_LearningEasyGraduates(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseLearning,
		LearningStep: 0,
		IntervalDays: 0,
		Ease:         2.5,
		DueAt:        T0,
	}
	got := schedule.Apply(p, s, schedule.GradeEasy, T0)

	if got.Phase != schedule.PhaseReview {
		t.Fatalf("phase: got %s want review", got.Phase)
	}
	if !almostEqual(got.IntervalDays, 4) {
		t.Fatalf("interval: got %v want 4", got.IntervalDays)
	}
	wantDue := T0.Add(4 * 24 * time.Hour)
	if !got.DueAt.Equal(wantDue) {
		t.Fatalf("due: got %v want %v", got.DueAt, wantDue)
	}
}

// G5 — Review Good multiplies ease
func TestG5_ReviewGoodMultiplies(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseReview,
		LearningStep: 0,
		IntervalDays: 1,
		Ease:         2.5,
		DueAt:        T0,
		Reps:         1,
	}
	got := schedule.Apply(p, s, schedule.GradeGood, T0)

	if !almostEqual(got.IntervalDays, 2.5) {
		t.Fatalf("interval: got %v want 2.5", got.IntervalDays)
	}
	if !almostEqual(got.Ease, 2.5) {
		t.Fatalf("ease: got %v want 2.5", got.Ease)
	}
	wantDue := T0.Add(time.Duration(2.5 * 24 * float64(time.Hour)))
	if !got.DueAt.Equal(wantDue) {
		t.Fatalf("due: got %v want %v", got.DueAt, wantDue)
	}
}

// G6 — Review Again lapses
func TestG6_ReviewAgainLapses(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseReview,
		LearningStep: 0,
		IntervalDays: 10,
		Ease:         2.5,
		DueAt:        T0,
		Lapses:       0,
	}
	got := schedule.Apply(p, s, schedule.GradeAgain, T0)

	if got.Phase != schedule.PhaseRelearning {
		t.Fatalf("phase: got %s want relearning", got.Phase)
	}
	if got.LearningStep != 0 {
		t.Fatalf("step: got %d want 0", got.LearningStep)
	}
	if got.Lapses != 1 {
		t.Fatalf("lapses: got %d want 1", got.Lapses)
	}
	if !almostEqual(got.Ease, 2.3) {
		t.Fatalf("ease: got %v want 2.3", got.Ease)
	}
	wantDue := T0.Add(time.Minute)
	if !got.DueAt.Equal(wantDue) {
		t.Fatalf("due: got %v want %v", got.DueAt, wantDue)
	}
	// interval kept for reference
	if !almostEqual(got.IntervalDays, 10) {
		t.Fatalf("interval kept: got %v want 10", got.IntervalDays)
	}
}

// G7 — Review Hard
func TestG7_ReviewHard(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseReview,
		IntervalDays: 10,
		Ease:         2.5,
		DueAt:        T0,
	}
	got := schedule.Apply(p, s, schedule.GradeHard, T0)

	if got.Phase != schedule.PhaseReview {
		t.Fatalf("phase: got %s want review", got.Phase)
	}
	if !almostEqual(got.IntervalDays, 12) {
		t.Fatalf("interval: got %v want 12", got.IntervalDays)
	}
	if !almostEqual(got.Ease, 2.35) {
		t.Fatalf("ease: got %v want 2.35", got.Ease)
	}
	wantDue := T0.Add(12 * 24 * time.Hour)
	if !got.DueAt.Equal(wantDue) {
		t.Fatalf("due: got %v want %v", got.DueAt, wantDue)
	}
}

// G8 — Review Easy
func TestG8_ReviewEasy(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseReview,
		IntervalDays: 10,
		Ease:         2.5,
		DueAt:        T0,
	}
	got := schedule.Apply(p, s, schedule.GradeEasy, T0)

	if !almostEqual(got.Ease, 2.65) {
		t.Fatalf("ease: got %v want 2.65", got.Ease)
	}
	// 10 * 2.5 * 1.3 = 32.5
	if !almostEqual(got.IntervalDays, 32.5) {
		t.Fatalf("interval: got %v want 32.5", got.IntervalDays)
	}
	wantDue := T0.Add(time.Duration(32.5 * 24 * float64(time.Hour)))
	if !got.DueAt.Equal(wantDue) {
		t.Fatalf("due: got %v want %v", got.DueAt, wantDue)
	}
}

// G9 — Min ease floor
func TestG9_MinEaseFloor(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseReview,
		IntervalDays: 10,
		Ease:         1.35,
		DueAt:        T0,
	}
	got := schedule.Apply(p, s, schedule.GradeAgain, T0)

	if !almostEqual(got.Ease, 1.3) {
		t.Fatalf("ease: got %v want 1.3 (MinEase floor)", got.Ease)
	}
}

func TestNewCardDefaults(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.NewCard(p, T0)
	if s.Phase != schedule.PhaseNew || s.LearningStep != 0 || s.IntervalDays != 0 {
		t.Fatalf("new card seed: %+v", s)
	}
	if !almostEqual(s.Ease, 2.5) || s.Reps != 0 || s.Lapses != 0 {
		t.Fatalf("new card ease/reps: %+v", s)
	}
	if !s.DueAt.Equal(T0) {
		t.Fatalf("due_at: got %v want %v", s.DueAt, T0)
	}
}

func TestIsUnfamiliar(t *testing.T) {
	p := params()
	now := t0()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name  string
		state schedule.State
		want  bool
	}{
		{"new", schedule.State{Phase: schedule.PhaseNew, DueAt: future}, true},
		{"learning", schedule.State{Phase: schedule.PhaseLearning, DueAt: future}, true},
		{"relearning", schedule.State{Phase: schedule.PhaseRelearning, DueAt: future}, true},
		{"due review", schedule.State{Phase: schedule.PhaseReview, IntervalDays: 30, DueAt: past}, true},
		{"due exactly", schedule.State{Phase: schedule.PhaseReview, IntervalDays: 30, DueAt: now}, true},
		{"young review", schedule.State{Phase: schedule.PhaseReview, IntervalDays: 10, DueAt: future}, true},
		{"mature not due", schedule.State{Phase: schedule.PhaseReview, IntervalDays: 21, DueAt: future}, false},
		{"mature long", schedule.State{Phase: schedule.PhaseReview, IntervalDays: 100, DueAt: future}, false},
		{"interval under 21", schedule.State{Phase: schedule.PhaseReview, IntervalDays: 20.999, DueAt: future}, true},
		{"unknown phase due", schedule.State{Phase: "graduated", IntervalDays: 100, DueAt: past}, true},
		{"unknown phase not due", schedule.State{Phase: "graduated", IntervalDays: 100, DueAt: future}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := schedule.IsUnfamiliar(p, tc.state, now)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestParseGrade(t *testing.T) {
	for _, s := range []string{"again", "Hard", " GOOD ", "easy"} {
		if _, err := schedule.ParseGrade(s); err != nil {
			t.Fatalf("ParseGrade(%q): %v", s, err)
		}
	}
	if _, err := schedule.ParseGrade("maybe"); err == nil {
		t.Fatal("expected error for invalid grade")
	}
}

func TestNewAgainHardEnterLearning(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.NewCard(p, T0)
	for _, g := range []schedule.Grade{schedule.GradeAgain, schedule.GradeHard} {
		got := schedule.Apply(p, s, g, T0)
		if got.Phase != schedule.PhaseLearning || got.LearningStep != 0 {
			t.Fatalf("%s: %+v", g, got)
		}
		if !got.DueAt.Equal(T0.Add(time.Minute)) {
			t.Fatalf("%s due: %v", g, got.DueAt)
		}
	}
}

func TestNewEasyGraduates(t *testing.T) {
	p := params()
	T0 := t0()
	got := schedule.Apply(p, schedule.NewCard(p, T0), schedule.GradeEasy, T0)
	if got.Phase != schedule.PhaseReview || !almostEqual(got.IntervalDays, 4) {
		t.Fatalf("new easy: %+v", got)
	}
	// Spec table for new→Easy does not increment reps
	if got.Reps != 0 {
		t.Fatalf("new easy reps: got %d want 0", got.Reps)
	}
}

// Learning Again resets to step 0 and reschedules steps[0].
func TestLearningAgainResetsStep(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseLearning,
		LearningStep: 1,
		Ease:         2.5,
		DueAt:        T0,
	}
	got := schedule.Apply(p, s, schedule.GradeAgain, T0)
	if got.Phase != schedule.PhaseLearning {
		t.Fatalf("phase: %s", got.Phase)
	}
	if got.LearningStep != 0 {
		t.Fatalf("step: got %d want 0", got.LearningStep)
	}
	if !got.DueAt.Equal(T0.Add(time.Minute)) {
		t.Fatalf("due: %v", got.DueAt)
	}
	if !almostEqual(got.Ease, 2.5) {
		t.Fatalf("ease should be unchanged: %v", got.Ease)
	}
}

// Learning Hard repeats the current step.
func TestLearningHardRepeatsStep(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseLearning,
		LearningStep: 1,
		Ease:         2.5,
		DueAt:        T0,
	}
	got := schedule.Apply(p, s, schedule.GradeHard, T0)
	if got.LearningStep != 1 {
		t.Fatalf("step: got %d want 1", got.LearningStep)
	}
	if !got.DueAt.Equal(T0.Add(10 * time.Minute)) {
		t.Fatalf("due: %v", got.DueAt)
	}
}

// Relearning Good from last step graduates with GraduatingInterval (lapse factor 0 → 1d).
func TestRelearningGoodGraduatesOneDay(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseRelearning,
		LearningStep: 1,
		IntervalDays: 10, // prior review interval kept for reference
		Ease:         2.3,
		DueAt:        T0,
		Reps:         3,
		Lapses:       1,
	}
	got := schedule.Apply(p, s, schedule.GradeGood, T0)
	if got.Phase != schedule.PhaseReview {
		t.Fatalf("phase: %s", got.Phase)
	}
	if !almostEqual(got.IntervalDays, 1) {
		t.Fatalf("interval: got %v want 1 (lapse reset)", got.IntervalDays)
	}
	if !got.DueAt.Equal(T0.Add(24 * time.Hour)) {
		t.Fatalf("due: %v", got.DueAt)
	}
	if got.Reps != 4 {
		t.Fatalf("reps: got %d want 4", got.Reps)
	}
	if !almostEqual(got.Ease, 2.3) {
		t.Fatalf("ease unchanged on relearn graduate: %v", got.Ease)
	}
}

// Relearning Easy graduates with EasyInterval.
func TestRelearningEasyGraduatesFourDays(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseRelearning,
		LearningStep: 0,
		Ease:         2.3,
		DueAt:        T0,
		Reps:         2,
	}
	got := schedule.Apply(p, s, schedule.GradeEasy, T0)
	if got.Phase != schedule.PhaseReview {
		t.Fatalf("phase: %s", got.Phase)
	}
	if !almostEqual(got.IntervalDays, 4) {
		t.Fatalf("interval: got %v want 4", got.IntervalDays)
	}
	if got.Reps != 3 {
		t.Fatalf("reps: %d", got.Reps)
	}
}

// Review Hard respects MinEase floor (same idea as G9 for Again).
func TestReviewHardMinEaseFloor(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        schedule.PhaseReview,
		IntervalDays: 10,
		Ease:         1.35,
		DueAt:        T0,
	}
	got := schedule.Apply(p, s, schedule.GradeHard, T0)
	// 1.35 - 0.15 = 1.20 → floor 1.3
	if !almostEqual(got.Ease, 1.3) {
		t.Fatalf("ease: got %v want 1.3", got.Ease)
	}
}

// Small intervals floor at 1 day after Good/Hard multiply.
func TestReviewIntervalMinOneDay(t *testing.T) {
	p := params()
	T0 := t0()
	// Hard: max(1, 0.5 * 1.2) = max(1, 0.6) = 1
	s := schedule.State{
		Phase:        schedule.PhaseReview,
		IntervalDays: 0.5,
		Ease:         2.5,
		DueAt:        T0,
	}
	got := schedule.Apply(p, s, schedule.GradeHard, T0)
	if !almostEqual(got.IntervalDays, 1) {
		t.Fatalf("hard interval floor: got %v want 1", got.IntervalDays)
	}
	// Good: max(1, 0.3 * 2.5) = max(1, 0.75) = 1
	s.IntervalDays = 0.3
	got = schedule.Apply(p, s, schedule.GradeGood, T0)
	if !almostEqual(got.IntervalDays, 1) {
		t.Fatalf("good interval floor: got %v want 1", got.IntervalDays)
	}
}

func TestDefaultParamsMatchSpec(t *testing.T) {
	p := schedule.DefaultParams()
	if len(p.LearningSteps) != 2 || p.LearningSteps[0] != time.Minute || p.LearningSteps[1] != 10*time.Minute {
		t.Fatalf("LearningSteps: %v", p.LearningSteps)
	}
	if p.GraduatingInterval != 1 || p.EasyInterval != 4 || p.StartingEase != 2.5 {
		t.Fatalf("intervals/ease: %+v", p)
	}
	if p.MinEase != 1.3 || p.EasyBonus != 1.3 || p.HardIntervalFactor != 1.2 {
		t.Fatalf("factors: %+v", p)
	}
	if p.NewPerDay != 20 || p.SessionLimit != 40 || p.ComfortableIntervalDays != 21 {
		t.Fatalf("caps: %+v", p)
	}
}

func TestNewCardNormalizesToUTC(t *testing.T) {
	p := params()
	// Fixed zone +9
	loc := time.FixedZone("JST", 9*3600)
	now := time.Date(2026, 7, 17, 21, 0, 0, 0, loc)
	s := schedule.NewCard(p, now)
	if s.DueAt.Location() != time.UTC {
		t.Fatalf("DueAt location: %v", s.DueAt.Location())
	}
	if !s.DueAt.Equal(now.UTC()) {
		t.Fatalf("DueAt: got %v want %v", s.DueAt, now.UTC())
	}
}

// Degenerate LearningSteps and out-of-range step indices stay safe (no panic).
func TestLearningEmptyStepsAndStepClamp(t *testing.T) {
	T0 := t0()
	p := params()
	p.LearningSteps = nil

	s := schedule.State{Phase: schedule.PhaseLearning, LearningStep: 0, Ease: 2.5, DueAt: T0}
	got := schedule.Apply(p, s, schedule.GradeHard, T0)
	if !got.DueAt.Equal(T0.Add(time.Minute)) {
		t.Fatalf("empty steps hard due: %v", got.DueAt)
	}

	// Negative step clamps to 0; oversized step clamps to last.
	p = params()
	s.LearningStep = -1
	got = schedule.Apply(p, s, schedule.GradeHard, T0)
	if got.LearningStep != 0 {
		t.Fatalf("negative step clamp: got %d", got.LearningStep)
	}
	if !got.DueAt.Equal(T0.Add(time.Minute)) {
		t.Fatalf("negative step due: %v", got.DueAt)
	}
	s.LearningStep = 99
	got = schedule.Apply(p, s, schedule.GradeHard, T0)
	if got.LearningStep != 1 {
		t.Fatalf("oversized step clamp: got %d want 1", got.LearningStep)
	}
	if !got.DueAt.Equal(T0.Add(10 * time.Minute)) {
		t.Fatalf("oversized step due: %v", got.DueAt)
	}
}

// Unknown phase falls through to review transitions (no panic).
func TestApplyUnknownPhaseUsesReview(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase:        "weird",
		IntervalDays: 10,
		Ease:         2.5,
		DueAt:        T0,
		Reps:         2,
	}
	got := schedule.Apply(p, s, schedule.GradeGood, T0)
	// review Good: 10 * 2.5 = 25
	if !almostEqual(got.IntervalDays, 25) {
		t.Fatalf("interval: got %v want 25", got.IntervalDays)
	}
	if got.Reps != 3 {
		t.Fatalf("reps: %d", got.Reps)
	}
}

// ComfortableIntervalDays 0 falls back to 21 for unfamiliar highlight.
func TestIsUnfamiliarZeroComfortableThreshold(t *testing.T) {
	p := params()
	p.ComfortableIntervalDays = 0
	now := t0()
	future := now.Add(time.Hour)
	// interval 20.9 < default 21 → unfamiliar
	if !schedule.IsUnfamiliar(p, schedule.State{
		Phase: schedule.PhaseReview, IntervalDays: 20.9, DueAt: future,
	}, now) {
		t.Fatal("want unfamiliar under fallback threshold")
	}
	if schedule.IsUnfamiliar(p, schedule.State{
		Phase: schedule.PhaseReview, IntervalDays: 21, DueAt: future,
	}, now) {
		t.Fatal("want familiar at interval 21 with fallback threshold")
	}
}

// Invalid grade wire values already fail ParseGrade; Apply leaves state alone.
func TestApplyUnknownGradeNoOp(t *testing.T) {
	p := params()
	T0 := t0()
	s := schedule.State{
		Phase: schedule.PhaseLearning, LearningStep: 1, Ease: 2.5, DueAt: T0,
	}
	got := schedule.Apply(p, s, schedule.Grade("maybe"), T0)
	if got != s {
		t.Fatalf("learning unknown grade should no-op: got %+v want %+v", got, s)
	}
	s = schedule.State{
		Phase: schedule.PhaseReview, IntervalDays: 5, Ease: 2.5, DueAt: T0, Reps: 1,
	}
	got = schedule.Apply(p, s, schedule.Grade("maybe"), T0)
	if got != s {
		t.Fatalf("review unknown grade should no-op: got %+v want %+v", got, s)
	}
}
