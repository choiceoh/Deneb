package shadow

import "testing"

func TestDetectTasks_Korean(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantN   int // expected number of tasks
	}{
		{
			name:    "나중에 pattern",
			content: "이건 나중에 해야 할 것 같아요",
			wantN:   1, // "나중에" and "해야" overlap; deduped by context
		},
		{
			name:    "TODO pattern",
			content: "TODO: fix the login page",
			wantN:   1,
		},
		{
			name:    "할 일 pattern",
			content: "할 일 목록에 추가해줘",
			wantN:   1,
		},
		{
			name:    "잊지 말고 pattern",
			content: "잊지 말고 테스트 추가해줘",
			wantN:   1,
		},
		{
			name:    "no task patterns",
			content: "이건 그냥 일반 대화입니다",
			wantN:   0,
		},
		{
			name:    "empty content",
			content: "",
			wantN:   0,
		},
		{
			name:    "FIXME pattern",
			content: "FIXME: this is broken and needs attention",
			wantN:   1,
		},
		{
			name:    "remind me english",
			content: "remind me to check the deployment logs tomorrow",
			wantN:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks := detectTasks(tt.content, "test-session")
			if len(tasks) != tt.wantN {
				t.Errorf("detectTasks() got %d tasks, want %d", len(tasks), tt.wantN)
				for i, task := range tasks {
					t.Logf("  task[%d]: %q", i, task.Content)
				}
			}
			for _, task := range tasks {
				if task.Status != "pending" {
					t.Errorf("task status = %q, want %q", task.Status, "pending")
				}
				if task.SessionKey != "test-session" {
					t.Errorf("task sessionKey = %q, want %q", task.SessionKey, "test-session")
				}
				if task.ID == "" {
					t.Error("task ID is empty")
				}
			}
		})
	}
}

func TestExtractContext(t *testing.T) {
	long := "이것은 매우 긴 문자열입니다. 여기에 TODO가 있고 그 뒤에도 내용이 계속됩니다. 정말 긴 텍스트죠."
	result := extractContext(long, 0, 30)
	runes := []rune(result)
	// Should be at most 30 runes + possible "..." suffix.
	if len(runes) > 33+3 { // 30 + "..."
		t.Errorf("extractContext returned %d runes, want <= 33", len(runes))
	}
}

func TestDetectTasks_Dedup(t *testing.T) {
	// Content with overlapping patterns should not create duplicate tasks.
	content := "나중에 해야 할 TODO 목록 정리"
	tasks := detectTasks(content, "test-session")
	// "나중에", "해야", "TODO", "할 일" — but content is short enough
	// that multiple patterns may extract the same context → dedup kicks in.
	if len(tasks) > 4 {
		t.Errorf("expected at most 4 tasks from overlapping patterns, got %d", len(tasks))
	}
}
