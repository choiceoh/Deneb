package runtimeops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// runPwd executes the exec tool with the given context and returns its output.
func runPwd(ctx context.Context, t *testing.T, defaultDir string) string {
	t.Helper()
	fn := ToolExec(nil, defaultDir)
	in, err := json.Marshal(map[string]any{"command": "pwd"})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	out, err := fn(ctx, in)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	return out
}

// A run that pins a workspace must actually run its commands there. Tools are
// registered once at handler construction, so before this the request-scoped
// workspace could steer the prompt but never the commands.
func TestExecPrefersRunScopedWorkspaceOverRegisteredDefault(t *testing.T) {
	registered := t.TempDir()
	pinned, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve pinned dir: %v", err)
	}

	out := runPwd(toolport.WithWorkspaceDir(context.Background(), pinned), t, registered)
	if !strings.Contains(out, pinned) {
		t.Errorf("command ran outside the run's workspace:\nwant %q in\n%s", pinned, out)
	}
}

// The safety property: with no workspace pinned, exec keeps using the directory
// it was registered with. Nobody sends one today, so this is what guarantees the
// change is inert until a caller opts in.
func TestExecKeepsRegisteredDefaultWhenRunPinsNothing(t *testing.T) {
	registered, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve registered dir: %v", err)
	}

	out := runPwd(context.Background(), t, registered)
	if !strings.Contains(out, registered) {
		t.Errorf("command left the registered workspace:\nwant %q in\n%s", registered, out)
	}
}

// An explicit per-call workdir still wins over the run's workspace — the model
// asked for a specific directory and the run default must not override it.
func TestExecPerCallWorkdirOutranksRunWorkspace(t *testing.T) {
	pinned := t.TempDir()
	explicit, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve explicit dir: %v", err)
	}

	fn := ToolExec(nil, t.TempDir())
	in, err := json.Marshal(map[string]any{"command": "pwd", "workdir": explicit})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	out, err := fn(toolport.WithWorkspaceDir(context.Background(), pinned), in)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(out, explicit) {
		t.Errorf("per-call workdir was overridden:\nwant %q in\n%s", explicit, out)
	}
}

// A pinned workspace that does not exist must fail loudly rather than silently
// falling back to the server-wide default — running the wrong tree is worse
// than refusing.
func TestExecRejectsPinnedWorkspaceThatDoesNotExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("fixture should not exist")
	}

	fn := ToolExec(nil, t.TempDir())
	in, err := json.Marshal(map[string]any{"command": "pwd"})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	if _, err := fn(toolport.WithWorkspaceDir(context.Background(), missing), in); err == nil {
		t.Error("expected an error for a missing pinned workspace, got none")
	}
}
