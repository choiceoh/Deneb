package agent

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestCreateAndListAgents(t *testing.T) {
	s := NewStore()

	a := s.Create(CreateParams{Name: "Test Agent", Model: "gpt-4"})
	if a.AgentID == "" {
		t.Fatal("expected non-empty agentId")
	}
	if a.Name != "Test Agent" {
		t.Fatalf("expected 'Test Agent', got %q", a.Name)
	}

	agents := s.List()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
}

func TestCreateWithCustomID(t *testing.T) {
	s := NewStore()
	a := s.Create(CreateParams{AgentID: "my-agent", Name: "Custom"})
	if a.AgentID != "my-agent" {
		t.Fatalf("expected 'my-agent', got %q", a.AgentID)
	}
}

func TestGetAgent(t *testing.T) {
	s := NewStore()
	created := s.Create(CreateParams{Name: "Finder"})

	got := s.Get(created.AgentID)
	if got == nil {
		t.Fatal("expected agent, got nil")
	}
	if got.Name != "Finder" {
		t.Fatalf("expected 'Finder', got %q", got.Name)
	}

	if s.Get("nonexistent") != nil {
		t.Fatal("expected nil for unknown agent")
	}
}

func TestUpdateAgent(t *testing.T) {
	s := NewStore()
	a := s.Create(CreateParams{Name: "Original"})

	updated, err := s.Update(a.AgentID, map[string]any{
		"name":  "Updated",
		"model": "claude-3",
	})
	testutil.NoError(t, err)
	if updated.Name != "Updated" {
		t.Fatalf("expected 'Updated', got %q", updated.Name)
	}
	if updated.Model != "claude-3" {
		t.Fatalf("expected 'claude-3', got %q", updated.Model)
	}
}

func TestUpdateNotFound(t *testing.T) {
	s := NewStore()
	_, err := s.Update("nonexistent", map[string]any{"name": "x"})
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestDeleteAgent(t *testing.T) {
	s := NewStore()
	a := s.Create(CreateParams{Name: "Deletable"})

	if !s.Delete(a.AgentID) {
		t.Fatal("expected delete to return true")
	}
	if s.Delete(a.AgentID) {
		t.Fatal("expected second delete to return false")
	}
	if s.Get(a.AgentID) != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestFileOperations(t *testing.T) {
	s := NewStore()
	a := s.Create(CreateParams{Name: "File Agent"})

	// Set file.
	f := testutil.Must(s.SetFile(a.AgentID, "config.json", "eyJ0ZXN0IjogdHJ1ZX0="))
	if f.Name != "config.json" {
		t.Fatalf("expected 'config.json', got %q", f.Name)
	}

	// List files.
	files := testutil.Must(s.ListFiles(a.AgentID))
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	// List should not include content.
	if files[0].ContentBase64 != "" {
		t.Fatal("list should not include content")
	}

	// Get file (includes content).
	got := testutil.Must(s.File(a.AgentID, "config.json"))
	if got.ContentBase64 != "eyJ0ZXN0IjogdHJ1ZX0=" {
		t.Fatalf("unexpected content")
	}

	// Get unknown file.
	_, err := s.File(a.AgentID, "unknown.txt")
	if err == nil {
		t.Fatal("expected error for unknown file")
	}
}

func TestFileOpsUnknownAgent(t *testing.T) {
	s := NewStore()

	if _, err := s.ListFiles("unknown"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := s.File("unknown", "f"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := s.SetFile("unknown", "f", ""); err == nil {
		t.Fatal("expected error")
	}
}
