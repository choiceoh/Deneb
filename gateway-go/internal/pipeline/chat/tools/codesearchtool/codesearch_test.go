package codesearchtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolCodeSearchRequiresQuery(t *testing.T) {
	out, err := ToolCodeSearch(t.TempDir())(context.Background(), json.RawMessage(`{"query":" "}`))
	if err == nil {
		t.Fatalf("expected missing query error, got output %q", out)
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolCodeSearchMissingIndexGivesGuidance(t *testing.T) {
	t.Setenv("DENEB_CODESEARCH_DIR", "")

	out, err := ToolCodeSearch(t.TempDir())(context.Background(), json.RawMessage(`{"query":"session state"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "시맨틱 코드 인덱스") {
		t.Fatalf("expected missing-index guidance, got %q", out)
	}
}

func TestResolveCodeIndexDirPrefersEnvSemanticIndex(t *testing.T) {
	workspace := t.TempDir()
	indexDir := filepath.Join(t.TempDir(), ".codegraph")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "semantic-code.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DENEB_CODESEARCH_DIR", indexDir)

	if got := resolveCodeIndexDir(workspace); got != indexDir {
		t.Fatalf("resolveCodeIndexDir() = %q, want %q", got, indexDir)
	}
}
