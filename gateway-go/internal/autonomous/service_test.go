package autonomous

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// mockAgentRunner is a test double for AgentRunner.
type mockAgentRunner struct {
	output    string
	err       error
	callCount atomic.Int32
}

func (m *mockAgentRunner) RunAgentTurn(_ context.Context, _, _ string) (string, error) {
	m.callCount.Add(1)
	return m.output, m.err
}

func newTestService(t *testing.T, runner AgentRunner) *Service {
	t.Helper()
	dir := t.TempDir()
	cfg := ServiceConfig{
		GoalStorePath:  filepath.Join(dir, "goals.json"),
		CycleTimeoutMs: 5000,
	}
	return NewService(cfg, runner, nil)
}

func TestService_RunCycle_NoGoals(t *testing.T) {
	runner := &mockAgentRunner{output: "nothing to do"}
	svc := newTestService(t, runner)

	outcome, err := svc.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if outcome.Status != "skipped" {
		t.Errorf("status = %q, want skipped", outcome.Status)
	}
	if runner.callCount.Load() != 0 {
		t.Error("agent should not be called when no goals exist")
	}
}

func TestService_RunCycle_WithGoal(t *testing.T) {
	goalUpdate := "```goal_update\n" +
		`{"goalUpdates": [{"id": "PLACEHOLDER", "status": "active", "note": "진행 중"}]}` +
		"\n```"
	runner := &mockAgentRunner{}
	svc := newTestService(t, runner)

	goal, err := svc.AddGoal("test goal", "high")
	if err != nil {
		t.Fatalf("AddGoal: %v", err)
	}

	// Set the mock to return output with the real goal ID.
	runner.output = fmt.Sprintf("```goal_update\n"+
		`{"goalUpdates": [{"id": "%s", "status": "active", "note": "진행 중"}]}`+
		"\n```", goal.ID)
	_ = goalUpdate

	outcome, err := svc.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if outcome.Status != "ok" {
		t.Errorf("status = %q, want ok", outcome.Status)
	}
	if len(outcome.GoalUpdates) != 1 {
		t.Fatalf("goalUpdates = %d, want 1", len(outcome.GoalUpdates))
	}
	if outcome.GoalUpdates[0].Note != "진행 중" {
		t.Errorf("note = %q", outcome.GoalUpdates[0].Note)
	}

	// Verify goal was updated in store.
	goals, _ := svc.Goals().List()
	if goals[0].LastNote != "진행 중" {
		t.Errorf("stored note = %q", goals[0].LastNote)
	}
	if goals[0].CycleCount != 1 {
		t.Errorf("cycleCount = %d, want 1", goals[0].CycleCount)
	}
}

func TestService_RunCycle_AgentError(t *testing.T) {
	runner := &mockAgentRunner{err: fmt.Errorf("LLM timeout")}
	svc := newTestService(t, runner)
	svc.AddGoal("test goal", "medium")

	outcome, err := svc.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if outcome.Status != "error" {
		t.Errorf("status = %q, want error", outcome.Status)
	}
	if outcome.Error == "" {
		t.Error("expected non-empty error")
	}

	status := svc.Status()
	if status.ConsecutiveErr != 1 {
		t.Errorf("consecutiveErrors = %d, want 1", status.ConsecutiveErr)
	}
	if status.TotalErrors != 1 {
		t.Errorf("totalErrors = %d, want 1", status.TotalErrors)
	}
}

func TestService_RunCycle_ConsecutiveErrorReset(t *testing.T) {
	runner := &mockAgentRunner{err: fmt.Errorf("fail")}
	svc := newTestService(t, runner)
	svc.AddGoal("test", "medium")

	// Two failures.
	svc.RunCycle(context.Background())
	svc.RunCycle(context.Background())
	if svc.Status().ConsecutiveErr != 2 {
		t.Fatalf("expected 2 consecutive errors")
	}

	// Success resets consecutive errors.
	runner.err = nil
	runner.output = "done"
	svc.RunCycle(context.Background())
	if svc.Status().ConsecutiveErr != 0 {
		t.Errorf("consecutiveErrors = %d, want 0 after success", svc.Status().ConsecutiveErr)
	}
}

func TestService_RunCycle_Concurrent(t *testing.T) {
	runner := &mockAgentRunner{output: "ok"}
	svc := newTestService(t, runner)
	svc.AddGoal("test", "medium")

	// Slow runner to test concurrency guard.
	slowRunner := &mockAgentRunner{}
	slowRunner.output = "ok"
	svc.agent = &slowAgentRunner{delay: 100 * time.Millisecond, output: "ok"}

	// Start first cycle.
	done := make(chan struct{})
	go func() {
		svc.RunCycle(context.Background())
		close(done)
	}()

	// Give it time to start.
	time.Sleep(20 * time.Millisecond)

	// Second cycle should fail immediately.
	_, err := svc.RunCycle(context.Background())
	if err == nil {
		t.Error("expected error for concurrent cycle")
	}

	<-done
}

type slowAgentRunner struct {
	delay  time.Duration
	output string
}

func (s *slowAgentRunner) RunAgentTurn(ctx context.Context, _, _ string) (string, error) {
	select {
	case <-time.After(s.delay):
		return s.output, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestService_RunCycleAsync(t *testing.T) {
	runner := &mockAgentRunner{output: "done"}
	svc := newTestService(t, runner)
	svc.AddGoal("async test", "medium")

	if err := svc.RunCycleAsync(); err != nil {
		t.Fatalf("RunCycleAsync: %v", err)
	}

	// Wait for async cycle to complete.
	time.Sleep(200 * time.Millisecond)
	if runner.callCount.Load() != 1 {
		t.Errorf("callCount = %d, want 1", runner.callCount.Load())
	}
}

func TestService_StopCancelsAsync(t *testing.T) {
	svc := newTestService(t, &slowAgentRunner{delay: 5 * time.Second, output: "ok"})
	svc.AddGoal("test", "medium")

	svc.RunCycleAsync()
	time.Sleep(50 * time.Millisecond) // let it start
	svc.Stop()

	// After stop, the service context is cancelled. Wait briefly to confirm.
	time.Sleep(100 * time.Millisecond)
	status := svc.Status()
	if status.CycleRunning {
		t.Error("cycle should not be running after Stop")
	}
}

func TestService_Status(t *testing.T) {
	runner := &mockAgentRunner{output: "ok"}
	svc := newTestService(t, runner)

	status := svc.Status()
	if status.ActiveGoals != 0 {
		t.Errorf("activeGoals = %d, want 0", status.ActiveGoals)
	}
	if !status.Enabled {
		t.Error("should be enabled by default")
	}
}

func TestService_SetEnabled(t *testing.T) {
	svc := newTestService(t, &mockAgentRunner{})
	svc.SetEnabled(false)
	if svc.Enabled() {
		t.Error("expected disabled")
	}
	svc.SetEnabled(true)
	if !svc.Enabled() {
		t.Error("expected enabled")
	}
}

func TestService_NoAgent(t *testing.T) {
	dir := t.TempDir()
	cfg := ServiceConfig{GoalStorePath: filepath.Join(dir, "goals.json")}
	svc := NewService(cfg, nil, nil)

	_, err := svc.RunCycle(context.Background())
	if err == nil {
		t.Error("expected error when agent is nil")
	}
}

func TestTruncateOutput(t *testing.T) {
	tests := []struct {
		input   string
		maxLen  int
		wantLen int
	}{
		{"hello", 10, 5},
		{"hello", 5, 5},
		{"hello", 3, 6}, // 3 runes + "..."
		{"한국어테스트", 3, 6}, // 3 runes + "..." (3 chars)
	}
	for _, tt := range tests {
		got := truncateOutput(tt.input, tt.maxLen)
		runes := []rune(got)
		if len(runes) != tt.wantLen {
			t.Errorf("truncateOutput(%q, %d) = %q (len %d runes), want %d runes",
				tt.input, tt.maxLen, got, len(runes), tt.wantLen)
		}
	}
}

func TestTruncateOutput_UTF8Safety(t *testing.T) {
	// Ensure Korean text is not broken at byte boundaries.
	input := "한국어로 작성된 긴 텍스트입니다"
	result := truncateOutput(input, 5)
	// Should be valid UTF-8 and not break mid-character.
	for i, r := range result {
		if r == '\uFFFD' {
			t.Errorf("invalid UTF-8 at position %d", i)
		}
	}
}

func TestBuildCycleSummary(t *testing.T) {
	tests := []struct {
		name    string
		outcome CycleOutcome
		want    string
	}{
		{
			"skipped",
			CycleOutcome{Status: "skipped"},
			"이전 사이클: 활성 목표 없어 건너뜀",
		},
		{
			"error",
			CycleOutcome{Status: "error", Error: "timeout"},
			"이전 사이클: 오류 발생 — timeout",
		},
		{
			"ok no updates",
			CycleOutcome{Status: "ok"},
			"이전 사이클: 완료 (목표 업데이트 없음)",
		},
		{
			"ok with updates",
			CycleOutcome{Status: "ok", GoalUpdates: []GoalUpdate{
				{ID: "g1", Note: "진행 완료"},
			}},
			"이전 사이클 진행: [g1] 진행 완료",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCycleSummary(&tt.outcome)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
