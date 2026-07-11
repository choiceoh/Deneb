package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolRead_ReadsSkillFromCatalogRoot(t *testing.T) {
	ws := t.TempDir()
	catalog := t.TempDir()
	skillDir := filepath.Join(catalog, "email-analysis")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Email Analysis Skill\nprocedure body"
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	read := ToolRead(ws, catalog)
	input, _ := json.Marshal(map[string]any{"file_path": skillPath})
	out, err := read(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("read on catalog skill failed: %v", err)
	}
	if !strings.Contains(out, "Email Analysis Skill") {
		t.Errorf("expected skill body, got: %s", out)
	}

	readNoRoot := ToolRead(ws)
	out, err = readNoRoot(context.Background(), json.RawMessage(input))
	if err == nil && strings.Contains(out, "Email Analysis Skill") {
		t.Errorf("read without catalog root should not reach the skill body")
	}
}
