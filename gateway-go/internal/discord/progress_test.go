package discord

import (
	"context"
	"strings"
	"testing"
)

func TestProgressStep_StatusTransitions(t *testing.T) {
	steps := []ProgressStep{
		{Name: "read", Status: StepPending},
		{Name: "edit", Status: StepRunning},
		{Name: "test", Status: StepDone},
		{Name: "fail", Status: StepError},
	}

	e := FormatProgressEmbed(steps)

	if e.Color != ColorProgress {
		t.Errorf("expected progress color %#x, got %#x", ColorProgress, e.Color)
	}

	for _, s := range steps {
		if !strings.Contains(e.Description, s.Name) {
			t.Errorf("expected description to contain step %q", s.Name)
		}
	}
}

func TestProgressStep_ReasonDisplayed(t *testing.T) {
	steps := []ProgressStep{
		{Name: "파일 읽기", Reason: "설정 파일을 확인하고 있습니다", Status: StepRunning},
		{Name: "명령어 실행", Status: StepPending},
	}

	e := FormatProgressEmbed(steps)

	if !strings.Contains(e.Description, "— 설정 파일을 확인하고 있습니다") {
		t.Errorf("expected description to contain reason, got: %s", e.Description)
	}
	// Step without reason should not have em dash.
	lines := strings.Split(e.Description, "\n")
	if len(lines) < 2 {
		t.Fatal("expected at least 2 lines")
	}
	if strings.Contains(lines[1], "—") {
		t.Errorf("step without reason should not have em dash, got: %s", lines[1])
	}
}

func TestProgressStep_AllDoneWithErrors(t *testing.T) {
	// Progress embed does not judge success/failure — always "완료" when all steps finish.
	steps := []ProgressStep{
		{Name: "step1", Status: StepDone},
		{Name: "step2", Status: StepError},
	}
	e := FormatProgressEmbed(steps)
	if e.Color != ColorSuccess {
		t.Errorf("expected success color when all steps finished, got %#x", e.Color)
	}
	if !strings.Contains(e.Title, "완료") {
		t.Errorf("expected 완료 title, got %q", e.Title)
	}
}

func TestProgressStep_ParallelGroup(t *testing.T) {
	// Steps in the same group should get the ┃ prefix.
	steps := []ProgressStep{
		{Name: "프로젝트 구조", Status: StepDone, Group: 0},
		{Name: "파일 읽기", Status: StepRunning, Group: 1},
		{Name: "코드 검색", Status: StepRunning, Group: 1},
		{Name: "파일 찾기", Status: StepRunning, Group: 1},
		{Name: "파일 수정", Status: StepPending, Group: 0},
	}
	e := FormatProgressEmbed(steps)

	// Parallel group members should have ┃ prefix.
	if !strings.Contains(e.Description, "┃ 🔄 파일 읽기") {
		t.Error("expected parallel prefix for grouped step '파일 읽기'")
	}
	if !strings.Contains(e.Description, "┃ 🔄 코드 검색") {
		t.Error("expected parallel prefix for grouped step '코드 검색'")
	}

	// Non-grouped steps should NOT have ┃ prefix.
	if strings.Contains(e.Description, "┃ ✅ 프로젝트 구조") {
		t.Error("ungrouped step should not have parallel prefix")
	}
	if strings.Contains(e.Description, "┃ ⬜ 파일 수정") {
		t.Error("ungrouped step should not have parallel prefix")
	}
}

func TestProgressStep_SingleMemberGroupNoPrefix(t *testing.T) {
	// A group with only one member should render without ┃ prefix.
	steps := []ProgressStep{
		{Name: "파일 읽기", Status: StepDone, Group: 1},
		{Name: "파일 수정", Status: StepRunning, Group: 2},
	}
	e := FormatProgressEmbed(steps)
	if strings.Contains(e.Description, "┃") {
		t.Error("single-member groups should not have parallel prefix")
	}
}

func TestProgressTracker_NilSafe(t *testing.T) {
	// All methods should be safe to call on nil tracker.
	var pt *ProgressTracker
	ctx := context.Background()
	pt.AddStep("test")
	pt.StartStep(ctx, "test", "")
	pt.CompleteStep(ctx, "test", false)
	pt.Finalize(ctx)
	if pt.MessageID() != "" {
		t.Error("nil tracker should return empty message ID")
	}
}
