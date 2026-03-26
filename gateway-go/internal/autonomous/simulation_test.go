package autonomous

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scenarioRunner is a mock that cycles through different outputs per call.
type scenarioRunner struct {
	outputs []string
	call    int
}

func (s *scenarioRunner) RunAgentTurn(_ context.Context, _, _ string) (string, error) {
	if s.call >= len(s.outputs) {
		return s.outputs[len(s.outputs)-1], nil
	}
	out := s.outputs[s.call]
	s.call++
	return out, nil
}

func (s *scenarioRunner) ResetSession(_ string) error { return nil }

func newSimService(t *testing.T, runner AgentRunner) *Service {
	t.Helper()
	dir := t.TempDir()
	cfg := ServiceConfig{
		GoalStorePath:  filepath.Join(dir, "goals.json"),
		CycleTimeoutMs: 5000,
	}
	return NewService(cfg, runner, nil)
}

// --- Scenario 1: Goal Starvation ---
// 3 goals of varying priority. Mock always works on the first (highest priority).
// After several cycles, the prompt should flag starved goals.
func TestSimulation_GoalStarvation(t *testing.T) {
	svc := newSimService(t, nil)
	gA, _ := svc.AddGoal("서버 모니터링 설정", "high")
	gB, _ := svc.AddGoal("문서 업데이트", "medium")
	svc.AddGoal("코드 정리", "low")

	// Mock always returns update for goal A only.
	runner := &scenarioRunner{}
	for i := 0; i < 5; i++ {
		runner.outputs = append(runner.outputs,
			fmt.Sprintf("```goal_update\n"+
				`{"goalUpdates": [{"id": "%s", "status": "active", "note": "사이클 %d 작업"}]}`+
				"\n```", gA.ID, i+1))
	}
	svc.agent = runner

	// Run 5 cycles — only goal A gets worked on.
	for i := 0; i < 5; i++ {
		outcome, err := svc.RunCycle(context.Background())
		if err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
		if outcome.Status != "ok" {
			t.Fatalf("cycle %d: status = %q", i, outcome.Status)
		}
	}

	// Verify goal B has never been worked on.
	goals, _ := svc.Goals().List()
	var goalB Goal
	for _, g := range goals {
		if g.ID == gB.ID {
			goalB = g
			break
		}
	}
	if goalB.CycleCount != 0 {
		t.Errorf("goal B cycleCount = %d, want 0 (starved)", goalB.CycleCount)
	}
	if goalB.LastWorkedAtMs != 0 {
		t.Error("goal B should have zero LastWorkedAtMs (never worked)")
	}

	// Verify the prompt flags starvation.
	active, _ := svc.Goals().ActiveGoals()
	prompt := buildDecisionPrompt(active, nil)
	if !strings.Contains(prompt, "장기 미작업") {
		t.Error("prompt should contain starvation warning for unworked goals")
	}
}

// --- Scenario 2: Note History Accumulation ---
// Verify that NoteHistory builds up correctly across cycles.
func TestSimulation_NoteHistory(t *testing.T) {
	svc := newSimService(t, nil)
	g, _ := svc.AddGoal("테스트 목표", "medium")

	notes := []string{"1단계: 파일 분석", "2단계: 코드 수정", "3단계: 테스트 작성", "4단계: 리뷰"}
	runner := &scenarioRunner{}
	for _, note := range notes {
		runner.outputs = append(runner.outputs,
			fmt.Sprintf("```goal_update\n"+
				`{"goalUpdates": [{"id": "%s", "status": "active", "note": "%s"}]}`+
				"\n```", g.ID, note))
	}
	svc.agent = runner

	for i := 0; i < 4; i++ {
		if _, err := svc.RunCycle(context.Background()); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
	}

	goals, _ := svc.Goals().List()
	goal := goals[0]

	// LastNote should be the most recent.
	if goal.LastNote != "4단계: 리뷰" {
		t.Errorf("LastNote = %q, want '4단계: 리뷰'", goal.LastNote)
	}

	// NoteHistory should have previous 3 notes (newest first).
	if len(goal.NoteHistory) != 3 {
		t.Fatalf("NoteHistory len = %d, want 3", len(goal.NoteHistory))
	}
	if goal.NoteHistory[0] != "3단계: 테스트 작성" {
		t.Errorf("NoteHistory[0] = %q, want '3단계: 테스트 작성'", goal.NoteHistory[0])
	}
	if goal.NoteHistory[1] != "2단계: 코드 수정" {
		t.Errorf("NoteHistory[1] = %q, want '2단계: 코드 수정'", goal.NoteHistory[1])
	}
	if goal.NoteHistory[2] != "1단계: 파일 분석" {
		t.Errorf("NoteHistory[2] = %q, want '1단계: 파일 분석'", goal.NoteHistory[2])
	}

	// Verify prompt includes note history.
	active, _ := svc.Goals().ActiveGoals()
	prompt := buildDecisionPrompt(active, nil)
	if !strings.Contains(prompt, "이전(2):") {
		t.Error("prompt should contain previous note history")
	}
}

// --- Scenario 3: Stale Goal Auto-Pause ---
// Goal with repetitive notes should be auto-paused after threshold.
func TestSimulation_StaleAutoPause(t *testing.T) {
	svc := newSimService(t, nil)
	g, _ := svc.AddGoal("막힌 작업", "medium")

	// Mock returns the same note every cycle.
	runner := &scenarioRunner{}
	for i := 0; i < stalePauseThreshold+2; i++ {
		runner.outputs = append(runner.outputs,
			fmt.Sprintf("```goal_update\n"+
				`{"goalUpdates": [{"id": "%s", "status": "active", "note": "진행 중... 분석 계속"}]}`+
				"\n```", g.ID))
	}
	svc.agent = runner

	// Run enough cycles to trigger auto-pause.
	for i := 0; i < stalePauseThreshold+2; i++ {
		svc.RunCycle(context.Background())
	}

	goals, _ := svc.Goals().List()
	if goals[0].Status != StatusPaused {
		t.Errorf("stale goal status = %q, want paused", goals[0].Status)
	}
	if !strings.Contains(goals[0].PausedReason, "시스템 자동 중단") {
		t.Errorf("pausedReason = %q, should contain auto-pause marker", goals[0].PausedReason)
	}
}

// --- Scenario 4: Stale Detection —non-stale notes ---
// Varying notes should NOT trigger stale detection.
func TestSimulation_NotStaleWithVaryingNotes(t *testing.T) {
	svc := newSimService(t, nil)
	g, _ := svc.AddGoal("활발한 작업", "medium")

	runner := &scenarioRunner{}
	for i := 0; i < 12; i++ {
		runner.outputs = append(runner.outputs,
			fmt.Sprintf("```goal_update\n"+
				`{"goalUpdates": [{"id": "%s", "status": "active", "note": "사이클 %d: 새로운 진행 내용"}]}`+
				"\n```", g.ID, i+1))
	}
	svc.agent = runner

	for i := 0; i < 12; i++ {
		svc.RunCycle(context.Background())
	}

	goals, _ := svc.Goals().List()
	if goals[0].Status != StatusActive {
		t.Errorf("goal with varying notes should remain active, got %q", goals[0].Status)
	}
}

// --- Scenario 5: Deferred High-Priority Signal ---
// A goal added during cooldown should trigger a cycle after cooldown expires.
func TestSimulation_DeferredSignalAfterCooldown(t *testing.T) {
	runner := &mockAgentRunner{output: "ok"}
	svc := newSimService(t, runner)
	svc.AddGoal("기존 목표", "medium")

	cfg := AttentionConfig{
		CycleInterval: time.Minute,
		CooldownMs:    200, // 200ms cooldown for test speed
	}
	att := NewAttention(svc, cfg, svc.logger)

	// First push triggers immediately.
	att.Push(Signal{Kind: SignalGoalAdded, Priority: SignalPriorityHigh})

	// Wait a bit for the cycle to start, but not for cooldown to expire.
	time.Sleep(50 * time.Millisecond)

	// Second high-priority push during cooldown — should be deferred, not dropped.
	att.Push(Signal{Kind: SignalGoalAdded, Priority: SignalPriorityHigh, Context: "deferred"})

	// Wait for cooldown to expire + deferred trigger to fire.
	time.Sleep(400 * time.Millisecond)

	// The mock should have been called at least twice (initial + deferred).
	calls := runner.callCount.Load()
	if calls < 2 {
		t.Errorf("expected ≥2 agent calls (initial + deferred), got %d", calls)
	}
}

// --- Scenario 6: Status Metrics ---
// Verify that SuccessRate and AvgDurationMs are computed correctly.
func TestSimulation_StatusMetrics(t *testing.T) {
	svc := newSimService(t, nil)
	g, _ := svc.AddGoal("메트릭 테스트", "medium")

	runner := &scenarioRunner{}
	for i := 0; i < 5; i++ {
		runner.outputs = append(runner.outputs,
			fmt.Sprintf("```goal_update\n"+
				`{"goalUpdates": [{"id": "%s", "status": "active", "note": "step %d"}]}`+
				"\n```", g.ID, i))
	}
	svc.agent = runner

	// Run 5 successful cycles.
	for i := 0; i < 5; i++ {
		svc.RunCycle(context.Background())
	}

	status := svc.Status()
	if status.SuccessRate < 0.9 {
		t.Errorf("successRate = %.2f, want ~1.0", status.SuccessRate)
	}
	if status.TotalCycles != 5 {
		t.Errorf("totalCycles = %d, want 5", status.TotalCycles)
	}

	// Now cause errors.
	svc.agent = &mockAgentRunner{err: fmt.Errorf("fail")}
	for i := 0; i < 5; i++ {
		svc.RunCycle(context.Background())
	}

	status = svc.Status()
	// 5 ok + 5 error out of 10 recent = ~0.5 success rate.
	if status.SuccessRate < 0.4 || status.SuccessRate > 0.6 {
		t.Errorf("successRate = %.2f, want ~0.5", status.SuccessRate)
	}
}

// --- Scenario 7: LastWorkedAtMs Tracking ---
func TestSimulation_LastWorkedAtMs(t *testing.T) {
	svc := newSimService(t, nil)
	g, _ := svc.AddGoal("추적 테스트", "medium")

	beforeMs := time.Now().UnixMilli()

	runner := &scenarioRunner{
		outputs: []string{
			fmt.Sprintf("```goal_update\n"+
				`{"goalUpdates": [{"id": "%s", "status": "active", "note": "done"}]}`+
				"\n```", g.ID),
		},
	}
	svc.agent = runner
	svc.RunCycle(context.Background())

	goals, _ := svc.Goals().List()
	if goals[0].LastWorkedAtMs < beforeMs {
		t.Errorf("LastWorkedAtMs = %d, should be >= %d", goals[0].LastWorkedAtMs, beforeMs)
	}
}

// --- Scenario 8: Prompt shows stale warning ---
func TestSimulation_PromptStaleWarning(t *testing.T) {
	goals := []Goal{
		{
			ID:          "stale1",
			Description: "반복 정체 목표",
			Priority:    PriorityHigh,
			Status:      StatusActive,
			CycleCount:  7,
			LastNote:    "진행 중입니다 분석 계속합니다",
			NoteHistory: []string{
				"진행 중입니다 분석 계속합니다",
				"진행 중입니다 분석 계속합니다",
			},
			CreatedAtMs:    time.Now().Add(-48 * time.Hour).UnixMilli(),
			LastWorkedAtMs: time.Now().UnixMilli(),
		},
	}

	prompt := buildDecisionPrompt(goals, nil)
	if !strings.Contains(prompt, "반복 정체") {
		t.Error("prompt should flag stale goal with repetitive notes")
	}
}

// --- Scenario 9: NoteHistory cap at maxNoteHistory ---
func TestSimulation_NoteHistoryCap(t *testing.T) {
	store := tempGoalStore(t)
	g, _ := store.Add("cap test", "medium")

	// Update 6 times — history should cap at 3.
	for i := 0; i < 6; i++ {
		store.Update(g.ID, StatusActive, fmt.Sprintf("note %d", i+1))
	}

	goals, _ := store.List()
	goal := goals[0]

	if goal.LastNote != "note 6" {
		t.Errorf("LastNote = %q, want 'note 6'", goal.LastNote)
	}
	if len(goal.NoteHistory) != maxNoteHistory {
		t.Fatalf("NoteHistory len = %d, want %d", len(goal.NoteHistory), maxNoteHistory)
	}
	// Should be: [note 5, note 4, note 3] (newest first, capped at 3).
	if goal.NoteHistory[0] != "note 5" {
		t.Errorf("NoteHistory[0] = %q, want 'note 5'", goal.NoteHistory[0])
	}
	if goal.NoteHistory[2] != "note 3" {
		t.Errorf("NoteHistory[2] = %q, want 'note 3'", goal.NoteHistory[2])
	}
}

// --- Scenario 10: IsStale with insufficient history ---
func TestIsStale(t *testing.T) {
	tests := []struct {
		name string
		goal Goal
		want bool
	}{
		{
			name: "low cycle count",
			goal: Goal{CycleCount: 3, LastNote: "same", NoteHistory: []string{"same", "same"}},
			want: false,
		},
		{
			name: "no history",
			goal: Goal{CycleCount: 10, LastNote: "note"},
			want: false,
		},
		{
			name: "varying notes",
			goal: Goal{CycleCount: 10, LastNote: "different note", NoteHistory: []string{"other note", "another note"}},
			want: false,
		},
		{
			name: "repetitive notes",
			goal: Goal{CycleCount: 7, LastNote: "진행 중 분석 계속", NoteHistory: []string{"진행 중 분석 계속", "진행 중 분석 계속"}},
			want: true,
		},
		{
			name: "same prefix different suffix over 50 runes",
			goal: Goal{
				CycleCount:  7,
				LastNote:    "이 목표에 대해 계속 분석을 진행하고 있습니다. 코드베이스를 확인하고 관련 파일을 읽고 있습니다만 아직 구체적인 결과는 없습니다 — 버전 A",
				NoteHistory: []string{
					"이 목표에 대해 계속 분석을 진행하고 있습니다. 코드베이스를 확인하고 관련 파일을 읽고 있습니다만 아직 구체적인 결과는 없습니다 — 버전 B",
					"이 목표에 대해 계속 분석을 진행하고 있습니다. 코드베이스를 확인하고 관련 파일을 읽고 있습니다만 아직 구체적인 결과는 없습니다 — 버전 C",
				},
			},
			want: true, // first 50 runes are identical
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.goal.IsStale(); got != tt.want {
				t.Errorf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}
