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

func TestProgressStep_AllDoneWithError(t *testing.T) {
	steps := []ProgressStep{
		{Name: "step1", Status: StepDone},
		{Name: "step2", Status: StepError},
	}
	e := FormatProgressEmbed(steps)
	if e.Color != ColorError {
		t.Errorf("expected error color when has error steps, got %#x", e.Color)
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
