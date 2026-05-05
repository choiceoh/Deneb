package chat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadArchivedCuratorSkillNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateDir := filepath.Join(home, ".deneb", "data")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := []byte(`{
  "skills": {
    "active-helper": {"state": "active"},
    "archived-helper": {"state": "archived"}
  }
}`)
	if err := os.WriteFile(filepath.Join(stateDir, "skill_curator_state.json"), data, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	got := loadArchivedCuratorSkillNames()
	if _, ok := got["archived-helper"]; !ok {
		t.Fatalf("archived-helper missing from archived map: %+v", got)
	}
	if _, ok := got["active-helper"]; ok {
		t.Fatalf("active-helper should not be archived: %+v", got)
	}
}
