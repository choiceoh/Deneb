package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runHeartbeatUpdate(t *testing.T, homeDir, content string) (string, error) {
	t.Helper()
	fn := toolHeartbeatUpdateWithHome(homeDir)
	input, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	return fn(context.Background(), input)
}

func TestHeartbeatUpdate_writesContent(t *testing.T) {
	home := t.TempDir()
	out, err := runHeartbeatUpdate(t, home, "task A\ntask B\n")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out, "updated") {
		t.Errorf("expected 'updated' in result, got: %q", out)
	}
	got, err := os.ReadFile(filepath.Join(home, ".deneb", "HEARTBEAT.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "task A\ntask B\n" {
		t.Errorf("file content mismatch: %q", string(got))
	}
}

func TestHeartbeatUpdate_clearWithEmptyContent(t *testing.T) {
	home := t.TempDir()
	if _, err := runHeartbeatUpdate(t, home, "task A"); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	out, err := runHeartbeatUpdate(t, home, "")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !strings.Contains(out, "cleared") {
		t.Errorf("expected 'cleared' in result, got: %q", out)
	}
	got, err := os.ReadFile(filepath.Join(home, ".deneb", "HEARTBEAT.md"))
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
	home := t.TempDir()
	if _, err := runHeartbeatUpdate(t, home, "first version"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := runHeartbeatUpdate(t, home, "second version"); err != nil {
		t.Fatalf("second write: %v", err)
	}

	prev, err := os.ReadFile(filepath.Join(home, ".deneb", "HEARTBEAT.md.prev"))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(prev) != "first version" {
		t.Errorf("backup should hold prior content; got: %q", string(prev))
	}
	cur, _ := os.ReadFile(filepath.Join(home, ".deneb", "HEARTBEAT.md"))
	if string(cur) != "second version" {
		t.Errorf("current should hold new content; got: %q", string(cur))
	}
}

func TestHeartbeatUpdate_clearStillBacksUp(t *testing.T) {
	home := t.TempDir()
	if _, err := runHeartbeatUpdate(t, home, "important task"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := runHeartbeatUpdate(t, home, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}

	prev, err := os.ReadFile(filepath.Join(home, ".deneb", "HEARTBEAT.md.prev"))
	if err != nil {
		t.Fatalf("backup must exist after clear so user can recover: %v", err)
	}
	if string(prev) != "important task" {
		t.Errorf("backup mismatch: %q", string(prev))
	}
}

func TestHeartbeatUpdate_firstWriteHasNoBackup(t *testing.T) {
	home := t.TempDir()
	if _, err := runHeartbeatUpdate(t, home, "first"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".deneb", "HEARTBEAT.md.prev")); !os.IsNotExist(err) {
		t.Errorf("backup must not be created on first write (no prior content); err: %v", err)
	}
}

func TestHeartbeatUpdate_createsDirIfMissing(t *testing.T) {
	home := t.TempDir()
	// No ~/.deneb pre-created
	if _, err := runHeartbeatUpdate(t, home, "x"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".deneb")); err != nil {
		t.Errorf(".deneb dir should have been created: %v", err)
	}
}

func TestHeartbeatUpdate_rejectsBadInput(t *testing.T) {
	fn := toolHeartbeatUpdateWithHome(t.TempDir())
	_, err := fn(context.Background(), json.RawMessage(`{"content": 123}`))
	if err == nil {
		t.Error("expected error for non-string content")
	}
}
