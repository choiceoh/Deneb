package server

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testGuard(t *testing.T, dir string) *factCutoverGuard {
	t.Helper()
	path := ""
	if dir != "" {
		path = filepath.Join(dir, factCutoverFileName)
	}
	return &factCutoverGuard{path: path, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// TestFactCutoverGuardCountsAcrossProcesses is the property the whole guard
// exists for: the streak has to survive the restart, because a process-local
// counter would reset on each one and never reach the bound.
func TestFactCutoverGuardCountsAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	cause := errors.New("import legacy facts: boom")

	for want := 1; want <= maxFactCutoverAttempts; want++ {
		// A fresh guard each time stands in for a fresh process.
		if got := testGuard(t, dir).recordFailure(cause); got != want {
			t.Fatalf("attempt %d recorded %d failures, want %d", want, got, want)
		}
	}
}

func TestFactCutoverGuardSuccessClearsTheStreak(t *testing.T) {
	dir := t.TempDir()
	guard := testGuard(t, dir)
	guard.recordFailure(errors.New("transient"))
	guard.recordFailure(errors.New("transient"))

	guard.recordSuccess()

	if _, err := os.Stat(filepath.Join(dir, factCutoverFileName)); !os.IsNotExist(err) {
		t.Fatalf("counter file survived a success: %v", err)
	}
	// A later failure starts a fresh budget rather than degrading at once.
	if got := testGuard(t, dir).recordFailure(errors.New("later")); got != 1 {
		t.Fatalf("failures after a success = %d, want 1", got)
	}
}

// TestFactCutoverGuardWithoutStateDirNeverDegrades keeps the fallback on the
// safe side: with nowhere to persist, the count is meaningless, and degrading
// on an uncounted failure would be worse than the historical restart.
func TestFactCutoverGuardWithoutStateDirNeverDegrades(t *testing.T) {
	guard := testGuard(t, "")
	for i := 0; i < maxFactCutoverAttempts+2; i++ {
		if got := guard.recordFailure(errors.New("boom")); got >= maxFactCutoverAttempts {
			t.Fatalf("unpersisted failure %d reported %d, which would degrade", i+1, got)
		}
	}
	guard.recordSuccess() // must not panic
}

// TestFactCutoverGuardTolerantOfCorruptState: an unreadable counter must not
// itself become the reason a gateway starts degraded.
func TestFactCutoverGuardTolerantOfCorruptState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, factCutoverFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := testGuard(t, dir).recordFailure(errors.New("boom")); got != 1 {
		t.Fatalf("failures after corrupt state = %d, want 1", got)
	}
}
