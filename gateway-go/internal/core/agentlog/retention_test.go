package agentlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The retention contract: only .jsonl session files past the age bound go;
// recent files, directories, and foreign files stay.
func TestPruneStaleFilesRemovesOnlyOldSessionLogs(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	old := now.Add(-retentionMaxAge - 24*time.Hour)
	fresh := now.Add(-24 * time.Hour)

	mk := func(name string, mtime time.Time) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	mk("dead-session.jsonl", old)
	mk("live-session.jsonl", fresh)
	mk("not-a-log.txt", old) // foreign file — never touched
	if err := os.Mkdir(filepath.Join(dir, "subdir.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}

	if removed := w.PruneStaleFiles(now); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	for name, wantGone := range map[string]bool{
		"dead-session.jsonl": true,
		"live-session.jsonl": false,
		"not-a-log.txt":      false,
		"subdir.jsonl":       false,
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		if gone := os.IsNotExist(err); gone != wantGone {
			t.Errorf("%s: gone=%v want %v", name, gone, wantGone)
		}
	}
}

// The bound must exceed every consumer's lookback with headroom — the widest
// is miniapp.usage.stats' 31-day clamp.
func TestRetentionExceedsWidestConsumerWindow(t *testing.T) {
	if retentionMaxAge < 4*31*24*time.Hour {
		t.Fatalf("retentionMaxAge %v < 4× the 31d usage window", retentionMaxAge)
	}
}
