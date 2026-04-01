package coordinator

import "testing"

func TestShouldSuggestCoordinator(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{
			name:    "korean multi-file keyword",
			message: "여러 파일에 걸쳐서 리팩토링 해주세요",
			want:    true,
		},
		{
			name:    "english refactor across",
			message: "Please refactor across the codebase to rename FooBar",
			want:    true,
		},
		{
			name:    "explicit coordinator request",
			message: "Use coordinator mode for this task",
			want:    true,
		},
		{
			name:    "three file paths",
			message: "Modify internal/chat/handler.go, internal/session/manager.go, and internal/rpc/dispatch.go",
			want:    true,
		},
		{
			name:    "simple single-file request",
			message: "Fix the bug in handler.go",
			want:    false,
		},
		{
			name:    "no file paths or keywords",
			message: "What does the gateway do?",
			want:    false,
		},
		{
			name:    "two file paths (below threshold)",
			message: "Update main.go and config.go",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldSuggestCoordinator(tt.message)
			if got != tt.want {
				t.Errorf("ShouldSuggestCoordinator(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}
