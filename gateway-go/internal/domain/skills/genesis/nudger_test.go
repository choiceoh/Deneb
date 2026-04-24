package genesis

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// newTestNudger creates a Nudger with a throwaway Service that short-
// circuits LLM calls. The underlying Service is created with zero deps;
// callers that need the evaluator path can supply their own config.
func newTestNudger(t *testing.T, interval int) *Nudger {
	t.Helper()
	svc := &Service{
		cfg: Config{
			MinToolCalls:     5,
			MinTurns:         3,
			MaxSkillsPerDay:  10,
			CooldownPerSkill: 24 * time.Hour,
			OutputDir:        t.TempDir(),
		},
		recentSkills: make(map[string]time.Time),
		logger:       slog.Default(),
	}
	return NewNudger(svc, NudgerConfig{Interval: interval}, slog.Default())
}

func TestNudger_Disabled_IntervalZero(t *testing.T) {
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
	n.OnToolCalls(context.TODO(), "s", 5, SessionContext{})
}

func TestNudger_CountsIncrement(t *testing.T) {
	n := newTestNudger(t, 10)
	for range 5 {
		n.OnToolCalls(context.TODO(), "session-a", 1, SessionContext{})
	}
	if got := n.Count("session-a"); got != 5 {
		t.Errorf("expected count=5, got %d", got)
	}
	// Other sessions are independent.
	if got := n.Count("session-b"); got != 0 {
		t.Errorf("expected session-b count=0, got %d", got)
	}
}

func TestNudger_ThresholdResetsCounter(t *testing.T) {
	n := newTestNudger(t, 10)
	// Supply a snapshot that Evaluate will reject so nothing spawns.
	sctx := SessionContext{
		ToolActivities: []ToolActivity{{Name: "read"}},
		Turns:          1, // fails MinTurns gate in Evaluate
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

func TestNudger_Reset(t *testing.T) {
	n := newTestNudger(t, 10)
	n.OnToolCalls(context.TODO(), "s", 7, SessionContext{})
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
				n.OnToolCalls(context.TODO(), "s", 1, SessionContext{
					ToolActivities: []ToolActivity{{Name: "read"}},
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
	sctx := SessionContext{
		Turns: 1,
		ToolActivities: []ToolActivity{
			{Name: "read"}, {Name: "exec"}, {Name: "write"},
			{Name: "grep"}, {Name: "read"},
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

func TestNudger_FromEnv_DefaultWhenUnset(t *testing.T) {
	t.Setenv("DENEB_SKILL_NUDGE_INTERVAL", "")
	svc := &Service{cfg: Config{}, recentSkills: make(map[string]time.Time)}
	n := NewNudgerFromEnv(svc, slog.Default())
	if n.Interval() != DefaultNudgeInterval {
		t.Errorf("expected default interval %d, got %d", DefaultNudgeInterval, n.Interval())
	}
}

func TestNudger_FromEnv_ExplicitZeroDisables(t *testing.T) {
	t.Setenv("DENEB_SKILL_NUDGE_INTERVAL", "0")
	svc := &Service{cfg: Config{}, recentSkills: make(map[string]time.Time)}
	n := NewNudgerFromEnv(svc, slog.Default())
	if n.Enabled() {
		t.Errorf("expected disabled with env=0")
	}
}

func TestNudger_FromEnv_InvalidValueUsesDefault(t *testing.T) {
	t.Setenv("DENEB_SKILL_NUDGE_INTERVAL", "not-a-number")
	svc := &Service{cfg: Config{}, recentSkills: make(map[string]time.Time)}
	n := NewNudgerFromEnv(svc, slog.Default())
	if n.Interval() != DefaultNudgeInterval {
		t.Errorf("expected fallback to %d, got %d", DefaultNudgeInterval, n.Interval())
	}
}

func TestNudger_InflightBlocksSecondFire(t *testing.T) {
	n := newTestNudger(t, 5)
	// Manually flip inflight.
	n.mu.Lock()
	n.inflight["s"] = time.Now()
	n.mu.Unlock()
	n.OnToolCalls(context.TODO(), "s", 10, SessionContext{
		Turns: 5, ToolActivities: []ToolActivity{{Name: "x"}},
	})
	// Inflight path rejects so the count is never incremented.
	if got := n.Count("s"); got != 0 {
		t.Errorf("expected count to stay 0 while inflight, got %d", got)
	}
}
