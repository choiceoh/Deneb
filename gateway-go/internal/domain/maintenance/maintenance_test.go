package maintenance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewRunner(t *testing.T) {
	r := NewRunner("/tmp/deneb-test-maintenance")
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
	if r.denebDir != "/tmp/deneb-test-maintenance" {
		t.Fatalf("got %q, want denebDir to be set", r.denebDir)
	}
}

func TestRunDryRun(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)

	report := r.Run(true)
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if !report.DryRun {
		t.Fatal("expected dry run flag to be true")
	}
	if report.RanAt == "" {
		t.Fatal("expected ranAt to be set")
	}
}

func TestRunCleansOldSessions(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create an old file (> 30 days).
	oldFile := filepath.Join(sessDir, "old-session.jsonl")
	if err := os.WriteFile(oldFile, []byte("old data"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Create a recent file.
	recentFile := filepath.Join(sessDir, "recent-session.jsonl")
	if err := os.WriteFile(recentFile, []byte("recent data"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(dir)
	report := r.Run(false)

	if len(report.Sessions) != 1 {
		t.Fatalf("got %d, want 1 cleaned session", len(report.Sessions))
	}
	if !report.Sessions[0].Removed {
		t.Fatal("expected old session to be removed")
	}

	// Old file should be gone.
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("expected old file to be removed")
	}
	// Recent file should remain.
	if _, err := os.Stat(recentFile); err != nil {
		t.Fatal("expected recent file to remain")
	}
}

func TestRunCleansOldLogs(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldFile := filepath.Join(logDir, "old.log")
	if err := os.WriteFile(oldFile, []byte("old log"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-15 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(dir)
	report := r.Run(false)

	if len(report.Logs) != 1 {
		t.Fatalf("got %d, want 1 cleaned log", len(report.Logs))
	}
}

func TestDryRunDoesNotRemoveFiles(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldFile := filepath.Join(sessDir, "old.jsonl")
	if err := os.WriteFile(oldFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(dir)
	report := r.Run(true)

	if len(report.Sessions) != 1 {
		t.Fatalf("got %d, want 1 session in report", len(report.Sessions))
	}

	// File should still exist.
	if _, err := os.Stat(oldFile); err != nil {
		t.Fatal("expected file to remain after dry run")
	}
}

func TestLastReport(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)

	if r.LastReport() != nil {
		t.Fatal("expected nil last report before any run")
	}

	r.Run(true)
	if r.LastReport() == nil {
		t.Fatal("expected non-nil last report after run")
	}
}

func TestSummarizeReport(t *testing.T) {
	if SummarizeReport(nil) != nil {
		t.Fatal("expected nil summary for nil report")
	}

	report := &Report{
		RanAt:  "2026-01-01T00:00:00Z",
		DryRun: false,
		Sessions: []CleanedFile{
			{Path: "/a", Size: 1024, Removed: true},
		},
		Logs:         []CleanedFile{},
		TotalFreedMB: 0.001,
	}
	summary := SummarizeReport(report)
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.SessionsCleaned != 1 {
		t.Fatalf("got %d, want 1 session cleaned", summary.SessionsCleaned)
	}
	if summary.LogsCleaned != 0 {
		t.Fatalf("got %d, want 0 logs cleaned", summary.LogsCleaned)
	}
}

func TestRunEmptyDirs(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)

	// No sessions/ or logs/ directories — should not panic.
	report := r.Run(false)
	if report == nil {
		t.Fatal("expected non-nil report even with missing dirs")
	}
	if len(report.Sessions) != 0 {
		t.Fatal("expected no sessions")
	}
	if len(report.Logs) != 0 {
		t.Fatal("expected no logs")
	}
}

func TestLogSizeBudgetCleanup(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create files whose total exceeds 50 MB — use small files + override
	// by testing the internal function directly.
	now := time.Now()

	// 3 recent files, each 20 MB (total 60 MB, budget 50 MB).
	for i, name := range []string{"a.log", "b.log", "c.log"} {
		path := filepath.Join(logDir, name)
		data := make([]byte, 20*1024*1024)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		// Make files progressively newer so oldest is removed first.
		ft := now.Add(-time.Duration(3-i) * time.Hour)
		if err := os.Chtimes(path, ft, ft); err != nil {
			t.Fatal(err)
		}
	}

	files := cleanLogFiles(logDir, logMaxAge, logMaxTotalMB*1024*1024, now, false)

	// Should have removed the oldest file (a.log) to bring total under 50 MB.
	if len(files) < 1 {
		t.Fatalf("got %d, want at least 1 file cleaned", len(files))
	}
}
