package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestParseGoalUpdates_Structured(t *testing.T) {
	output := `I've completed the task.

` + "```goal_update\n" + `{"goalUpdates": [{"id": "abc123", "status": "completed", "note": "작업 완료"}]}
` + "```"

	updates := parseGoalUpdates(output, []string{"abc123"})
	if len(updates) != 1 {
		t.Fatalf("len = %d, want 1", len(updates))
	}
	if updates[0].ID != "abc123" {
		t.Errorf("id = %q, want %q", updates[0].ID, "abc123")
	}
	if updates[0].Status != StatusCompleted {
		t.Errorf("status = %q, want %q", updates[0].Status, StatusCompleted)
	}
	if updates[0].Note != "작업 완료" {
		t.Errorf("note = %q", updates[0].Note)
	}
}

func TestParseGoalUpdates_MultipleGoals(t *testing.T) {
	output := "done\n```goal_update\n" +
		`{"goalUpdates": [{"id": "a1", "status": "completed", "note": "done"}, {"id": "b2", "status": "active", "note": "WIP"}]}` +
		"\n```"

	updates := parseGoalUpdates(output, []string{"a1", "b2"})
	if len(updates) != 2 {
		t.Fatalf("len = %d, want 2", len(updates))
	}
}

func TestParseGoalUpdates_Fallback(t *testing.T) {
	// No structured block — should extract fallback note.
	output := "I analyzed the codebase and found several issues.\n\nThe main problem is in the auth module."

	updates := parseGoalUpdates(output, []string{"goal1"})
	if len(updates) != 1 {
		t.Fatalf("len = %d, want 1", len(updates))
	}
	if updates[0].ID != "goal1" {
		t.Errorf("id = %q, want %q", updates[0].ID, "goal1")
	}
	if updates[0].Status != StatusActive {
		t.Errorf("status = %q, want %q", updates[0].Status, StatusActive)
	}
	if updates[0].Note == "" {
		t.Error("expected non-empty fallback note")
	}
}

func TestParseGoalUpdates_EmptyOutput(t *testing.T) {
	updates := parseGoalUpdates("", []string{"goal1"})
	if len(updates) != 0 {
		t.Fatalf("len = %d, want 0", len(updates))
	}
}

func TestParseGoalUpdates_NoGoals(t *testing.T) {
	updates := parseGoalUpdates("some output", nil)
	if len(updates) != 0 {
		t.Fatalf("len = %d, want 0", len(updates))
	}
}

func TestParseGoalUpdates_InvalidJSON(t *testing.T) {
	output := "```goal_update\n{invalid json}\n```\n\nFallback text here."
	updates := parseGoalUpdates(output, []string{"goal1"})
	// Should fall back to note extraction.
	if len(updates) != 1 {
		t.Fatalf("len = %d, want 1 (fallback)", len(updates))
	}
	if updates[0].Status != StatusActive {
		t.Errorf("status = %q, want active (fallback)", updates[0].Status)
	}
	if updates[0].Note == "" {
		t.Error("expected non-empty fallback note from malformed JSON")
	}
}

func TestParseGoalUpdates_InvalidStatus(t *testing.T) {
	output := "```goal_update\n" +
		`{"goalUpdates": [{"id": "abc", "status": "unknown", "note": "test"}]}` +
		"\n```"
	updates := parseGoalUpdates(output, []string{"abc"})
	if len(updates) != 1 {
		t.Fatalf("len = %d, want 1", len(updates))
	}
	// Invalid status should be normalized to "active".
	if updates[0].Status != StatusActive {
		t.Errorf("status = %q, want %q", updates[0].Status, StatusActive)
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
				t.Errorf("note mismatch: got len %d, want len %d", len(got[0].Note), len(tc.wantNote))
			}
		})
	}
}

func TestBuildDecisionPrompt_NoGoals(t *testing.T) {
	prompt := buildDecisionPrompt(nil, nil)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "활성 목표 없음") {
		t.Error("expected '활성 목표 없음' for empty goals")
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
		{ID: "g2", Description: "Add feature", Priority: PriorityLow, LastNote: "started"},
	}

	prompt := buildDecisionPrompt(goals, nil)
	if !strings.Contains(prompt, "abc") || !strings.Contains(prompt, "g2") {
		t.Error("prompt should contain goal IDs")
	}
	if !strings.Contains(prompt, "Fix the bug") {
		t.Error("prompt should contain goal descriptions")
	}
	if !strings.Contains(prompt, "HIGH") {
		t.Error("expected priority label in prompt")
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

func TestBuildDecisionPrompt_WithPreviousCycle(t *testing.T) {
	goals := []Goal{{ID: "g1", Description: "test", Priority: PriorityMedium, CreatedAtMs: time.Now().UnixMilli()}}
	cs := &CycleState{LastSummary: "이전 사이클: 완료"}
	prompt := buildDecisionPrompt(goals, cs)
	if !strings.Contains(prompt, "이전 사이클: 완료") {
		t.Error("prompt should contain previous cycle summary")
	}
}

func TestBuildDecisionPrompt_WithRecentlyChanged(t *testing.T) {
	goals := []Goal{{ID: "g1", Description: "test", Priority: PriorityMedium, CreatedAtMs: time.Now().UnixMilli()}}
	changed := []Goal{
		{Description: "completed goal", Status: StatusCompleted, LastNote: "all done"},
		{Description: "paused goal", Status: StatusPaused},
	}
	prompt := buildDecisionPrompt(goals, nil, changed...)
	if !strings.Contains(prompt, "completed goal") {
		t.Error("prompt should contain recently changed goals")
	}
	if !strings.Contains(prompt, "✓ 완료") {
		t.Error("prompt should show completion label")
	}
	if !strings.Contains(prompt, "⏸ 중단") {
		t.Error("prompt should show paused label")
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
			name:  "only whitespace",
			input: "   \n  \n  ",
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

func TestExtractFallbackNote_MaxLength(t *testing.T) {
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	note := extractFallbackNote(string(long))
	if len(note) > 200 {
		t.Errorf("note len = %d, should be <= 200", len(note))
	}
}
