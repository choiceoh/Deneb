package wizard

import (
	"testing"
)

func TestStartAndGetStatus(t *testing.T) {
	e := NewEngine()

	sess := e.Start("onboard", "")
	if sess.SessionID == "" {
		t.Fatal("expected non-empty sessionId")
	}
	if sess.Status != StatusRunning {
		t.Fatalf("expected running, got %s", sess.Status)
	}
	if sess.Mode != "onboard" {
		t.Fatalf("expected 'onboard', got %q", sess.Mode)
	}

	status, err := e.GetStatus(sess.SessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != StatusRunning {
		t.Fatalf("expected running, got %s", status.Status)
	}
}

func TestNext(t *testing.T) {
	e := NewEngine()
	sess := e.Start("setup", "")

	result, err := e.Next(sess.SessionID, &Answer{StepID: "step1", Value: "yes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Done {
		t.Fatal("expected done=true after Next")
	}
	if result.Status != StatusDone {
		t.Fatalf("expected done status, got %s", result.Status)
	}
}

func TestCancel(t *testing.T) {
	e := NewEngine()
	sess := e.Start("setup", "")

	cancelled, err := e.Cancel(sess.SessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancelled.Status != StatusCancelled {
		t.Fatalf("expected cancelled, got %s", cancelled.Status)
	}
}

func TestCancelIdempotent(t *testing.T) {
	e := NewEngine()
	sess := e.Start("setup", "")
	e.Cancel(sess.SessionID)

	// Second cancel should not error.
	result, err := e.Cancel(sess.SessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusCancelled {
		t.Fatalf("expected cancelled, got %s", result.Status)
	}
}

func TestMultiStepWizard(t *testing.T) {
	e := NewEngine()
	steps := []Step{
		{ID: "choose-provider", Prompt: "Select a provider"},
		{ID: "enter-key", Prompt: "Enter API key"},
		{ID: "confirm", Prompt: "Confirm settings"},
	}
	sess := e.StartWithSteps("setup", "", steps)
	if sess.StepID != "choose-provider" {
		t.Fatalf("expected step choose-provider, got %q", sess.StepID)
	}
	if sess.StepIndex != 0 {
		t.Fatalf("expected stepIndex 0, got %d", sess.StepIndex)
	}

	// Step 1 → 2.
	result, err := e.Next(sess.SessionID, &Answer{Value: "anthropic"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Done {
		t.Fatal("expected not done after step 1")
	}
	if result.Status != StatusRunning {
		t.Fatalf("expected running, got %s", result.Status)
	}
	if result.StepID != "enter-key" {
		t.Fatalf("expected step enter-key, got %q", result.StepID)
	}

	// Step 2 → 3 (final).
	result, err = e.Next(sess.SessionID, &Answer{Value: "sk-..."})
	if err != nil {
		t.Fatal(err)
	}
	if result.Done {
		t.Fatal("expected not done after step 2")
	}
	if result.StepID != "confirm" {
		t.Fatalf("expected step confirm, got %q", result.StepID)
	}

	// Step 3 → done.
	result, err = e.Next(sess.SessionID, &Answer{Value: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Done {
		t.Fatal("expected done after final step")
	}
	if result.Status != StatusDone {
		t.Fatalf("expected done status, got %s", result.Status)
	}
}

func TestMultiStepWizardCancel(t *testing.T) {
	e := NewEngine()
	steps := []Step{
		{ID: "step1"},
		{ID: "step2"},
		{ID: "step3"},
	}
	sess := e.StartWithSteps("setup", "", steps)

	// Advance one step.
	e.Next(sess.SessionID, &Answer{Value: "a"})

	// Cancel mid-way.
	result, err := e.Cancel(sess.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCancelled {
		t.Fatalf("expected cancelled, got %s", result.Status)
	}
}

func TestNotFound(t *testing.T) {
	e := NewEngine()

	if _, err := e.GetStatus("nonexistent"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := e.Next("nonexistent", nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := e.Cancel("nonexistent"); err == nil {
		t.Fatal("expected error")
	}
}
