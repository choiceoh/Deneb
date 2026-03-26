package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestParseGoalUpdates_Structured(t *testing.T) {
	output := `I worked on the goal.

` + "```goal_update" + `
{"goalUpdates": [{"id": "abc123", "status": "completed", "note": "Task done"}]}
` + "```"

	updates := parseGoalUpdates(output, []string{"abc123"})
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if updates[0].ID != "abc123" {
		t.Errorf("expected ID abc123, got %q", updates[0].ID)
	}
	if updates[0].Status != StatusCompleted {
		t.Errorf("expected status completed, got %q", updates[0].Status)
	}
	if updates[0].Note != "Task done" {
		t.Errorf("expected note 'Task done', got %q", updates[0].Note)
	}
}

func TestParseGoalUpdates_MultipleUpdates(t *testing.T) {
	output := "```goal_update\n" +
		`{"goalUpdates": [` +
		`{"id": "a1", "status": "active", "note": "progress"},` +
		`{"id": "b2", "status": "paused", "note": "blocked"}` +
		`]}` +
		"\n```"

	updates := parseGoalUpdates(output, []string{"a1", "b2"})
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}
	if updates[0].Status != StatusActive {
		t.Errorf("first update status: got %q, want %q", updates[0].Status, StatusActive)
	}
	if updates[1].Status != StatusPaused {
		t.Errorf("second update status: got %q, want %q", updates[1].Status, StatusPaused)
	}
}

func TestParseGoalUpdates_FallbackNote(t *testing.T) {
	output := "I looked at the code and made changes.\n\nThe task is partially done."

	updates := parseGoalUpdates(output, []string{"goal1"})
	if len(updates) != 1 {
		t.Fatalf("expected 1 fallback update, got %d", len(updates))
	}
	if updates[0].ID != "goal1" {
		t.Errorf("expected fallback to use first active goal ID")
	}
	if updates[0].Status != StatusActive {
		t.Errorf("expected fallback status active, got %q", updates[0].Status)
	}
	if updates[0].Note == "" {
		t.Error("expected non-empty fallback note")
	}
}

func TestParseGoalUpdates_NoActiveGoals(t *testing.T) {
	output := "Some output text."
	updates := parseGoalUpdates(output, nil)
	if updates != nil {
		t.Errorf("expected nil updates with no active goals, got %v", updates)
	}
}

func TestParseGoalUpdates_EmptyOutput(t *testing.T) {
	updates := parseGoalUpdates("", []string{"goal1"})
	if updates != nil {
		t.Errorf("expected nil updates for empty output, got %v", updates)
	}
}

func TestParseGoalUpdates_MalformedJSON(t *testing.T) {
	output := "```goal_update\n{invalid json}\n```\n\nSome fallback text here."
	updates := parseGoalUpdates(output, []string{"goal1"})
	// Should fall back to extracting note.
	if len(updates) != 1 {
		t.Fatalf("expected 1 fallback update, got %d", len(updates))
	}
	if updates[0].Note == "" {
		t.Error("expected non-empty fallback note from malformed JSON")
	}
}

func TestValidateUpdates(t *testing.T) {
	tests := []struct {
		name     string
		input    []GoalUpdate
		wantLen  int
		wantNote string
	}{
		{
			name:    "empty ID filtered",
			input:   []GoalUpdate{{ID: "", Status: "active"}},
			wantLen: 0,
		},
		{
			name:    "invalid status defaults to active",
			input:   []GoalUpdate{{ID: "a", Status: "invalid"}},
			wantLen: 1,
		},
		{
			name:     "long note truncated",
			input:    []GoalUpdate{{ID: "a", Status: "active", Note: strings.Repeat("x", 600)}},
			wantLen:  1,
			wantNote: strings.Repeat("x", 497) + "...",
		},
		{
			name:    "valid statuses preserved",
			input:   []GoalUpdate{{ID: "a", Status: "active"}, {ID: "b", Status: "completed"}, {ID: "c", Status: "paused"}},
			wantLen: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateUpdates(tc.input)
			if len(got) != tc.wantLen {
				t.Fatalf("expected %d updates, got %d", tc.wantLen, len(got))
			}
			if tc.wantNote != "" && got[0].Note != tc.wantNote {
				t.Errorf("note mismatch: got %q (len %d), want len %d", got[0].Note[:20], len(got[0].Note), len(tc.wantNote))
			}
		})
	}
}

func TestExtractFallbackNote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "last paragraph",
			input: "First line.\n\nLast paragraph here.",
			want:  "Last paragraph here.",
		},
		{
			name:  "skips code blocks",
			input: "Text above.\n```\ncode\n```\nAfter code.",
			want:  "After code.",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "multi-line last paragraph",
			input: "First.\n\nLine one of last.\nLine two of last.",
			want:  "Line one of last. Line two of last.",
		},
		{
			name:  "truncates long note",
			input: strings.Repeat("word ", 100),
			want:  "", // just check it's <= 200
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFallbackNote(tc.input)
			if tc.want != "" {
				if got != tc.want {
					t.Errorf("got %q, want %q", got, tc.want)
				}
			}
			if len(got) > 200 {
				t.Errorf("note exceeds 200 chars: len=%d", len(got))
			}
		})
	}
}

func TestBuildDecisionPrompt_NoGoals(t *testing.T) {
	prompt := buildDecisionPrompt(nil, nil)
	if !strings.Contains(prompt, "활성 목표 없음") {
		t.Error("expected 'no active goals' message in prompt")
	}
}

func TestBuildDecisionPrompt_WithGoals(t *testing.T) {
	goals := []Goal{
		{
			ID:          "abc",
			Description: "Fix the bug",
			Priority:    PriorityHigh,
			Status:      StatusActive,
			CycleCount:  3,
			CreatedAtMs: time.Now().Add(-48 * time.Hour).UnixMilli(),
			LastNote:    "Found root cause",
		},
	}

	prompt := buildDecisionPrompt(goals, nil)
	if !strings.Contains(prompt, "Fix the bug") {
		t.Error("expected goal description in prompt")
	}
	if !strings.Contains(prompt, "HIGH") {
		t.Error("expected priority label in prompt")
	}
	if !strings.Contains(prompt, "abc") {
		t.Error("expected goal ID in prompt")
	}
	if !strings.Contains(prompt, "Found root cause") {
		t.Error("expected last note in prompt")
	}
	if !strings.Contains(prompt, "3회 작업") {
		t.Error("expected cycle count in prompt")
	}
	if !strings.Contains(prompt, "2일 경과") {
		t.Error("expected age in prompt")
	}
}

func TestBuildDecisionPrompt_WithLastCycle(t *testing.T) {
	goals := []Goal{{ID: "a", Description: "goal", Priority: PriorityMedium, Status: StatusActive, CreatedAtMs: time.Now().UnixMilli()}}
	lastCycle := &CycleState{LastSummary: "Previous work summary"}

	prompt := buildDecisionPrompt(goals, lastCycle)
	if !strings.Contains(prompt, "Previous work summary") {
		t.Error("expected last cycle summary in prompt")
	}
	if !strings.Contains(prompt, "이전 사이클") {
		t.Error("expected previous cycle section header")
	}
}

func TestBuildDecisionPrompt_WithRecentlyChanged(t *testing.T) {
	goals := []Goal{{ID: "a", Description: "active", Priority: PriorityMedium, Status: StatusActive, CreatedAtMs: time.Now().UnixMilli()}}
	changed := []Goal{
		{Description: "completed task", Status: StatusCompleted, LastNote: "all done"},
		{Description: "paused task", Status: StatusPaused},
	}

	prompt := buildDecisionPrompt(goals, nil, changed...)
	if !strings.Contains(prompt, "✓ 완료") {
		t.Error("expected completed label")
	}
	if !strings.Contains(prompt, "⏸ 중단") {
		t.Error("expected paused label")
	}
	if !strings.Contains(prompt, "completed task") {
		t.Error("expected completed task description")
	}
	if !strings.Contains(prompt, "all done") {
		t.Error("expected completion note")
	}
}

func TestBuildDecisionPrompt_OutputFormatSection(t *testing.T) {
	goals := []Goal{{ID: "a", Description: "goal", Priority: PriorityMedium, Status: StatusActive, CreatedAtMs: time.Now().UnixMilli()}}
	prompt := buildDecisionPrompt(goals, nil)
	if !strings.Contains(prompt, "goal_update") {
		t.Error("expected goal_update format in prompt")
	}
	if !strings.Contains(prompt, "goalUpdates") {
		t.Error("expected goalUpdates JSON key in prompt")
	}
}

func TestPriorityLabel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{PriorityHigh, "높음"},
		{PriorityMedium, "보통"},
		{PriorityLow, "낮음"},
		{"unknown", "unknown"},
	}
	for _, tc := range tests {
		got := priorityLabel(tc.input)
		if got != tc.want {
			t.Errorf("priorityLabel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
