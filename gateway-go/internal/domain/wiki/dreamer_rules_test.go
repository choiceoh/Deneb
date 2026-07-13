package wiki

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func newRulesDreamer(t *testing.T, workspace string) *WikiDreamer {
	t.Helper()
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { store.Close() })
	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	wd.SetWorkspaceDir(workspace)
	return wd
}

func TestLoadWikiSynthesisRules_FallsBackToDefault(t *testing.T) {
	// No workspace dir set → default.
	if got := newRulesDreamer(t, "").loadWikiSynthesisRules(); got != defaultWikiSynthesisRules {
		t.Error("empty workspace should yield the built-in default rules")
	}
	// Workspace set but no override file present → default.
	if got := newRulesDreamer(t, t.TempDir()).loadWikiSynthesisRules(); got != defaultWikiSynthesisRules {
		t.Error("missing override file should yield the built-in default rules")
	}
}

func TestLoadWikiSynthesisRules_OverrideWins(t *testing.T) {
	ws := t.TempDir()
	override := "## 규칙\n- 실험 규칙 하나만"
	if err := os.WriteFile(filepath.Join(ws, wikiDreamRulesFile), []byte(override), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}
	wd := newRulesDreamer(t, ws)
	if got := wd.loadWikiSynthesisRules(); got != override {
		t.Errorf("override not honored: got %q", got)
	}
	// A blank override must not blank the rules — synthesis needs a policy.
	if err := os.WriteFile(filepath.Join(ws, wikiDreamRulesFile), []byte("   \n"), 0o644); err != nil {
		t.Fatalf("write blank override: %v", err)
	}
	if got := wd.loadWikiSynthesisRules(); got != defaultWikiSynthesisRules {
		t.Error("blank override should fall back to default, not empty rules")
	}
}
