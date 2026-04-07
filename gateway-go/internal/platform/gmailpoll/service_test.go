package gmailpoll

import (
	"testing"
	"time"
)

func TestNewService_Defaults(t *testing.T) {
	svc := NewService(Config{StateDir: t.TempDir()}, nil)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.cfg.IntervalMin != defaultIntervalMin {
		t.Errorf("IntervalMin = %d, want %d", svc.cfg.IntervalMin, defaultIntervalMin)
	}
	if svc.cfg.Query != defaultQuery {
		t.Errorf("Query = %q, want %q", svc.cfg.Query, defaultQuery)
	}
	if svc.cfg.MaxPerCycle != defaultMaxPerCycle {
		t.Errorf("MaxPerCycle = %d, want %d", svc.cfg.MaxPerCycle, defaultMaxPerCycle)
	}
	if svc.cfg.Model != defaultModel {
		t.Errorf("Model = %q, want %q", svc.cfg.Model, defaultModel)
	}
	if svc.cfg.PromptFile != defaultPromptFile {
		t.Errorf("PromptFile = %q, want %q", svc.cfg.PromptFile, defaultPromptFile)
	}
}

func TestNewService_CustomConfig(t *testing.T) {
	svc := NewService(Config{
		IntervalMin: 15,
		Query:       "is:unread label:important",
		MaxPerCycle: 10,
		Model:       "custom-model",
		PromptFile:  "/tmp/prompt.md",
		StateDir:    t.TempDir(),
	}, nil)

	if svc.cfg.IntervalMin != 15 {
		t.Errorf("IntervalMin = %d, want 15", svc.cfg.IntervalMin)
	}
	if svc.cfg.Query != "is:unread label:important" {
		t.Errorf("Query = %q", svc.cfg.Query)
	}
	if svc.cfg.MaxPerCycle != 10 {
		t.Errorf("MaxPerCycle = %d, want 10", svc.cfg.MaxPerCycle)
	}
}

func TestService_PeriodicTaskInterface(t *testing.T) {
	svc := NewService(Config{IntervalMin: 15, StateDir: t.TempDir()}, nil)

	if svc.Name() != "gmailpoll" {
		t.Errorf("Name() = %q, want gmailpoll", svc.Name())
	}
	if svc.Interval() != 15*time.Minute {
		t.Errorf("Interval() = %v, want 15m", svc.Interval())
	}
}

func TestService_DefaultInterval(t *testing.T) {
	svc := NewService(Config{StateDir: t.TempDir()}, nil)
	if svc.Interval() != 30*time.Minute {
		t.Errorf("Interval() = %v, want 30m", svc.Interval())
	}
}
