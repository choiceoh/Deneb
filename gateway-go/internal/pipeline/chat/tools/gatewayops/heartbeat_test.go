package gatewayops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runHeartbeatUpdate(t *testing.T, stateDir, content string) (string, error) {
	t.Helper()
	fn := toolHeartbeatUpdateInDir(stateDir)
	input, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	return fn(context.Background(), input)
}

func TestHeartbeatUpdate_writesContent(t *testing.T) {
	stateDir := t.TempDir()
	out, err := runHeartbeatUpdate(t, stateDir, "task A\ntask B\n")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out, "updated") {
		t.Errorf("expected 'updated' in result, got: %q", out)
	}
	got, err := os.ReadFile(filepath.Join(stateDir, "HEARTBEAT.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "task A\ntask B\n" {
		t.Errorf("file content mismatch: %q", string(got))
	}
}

func TestHeartbeatUpdate_clearWithEmptyContent(t *testing.T) {
	stateDir := t.TempDir()
	if _, err := runHeartbeatUpdate(t, stateDir, "task A"); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	out, err := runHeartbeatUpdate(t, stateDir, "")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !strings.Contains(out, "cleared") {
		t.Errorf("expected 'cleared' in result, got: %q", out)
	}
	got, err := os.ReadFile(filepath.Join(stateDir, "HEARTBEAT.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "" {
		t.Errorf("expected empty file, got: %q", string(got))
	}
}

// Backup is the safety net for "agent accidentally clears HEARTBEAT.md" — the
// most likely failure mode given the autonomous heartbeat is doing the writes.
func TestHeartbeatUpdate_backsUpPriorContent(t *testing.T) {
	stateDir := t.TempDir()
	if _, err := runHeartbeatUpdate(t, stateDir, "first version"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := runHeartbeatUpdate(t, stateDir, "second version"); err != nil {
		t.Fatalf("second write: %v", err)
	}

	prev, err := os.ReadFile(filepath.Join(stateDir, "HEARTBEAT.md.prev"))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(prev) != "first version" {
		t.Errorf("backup should hold prior content; got: %q", string(prev))
	}
	cur, _ := os.ReadFile(filepath.Join(stateDir, "HEARTBEAT.md"))
	if string(cur) != "second version" {
		t.Errorf("current should hold new content; got: %q", string(cur))
	}
}

func TestHeartbeatUpdate_clearStillBacksUp(t *testing.T) {
	stateDir := t.TempDir()
	if _, err := runHeartbeatUpdate(t, stateDir, "important task"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := runHeartbeatUpdate(t, stateDir, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}

	prev, err := os.ReadFile(filepath.Join(stateDir, "HEARTBEAT.md.prev"))
	if err != nil {
		t.Fatalf("backup must exist after clear so user can recover: %v", err)
	}
	if string(prev) != "important task" {
		t.Errorf("backup mismatch: %q", string(prev))
	}
}

func TestHeartbeatUpdate_firstWriteHasNoBackup(t *testing.T) {
	stateDir := t.TempDir()
	if _, err := runHeartbeatUpdate(t, stateDir, "first"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "HEARTBEAT.md.prev")); !os.IsNotExist(err) {
		t.Errorf("backup must not be created on first write (no prior content); err: %v", err)
	}
}

func TestHeartbeatUpdate_createsDirIfMissing(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state") // not pre-created
	if _, err := runHeartbeatUpdate(t, stateDir, "x"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "HEARTBEAT.md")); err != nil {
		t.Errorf("state dir should have been created with the file: %v", err)
	}
}

func TestHeartbeatUpdate_rejectsBadInput(t *testing.T) {
	fn := toolHeartbeatUpdateInDir(t.TempDir())
	_, err := fn(context.Background(), json.RawMessage(`{"content": 123}`))
	if err == nil {
		t.Error("expected error for non-string content")
	}
}

// The production entry resolves the STATE dir per call. A dev gateway keeps the
// real $HOME but runs under DENEB_STATE_DIR=/tmp/…, and its heartbeat turns
// used to rewrite the operator's HEARTBEAT.md through this tool.
func TestHeartbeatUpdateWritesUnderTheStateDirNotHome(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DENEB_STATE_DIR", stateDir)

	fn := ToolHeartbeatUpdate()
	if _, err := fn(context.Background(), json.RawMessage(`{"content":"dev task"}`)); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(stateDir, "HEARTBEAT.md")); err != nil || string(got) != "dev task" {
		t.Fatalf("state-dir HEARTBEAT.md = %q, %v; want the dev write", got, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".deneb")); !os.IsNotExist(err) {
		t.Fatalf("$HOME/.deneb was touched (stat err = %v)", err)
	}

	// Production pins DENEB_STATE_DIR=$HOME/.deneb, and unset defaults there:
	// the file the operator edits does not move.
	t.Setenv("DENEB_STATE_DIR", "")
	if _, err := fn(context.Background(), json.RawMessage(`{"content":"prod task"}`)); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(home, ".deneb", "HEARTBEAT.md")); err != nil || string(got) != "prod task" {
		t.Fatalf("production HEARTBEAT.md = %q, %v", got, err)
	}
}
