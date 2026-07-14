package review

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

type fakeReviewRunner struct {
	calls int
}

func (f *fakeReviewRunner) RunSkillReview(context.Context, string, generation.SessionContext) error {
	f.calls++
	return nil
}

type fakeActivityTracker struct {
	reviewAttempts int
	reviewSkips    int
	lastReviewAt   int64
	lastReviewOK   bool
}

func (f *fakeActivityTracker) RecordEvolutionActivity(kind string, ok bool, _ string) {
	switch kind {
	case activityReviewAttempt:
		f.reviewAttempts++
	case activityReviewSkipped:
		f.reviewSkips++
	case activityReview:
		f.lastReviewAt = time.Now().UnixMilli()
		f.lastReviewOK = ok
	}
}

func (f *fakeActivityTracker) LogGenesis(string, string, string, string, string) error { return nil }

func (f *fakeActivityTracker) LivenessSnapshot() struct {
	ReviewAttempts int
	ReviewSkips    int
	LastReviewAt   int64
	LastReviewOK   bool
} {
	return struct {
		ReviewAttempts int
		ReviewSkips    int
		LastReviewAt   int64
		LastReviewOK   bool
	}{f.reviewAttempts, f.reviewSkips, f.lastReviewAt, f.lastReviewOK}
}

// newTestNudger creates a Nudger with a throwaway Service that short-
// circuits LLM calls. The underlying Service is created with zero deps;
// callers that need the evaluator path can supply their own config.
func newTestNudger(t *testing.T, interval int) *Nudger {
	t.Helper()
	svc := generation.NewService(generation.Config{
		MinToolCalls:     5,
		MinTurns:         3,
		MaxSkillsPerDay:  10,
		CooldownPerSkill: 24 * time.Hour,
		OutputDir:        t.TempDir(),
	}, nil, nil, slog.Default())
	return NewNudger(svc, NudgerConfig{Interval: interval}, slog.Default())
}

func TestNudgerEnabledFalseWhenIntervalZero(t *testing.T) {
	n := newTestNudger(t, 0)
	if n.Enabled() {
		t.Error("expected disabled when interval=0")
	}
}

func TestNudger_Disabled_NilService(t *testing.T) {
	n := NewNudger(nil, NudgerConfig{Interval: 10}, slog.Default())
	if n.Enabled() {
		t.Error("expected disabled when service is nil")
	}
	// Should be a no-op, not panic.
	n.OnToolCalls(context.TODO(), "s", 5, generation.SessionContext{})
}

func TestNudgerCountReturnsPerSessionIncrementIndependently(t *testing.T) {
	n := newTestNudger(t, 10)
	for range 5 {
		n.OnToolCalls(context.TODO(), "session-a", 1, generation.SessionContext{})
	}
	if got := n.Count("session-a"); got != 5 {
		t.Errorf("expected count=5, got %d", got)
	}
	// Other sessions are independent.
	if got := n.Count("session-b"); got != 0 {
		t.Errorf("expected session-b count=0, got %d", got)
	}
}

func TestNudgerCounterReturnsToZeroAfterThresholdFire(t *testing.T) {
	n := newTestNudger(t, 10)
	// Supply a snapshot that EvaluateReview will reject so nothing spawns.
	sctx := generation.SessionContext{
		ToolActivities: []generation.ToolActivity{{Name: "read"}},
		Turns:          1, // too little observed work for review
	}
	n.OnToolCalls(context.TODO(), "s", 10, sctx)
	// Wait briefly for the background fire to clear inflight state.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		_, busy := n.inflight["s"]
		n.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := n.Count("s"); got != 0 {
		t.Errorf("expected counter reset to 0 after threshold, got %d", got)
	}
}

func TestNudgerResetClearsSessionCount(t *testing.T) {
	n := newTestNudger(t, 10)
	n.OnToolCalls(context.TODO(), "s", 7, generation.SessionContext{})
	if got := n.Count("s"); got != 7 {
		t.Fatalf("precondition: expected 7, got %d", got)
	}
	n.Reset("s")
	if got := n.Count("s"); got != 0 {
		t.Errorf("expected reset to 0, got %d", got)
	}
}

func TestNudger_Concurrent_NoRace(t *testing.T) {
	n := newTestNudger(t, 50)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				n.OnToolCalls(context.TODO(), "s", 1, generation.SessionContext{
					ToolActivities: []generation.ToolActivity{{Name: "read"}},
					Turns:          1,
				})
			}
		}()
	}
	wg.Wait()
	// Totally fine if we fired the threshold a few times — we just
	// care that -race is clean and state is sane (non-negative count).
	if got := n.Count("s"); got < 0 {
		t.Errorf("count went negative: %d", got)
	}
}

func TestNudger_RunOnce_RespectsEvaluateRejection(t *testing.T) {
	n := newTestNudger(t, 10)
	// MinTurns=3 so Turns=1 is rejected.
	sctx := generation.SessionContext{
		Turns: 1,
		ToolActivities: []generation.ToolActivity{
			{Name: "read"},
			{Name: "exec"},
			{Name: "write"},
			{Name: "grep"},
			{Name: "read"},
		},
	}
	persisted, err := n.runOnce("s", sctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if persisted {
		t.Errorf("expected not persisted when Evaluate rejects")
	}
}

func TestNudgerRunReviewOnceRunsReviewerWhenEvaluateReviewPasses(t *testing.T) {
	reviewer := &fakeReviewRunner{}
	tracker := &fakeActivityTracker{}
	n := newTestNudger(t, 10)
	n.reviewer = reviewer
	n.tracker = tracker

	sctx := generation.SessionContext{
		Turns: 3,
		ToolActivities: []generation.ToolActivity{
			{Name: "read"},
			{Name: "exec"},
			{Name: "write"},
			{Name: "web"},
			{Name: "skills"},
		},
	}
	ran, err := n.runReviewOnce("s", sctx)
	if err != nil {
		t.Fatalf("runReviewOnce: %v", err)
	}
	if !ran || reviewer.calls != 1 {
		t.Fatalf("expected reviewer to run once, ran=%v calls=%d", ran, reviewer.calls)
	}
	snap := tracker.LivenessSnapshot()
	if snap.ReviewAttempts != 1 || snap.ReviewSkips != 0 || snap.LastReviewAt == 0 || !snap.LastReviewOK {
		t.Fatalf("expected review attempt/run to be observable, got %+v", snap)
	}
}

func TestNudgerRunReviewOnceSkipsReviewerWhenEvaluateReviewRejects(t *testing.T) {
	reviewer := &fakeReviewRunner{}
	tracker := &fakeActivityTracker{}
	n := newTestNudger(t, 10)
	n.reviewer = reviewer
	n.tracker = tracker

	ran, err := n.runReviewOnce("s", generation.SessionContext{
		Turns:          1,
		ToolActivities: []generation.ToolActivity{{Name: "read"}},
	})
	if err != nil {
		t.Fatalf("runReviewOnce: %v", err)
	}
	if ran || reviewer.calls != 0 {
		t.Fatalf("expected reviewer to be skipped, ran=%v calls=%d", ran, reviewer.calls)
	}
	snap := tracker.LivenessSnapshot()
	if snap.ReviewAttempts != 1 || snap.ReviewSkips != 1 || snap.LastReviewAt != 0 {
		t.Fatalf("expected evaluate skip to be observable without review heartbeat, got %+v", snap)
	}
}

func TestNudger_FromEnv_DefaultWhenUnset(t *testing.T) {
	t.Setenv("DENEB_SKILL_NUDGE_INTERVAL", "")
	svc := generation.NewService(generation.Config{}, nil, nil, slog.Default())
	n := NewNudgerFromEnv(svc, slog.Default())
	if n.Interval() != DefaultNudgeInterval {
		t.Errorf("expected default interval %d, got %d", DefaultNudgeInterval, n.Interval())
	}
}

func TestNudgerFromEnvDisabledWhenIntervalEnvIsZero(t *testing.T) {
	t.Setenv("DENEB_SKILL_NUDGE_INTERVAL", "0")
	svc := generation.NewService(generation.Config{}, nil, nil, slog.Default())
	n := NewNudgerFromEnv(svc, slog.Default())
	if n.Enabled() {
		t.Errorf("expected disabled with env=0")
	}
}

func TestNudger_FromEnv_InvalidValueUsesDefault(t *testing.T) {
	t.Setenv("DENEB_SKILL_NUDGE_INTERVAL", "not-a-number")
	svc := generation.NewService(generation.Config{}, nil, nil, slog.Default())
	n := NewNudgerFromEnv(svc, slog.Default())
	if n.Interval() != DefaultNudgeInterval {
		t.Errorf("expected fallback to %d, got %d", DefaultNudgeInterval, n.Interval())
	}
}

func TestNudgerOnToolCallsSkipsIncrementWhenSessionInflight(t *testing.T) {
	n := newTestNudger(t, 5)
	// Manually flip inflight.
	n.mu.Lock()
	n.inflight["s"] = time.Now()
	n.mu.Unlock()
	n.OnToolCalls(context.TODO(), "s", 10, generation.SessionContext{
		Turns: 5, ToolActivities: []generation.ToolActivity{{Name: "x"}},
	})
	// Inflight path rejects so the count is never incremented.
	if got := n.Count("s"); got != 0 {
		t.Errorf("expected count to stay 0 while inflight, got %d", got)
	}
}

func TestNudgerIncrementDoublesThresholdWhenFired(t *testing.T) {
	n := newTestNudger(t, 5) // interval=5
	// First fire lands at the base threshold (5).
	if !n.increment("s", 5) {
		t.Fatal("expected first fire at threshold 5")
	}
	n.clearInflight("s") // simulate the background review completing
	// After 1 fire the threshold doubles to 10: 5 more calls must NOT fire.
	if n.increment("s", 5) {
		t.Fatal("expected no fire at 5 after backoff (threshold now 10)")
	}
	// Reaching cumulative 10 crosses the doubled threshold.
	if !n.increment("s", 5) {
		t.Fatal("expected fire at cumulative 10")
	}
	n.clearInflight("s")
	// After 2 fires the threshold is 20.
	if n.increment("s", 19) {
		t.Fatal("expected no fire at 19 (threshold now 20)")
	}
	if !n.increment("s", 1) {
		t.Fatal("expected fire at cumulative 20")
	}
}

func TestNudger_ResetClearsBackoff(t *testing.T) {
	n := newTestNudger(t, 5)
	if !n.increment("s", 5) { // fire once, raising fires to 1
		t.Fatal("expected first fire at 5")
	}
	n.clearInflight("s")
	n.Reset("s")
	// Reset clears the backoff, so the threshold returns to the base interval.
	if !n.increment("s", 5) {
		t.Fatal("expected fire at 5 after Reset cleared the backoff")
	}
}

func TestNudgerRunStaleReviewReturnsFiredOnlyForReviewWorthyUnblockedSessions(t *testing.T) {
	n := newTestNudger(t, 3)
	rec := &fakeReviewRunner{}
	n.reviewer = rec
	snap := generation.SessionContext{Turns: 2, ToolActivities: []generation.ToolActivity{{Name: "a"}, {Name: "b"}}}

	fired, err := n.RunStaleReview("client:main", snap)
	if err != nil || !fired {
		t.Fatalf("stale review = (%v, %v), want fired", fired, err)
	}
	if rec.calls != 1 {
		t.Fatalf("reviewer calls = %d, want 1", rec.calls)
	}

	// A thin snapshot is gate-rejected — a quiet skip, not an error.
	fired, err = n.RunStaleReview("client:main", generation.SessionContext{Turns: 0})
	if err != nil || fired {
		t.Fatalf("thin snapshot = (%v, %v), want quiet skip", fired, err)
	}
	if rec.calls != 1 {
		t.Fatalf("reviewer calls after skip = %d, want 1", rec.calls)
	}

	// A session already under review is never double-run.
	n.mu.Lock()
	n.inflight["client:busy"] = time.Now()
	n.mu.Unlock()
	if fired, _ := n.RunStaleReview("client:busy", snap); fired {
		t.Fatal("inflight session must not double-run")
	}
	if rec.calls != 1 {
		t.Fatalf("reviewer calls after inflight guard = %d, want 1", rec.calls)
	}

	// Empty key and disabled nudger are no-ops.
	if fired, _ := n.RunStaleReview("", snap); fired {
		t.Fatal("empty key must not fire")
	}
	n.interval = 0
	if fired, _ := n.RunStaleReview("client:main", snap); fired {
		t.Fatal("disabled nudger must not fire")
	}
}

func TestNudgerWouldReviewReturnsGateAndEnabledState(t *testing.T) {
	n := newTestNudger(t, 3)
	rich := generation.SessionContext{Turns: 2, ToolActivities: []generation.ToolActivity{{Name: "a"}, {Name: "b"}}}
	if !n.WouldReview(rich) {
		t.Fatal("review-worthy snapshot must pass the pre-filter")
	}
	if n.WouldReview(generation.SessionContext{Turns: 0}) {
		t.Fatal("thin snapshot must not pass")
	}
	n.interval = 0
	if n.WouldReview(rich) {
		t.Fatal("disabled nudger must not pass")
	}
}
