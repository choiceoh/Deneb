package server

import (
	"strings"
	"testing"
)

func TestParseJudgeVerdict(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantDone bool
		wantOK   bool
	}{
		{"plain done true", `{"done": true, "reason": "delivered"}`, true, true},
		{"plain done false", `{"done": false, "reason": "more work"}`, false, true},
		{"fenced json", "```json\n{\"done\": true, \"reason\": \"x\"}\n```", true, true},
		{"prose around json", `Sure: {"done": false, "reason": "y"} ok.`, false, true},
		{"string bool true", `{"done": "true", "reason": "z"}`, true, true},
		{"string bool false", `{"done": "false"}`, false, true},
		{"garbage no json", `I think it is done.`, false, false},
		{"empty", ``, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, _, ok := parseJudgeVerdict(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && done != tt.wantDone {
				t.Fatalf("done = %v, want %v", done, tt.wantDone)
			}
		})
	}
}

func TestComposeGoalContinuation(t *testing.T) {
	out := composeGoalContinuation("  탑솔라 6월 견적 정리  ", nil)
	if !strings.Contains(out, "탑솔라 6월 견적 정리") {
		t.Fatal("continuation is missing the goal text")
	}
	if !strings.Contains(out, "NO_REPLY") {
		t.Fatal("continuation is missing the NO_REPLY suppression instruction")
	}
	if strings.Contains(out, "  탑솔라") {
		t.Fatal("goal text was not trimmed")
	}

	withSub := composeGoalContinuation("목표", []string{"견적서 PDF 생성", "메일 초안 작성"})
	if !strings.Contains(withSub, "견적서 PDF 생성") || !strings.Contains(withSub, "메일 초안 작성") {
		t.Fatal("continuation is missing subgoal criteria")
	}
	if !strings.Contains(withSub, "추가 완료 기준") {
		t.Fatal("continuation is missing the subgoal criteria header")
	}
}
