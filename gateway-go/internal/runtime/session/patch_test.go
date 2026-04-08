package session

import (
	"testing"
)




func TestSessionApplyPatch(t *testing.T) {
	t.Run("empty patch no change", func(t *testing.T) {
		s := &Session{Key: "k1", Label: "orig"}
		if s.ApplyPatch(PatchFields{}) {
			t.Error("expected false for empty patch")
		}
		if s.Label != "orig" {
			t.Errorf("label = %q, want %q", s.Label, "orig")
		}
	})

	t.Run("single field patch", func(t *testing.T) {
		s := &Session{Key: "k1", Label: "old"}
		newLabel := "new-label"
		if !s.ApplyPatch(PatchFields{Label: &newLabel}) {
			t.Error("expected true for changed field")
		}
		if s.Label != "new-label" {
			t.Errorf("label = %q, want %q", s.Label, "new-label")
		}
		if s.UpdatedAt == 0 {
			t.Error("UpdatedAt should be set on change")
		}
	})

	t.Run("multiple fields patch", func(t *testing.T) {
		s := &Session{Key: "k1"}
		label := "my-session"
		model := "claude-3"
		thinking := "high"
		if !s.ApplyPatch(PatchFields{
			Label:         &label,
			Model:         &model,
			ThinkingLevel: &thinking,
		}) {
			t.Error("expected true for changed fields")
		}
		if s.Label != "my-session" {
			t.Errorf("label = %q, want %q", s.Label, "my-session")
		}
		if s.Model != "claude-3" {
			t.Errorf("model = %q, want %q", s.Model, "claude-3")
		}
		if s.ThinkingLevel != "high" {
			t.Errorf("thinkingLevel = %q, want %q", s.ThinkingLevel, "high")
		}
	})

	t.Run("unchanged fields not modified", func(t *testing.T) {
		s := &Session{Key: "k1", Label: "keep", Model: "keep-model"}
		label := "keep" // same as existing
		newModel := "new-model"
		s.ApplyPatch(PatchFields{Label: &label, Model: &newModel})
		if s.Label != "keep" {
			t.Errorf("label should remain %q", "keep")
		}
		if s.Model != "new-model" {
			t.Errorf("model = %q, want %q", s.Model, "new-model")
		}
	})
}

func TestManagerPatch(t *testing.T) {
	t.Run("patch existing session", func(t *testing.T) {
		m := NewManager()
		m.Create("s1", KindDirect)
		label := "patched"
		snap := m.Patch("s1", PatchFields{Label: &label})
		if snap.Label != "patched" {
			t.Errorf("label = %q, want %q", snap.Label, "patched")
		}
		// Verify stored session is also updated.
		got := m.Get("s1")
		if got.Label != "patched" {
			t.Errorf("stored label = %q, want %q", got.Label, "patched")
		}
	})

	t.Run("auto-creates for missing key", func(t *testing.T) {
		m := NewManager()
		label := "auto-created"
		snap := m.Patch("new-key", PatchFields{Label: &label})
		if snap.Key != "new-key" {
			t.Errorf("key = %q, want %q", snap.Key, "new-key")
		}
		if snap.Label != "auto-created" {
			t.Errorf("label = %q, want %q", snap.Label, "auto-created")
		}
		if snap.Kind != KindUnknown {
			t.Errorf("kind = %q, want %q", snap.Kind, KindUnknown)
		}
	})
}

func TestManagerResetSession(t *testing.T) {
	t.Run("resets runtime fields", func(t *testing.T) {
		m := NewManager()
		started := int64(1000)
		ended := int64(2000)
		runtime := int64(1000)
		input := int64(500)
		output := int64(300)
		total := int64(800)
		m.Set(&Session{
			Key:          "s1",
			Kind:         KindDirect,
			Status:       StatusDone,
			StartedAt:    &started,
			EndedAt:      &ended,
			RuntimeMs:    &runtime,
			InputTokens:  &input,
			OutputTokens: &output,
			TotalTokens:  &total,
		})

		snap := m.ResetSession("s1")
		if snap == nil {
			t.Fatal("expected non-nil snapshot")
		}
		if snap.Status != "" {
			t.Errorf("status = %q, want empty", snap.Status)
		}
		if snap.StartedAt != nil {
			t.Error("StartedAt should be nil")
		}
		if snap.EndedAt != nil {
			t.Error("EndedAt should be nil")
		}
		if snap.RuntimeMs != nil {
			t.Error("RuntimeMs should be nil")
		}
		if snap.InputTokens != nil {
			t.Error("InputTokens should be nil")
		}
		if snap.OutputTokens != nil {
			t.Error("OutputTokens should be nil")
		}
		if snap.TotalTokens != nil {
			t.Error("TotalTokens should be nil")
		}
	})

	t.Run("returns nil for unknown key", func(t *testing.T) {
		m := NewManager()
		if m.ResetSession("nonexistent") != nil {
			t.Error("expected nil for unknown key")
		}
	})
}

func TestManagerFindBySessionID(t *testing.T) {
	m := NewManager()
	m.Set(&Session{Key: "k1", Kind: KindDirect, SessionID: "sid-abc"})
	m.Set(&Session{Key: "k2", Kind: KindDirect, SessionID: "sid-def"})

	t.Run("found", func(t *testing.T) {
		s := m.FindBySessionID("sid-abc")
		if s == nil {
			t.Fatal("expected to find session")
		}
		if s.Key != "k1" {
			t.Errorf("key = %q, want %q", s.Key, "k1")
		}
	})

	t.Run("not found", func(t *testing.T) {
		if m.FindBySessionID("nonexistent") != nil {
			t.Error("expected nil for unknown session ID")
		}
	})
}

func TestManagerFindByLabel(t *testing.T) {
	m := NewManager()
	m.Set(&Session{Key: "k1", Kind: KindDirect, Label: "test"})
	m.Set(&Session{Key: "k2", Kind: KindDirect, Label: "test"})
	m.Set(&Session{Key: "k3", Kind: KindDirect, Label: "other"})

	t.Run("returns all matching", func(t *testing.T) {
		matches := m.FindByLabel("test")
		if len(matches) != 2 {
			t.Errorf("got %d matches, want 2", len(matches))
		}
	})

	t.Run("no matches", func(t *testing.T) {
		matches := m.FindByLabel("nonexistent")
		if len(matches) != 0 {
			t.Errorf("got %d matches, want 0", len(matches))
		}
	})
}

func TestManagerClearTokens(t *testing.T) {
	t.Run("clears token fields", func(t *testing.T) {
		m := NewManager()
		input := int64(100)
		output := int64(200)
		total := int64(300)
		m.Set(&Session{
			Key:          "s1",
			Kind:         KindDirect,
			InputTokens:  &input,
			OutputTokens: &output,
			TotalTokens:  &total,
		})

		m.ClearTokens("s1")

		s := m.Get("s1")
		if s.InputTokens != nil {
			t.Error("InputTokens should be nil")
		}
		if s.OutputTokens != nil {
			t.Error("OutputTokens should be nil")
		}
		if s.TotalTokens != nil {
			t.Error("TotalTokens should be nil")
		}
		if s.UpdatedAt == 0 {
			t.Error("UpdatedAt should be set")
		}
	})

	t.Run("no-op for missing key", func(t *testing.T) {
		m := NewManager()
		// Should not panic.
		m.ClearTokens("nonexistent")
	})
}
