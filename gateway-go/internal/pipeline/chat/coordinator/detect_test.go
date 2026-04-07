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
			message: "Modify internal/pipeline/chat/handler.go, internal/runtime/session/manager.go, and internal/runtime/rpc/dispatch.go",
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
			name:    "two file paths (lowered threshold)",
			message: "Update main.go and config.go",
			want:    true,
		},
		{
			name:    "korean architecture keyword",
			message: "전체 구조를 바꿔주세요",
			want:    true,
		},
		{
			name:    "english restructure keyword",
			message: "Restructure the API layer",
			want:    true,
		},
		{
			name:    "compound action with conjunctions",
			message: "이거 고치고 그리고 저거도 바꾸고 추가로 테스트 만들어줘",
			want:    true,
		},
		{
			name:    "single conjunction not enough",
			message: "이거 고치고 그리고 테스트도 돌려줘",
			want:    false,
		},
		{
			name:    "simple single task stays false",
			message: "간단한 버그 하나 고쳐줘",
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
