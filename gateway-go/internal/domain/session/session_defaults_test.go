package session

import "testing"

// TestSessionDefaults_AppliedOnCreate verifies new sessions inherit
// the operator-configured thinking defaults.
func TestSessionDefaults_AppliedOnCreate(t *testing.T) {
	m := NewManager()
	on := true
	m.SetSessionDefaults(SessionDefaults{
		ThinkingLevel:       "medium",
		InterleavedThinking: &on,
	})

	s := m.Create("telegram:1", KindDirect)
	if s == nil {
		t.Fatal("Create returned nil")
	}
	if s.ThinkingLevel != "medium" {
		t.Errorf("ThinkingLevel = %q, want medium", s.ThinkingLevel)
	}
	if s.InterleavedThinking == nil || !*s.InterleavedThinking {
		t.Errorf("InterleavedThinking = %v, want *true", s.InterleavedThinking)
	}

	// Mutating the original defaults pointer must not bleed into the session.
	on = false
	if s.InterleavedThinking == nil || !*s.InterleavedThinking {
		t.Errorf("InterleavedThinking changed after caller mutated source pointer: %v", s.InterleavedThinking)
	}
}

// TestSessionDefaults_AppliedOnPatchCreate verifies sessions implicitly
// created by Patch (when the key doesn't exist yet) also inherit defaults.
func TestSessionDefaults_AppliedOnPatchCreate(t *testing.T) {
	m := NewManager()
	off := false
	m.SetSessionDefaults(SessionDefaults{
		ThinkingLevel:       "low",
		InterleavedThinking: &off,
	})

	// Patch with an unrelated field; the session is created on the fly.
	model := "zai/glm-5.1"
	s := m.Patch("cron:foo", PatchFields{Model: &model})
	if s == nil {
		t.Fatal("Patch returned nil")
	}
	if s.ThinkingLevel != "low" {
		t.Errorf("ThinkingLevel = %q, want low", s.ThinkingLevel)
	}
	if s.InterleavedThinking == nil || *s.InterleavedThinking {
		t.Errorf("InterleavedThinking = %v, want *false", s.InterleavedThinking)
	}
}

// TestSessionDefaults_ZeroValueIsNoop verifies that a Manager with no
// defaults installed creates sessions with zero ModelConfig — preserving
// the prior behavior for operators who don't opt in.
func TestSessionDefaults_ZeroValueIsNoop(t *testing.T) {
	m := NewManager()
	s := m.Create("telegram:2", KindDirect)
	if s == nil {
		t.Fatal("Create returned nil")
	}
	if s.ThinkingLevel != "" {
		t.Errorf("ThinkingLevel = %q, want empty", s.ThinkingLevel)
	}
	if s.InterleavedThinking != nil {
		t.Errorf("InterleavedThinking = %v, want nil", s.InterleavedThinking)
	}
}

// TestSessionDefaults_GetterReturnsCopy ensures the getter does not leak
// the internal pointer for InterleavedThinking.
func TestSessionDefaults_GetterReturnsCopy(t *testing.T) {
	m := NewManager()
	on := true
	m.SetSessionDefaults(SessionDefaults{InterleavedThinking: &on})

	got := m.SessionDefaults()
	if got.InterleavedThinking == nil || !*got.InterleavedThinking {
		t.Fatalf("getter returned %v", got.InterleavedThinking)
	}
}
