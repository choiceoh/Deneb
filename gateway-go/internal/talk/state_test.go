package talk

import (
	"testing"
)

func TestSetMode(t *testing.T) {
	s := NewState()

	result := s.SetMode(true, "listening")
	if !result.Enabled {
		t.Fatal("expected enabled=true")
	}
	if result.Phase != "listening" {
		t.Fatalf("expected 'listening', got %q", result.Phase)
	}
	if result.Ts == 0 {
		t.Fatal("expected non-zero ts")
	}

	// Disable.
	result = s.SetMode(false, "")
	if result.Enabled {
		t.Fatal("expected enabled=false")
	}
	if result.Phase != "" {
		t.Fatalf("expected empty phase, got %q", result.Phase)
	}
}

func TestGetConfig(t *testing.T) {
	s := NewState()
	s.SetConfig(Config{
		Session: &SessionSettings{MainKey: "main"},
		UI:      &UISettings{SeamColor: "#FF0000"},
	})
	s.SetMode(true, "idle")

	cfg := s.GetConfig(false)
	if cfg.Talk == nil {
		t.Fatal("expected talk config")
	}
	if !cfg.Talk.Enabled {
		t.Fatal("expected enabled=true in config")
	}
	if cfg.Session.MainKey != "main" {
		t.Fatalf("expected 'main', got %q", cfg.Session.MainKey)
	}
}

func TestDefaultConfig(t *testing.T) {
	s := NewState()
	cfg := s.GetConfig(false)
	if cfg.Talk == nil {
		t.Fatal("expected non-nil talk")
	}
	if cfg.Talk.Enabled {
		t.Fatal("expected enabled=false by default")
	}
}
