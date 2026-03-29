package discord

import "testing"

func TestAnalyzeReply_MergeConflict(t *testing.T) {
	tests := []struct {
		name string
		text string
		want ReplyOutcome
	}{
		{"conflict markers", "파일에 <<<<<<< HEAD 충돌 마커가 있습니다", OutcomeMergeConflict},
		{"automatic merge failed", "Automatic merge failed; fix conflicts and then commit", OutcomeMergeConflict},
		{"CONFLICT keyword", "CONFLICT (content): Merge conflict in main.go", OutcomeMergeConflict},
		{"unmerged paths", "error: you need to resolve your current index first\nUnmerged paths:", OutcomeMergeConflict},
		{"both modified", "both modified: gateway-go/internal/server/server.go", OutcomeMergeConflict},
		{"Korean merge conflict", "병합 충돌이 발생했습니다. 3개의 파일에서 충돌이 감지되었어요.", OutcomeMergeConflict},
		{"not conflict - normal commit", "커밋 완료 [main abc1234]", OutcomeCommitDone},
		{"not conflict - code change", "파일을 수정했습니다", OutcomeCodeChange},
		{"not conflict - general", "안녕하세요, 도움이 필요하시면 말씀하세요.", OutcomeGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AnalyzeReply(tt.text)
			if got != tt.want {
				t.Errorf("AnalyzeReply(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestContextButtons_MergeConflict(t *testing.T) {
	buttons := ContextButtons(OutcomeMergeConflict, "discord:123")
	if len(buttons) != 1 {
		t.Fatalf("expected 1 action row, got %d", len(buttons))
	}
	if len(buttons[0].Components) != 3 {
		t.Fatalf("expected 3 buttons (mergefix, mergedetail, mergeabort), got %d", len(buttons[0].Components))
	}
}

func TestTranslateErrorToKorean_MergeConflict(t *testing.T) {
	tests := []struct {
		input string
		want  bool // true if translation should be non-empty
	}{
		{"CONFLICT: Merge conflict in file.go", true},
		{"Unmerged paths: need to resolve", true},
		{"Automatic merge failed", true},
		{"everything is fine", false},
	}

	for _, tt := range tests {
		result := TranslateErrorToKorean(tt.input)
		if tt.want && result == "" {
			t.Errorf("expected translation for %q, got empty", tt.input)
		}
		if !tt.want && result != "" {
			t.Errorf("expected no translation for %q, got %q", tt.input, result)
		}
	}
}
