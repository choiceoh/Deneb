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
