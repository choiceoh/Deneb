package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepAutomatedTranscripts_PrefixAllowlist(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-90 * 24 * time.Hour)
	files := map[string]bool{ // name → should be swept
		"cron:weekly-report:1780457749304.jsonl": true,
		"acp:client:main:fork1.jsonl":            true,
		":livetest:1780852962773.jsonl":          true,
		"client:main.jsonl":                      false, // user session — never
		"telegram:7074071666.jsonl":              false, // user history — never
		"system:heartbeat.jsonl":                 false, // bounded system session
		"cron:fresh:999.jsonl":                   false, // automated but recent
		"notes.txt":                              false, // not a transcript
	}
	for name := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(`{"role":"user"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if name != "cron:fresh:999.jsonl" {
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}

	removed := sweepAutomatedTranscripts(dir, 45*24*time.Hour, nil)
	if removed != 3 {
		t.Errorf("removed = %d, want 3", removed)
	}
	for name, swept := range files {
		_, err := os.Stat(filepath.Join(dir, name))
		if swept && !os.IsNotExist(err) {
			t.Errorf("%s should have been swept", name)
		}
		if !swept && err != nil {
			t.Errorf("%s should have survived: %v", name, err)
		}
	}
}

func TestMemorySweepRetention_EnvOverride(t *testing.T) {
	t.Setenv("DENEB_SWEEP_RETENTION_DAYS", "0")
	if got := memorySweepRetention(); got != 0 {
		t.Errorf("0 must disable sweeping, got %v", got)
	}
	t.Setenv("DENEB_SWEEP_RETENTION_DAYS", "10")
	if got := memorySweepRetention(); got != 10*24*time.Hour {
		t.Errorf("got %v, want 240h", got)
	}
	t.Setenv("DENEB_SWEEP_RETENTION_DAYS", "")
	if got := memorySweepRetention(); got != defaultSweepRetentionDays*24*time.Hour {
		t.Errorf("got %v, want default", got)
	}
}
