package agent

import (
	"log/slog"
	"strings"
	"testing"
)

func TestToolLoopDetector_GenericRepeat(t *testing.T) {
	cfg := DefaultToolLoopConfig()
	cfg.WarningThreshold = 3
	cfg.CriticalThreshold = 5
	cfg.GlobalCircuitBreakerThreshold = 10
	d := NewToolLoopDetector(cfg, slog.Default())

	args := []byte(`{"path": "/foo/bar"}`)

	// First 2 calls: no detection.
	for i := range 2 {
		r := d.RecordAndCheck("read", args)
		if r.Stuck {
			t.Fatalf("unexpected stuck at call %d", i+1)
		}
	}

	// 3rd call: warning.
	r := d.RecordAndCheck("read", args)
	if !r.Stuck || r.Level != ToolLoopWarning || r.Detector != "generic_repeat" {
		t.Fatalf("got stuck=%v level=%v detector=%s, want warning at 3", r.Stuck, r.Level, r.Detector)
	}

	// Calls 4: still warning.
	r = d.RecordAndCheck("read", args)
	if r.Level != ToolLoopWarning {
		t.Fatalf("got level=%v, want warning at 4", r.Level)
	}

	// 5th call: critical.
	r = d.RecordAndCheck("read", args)
	if !r.Stuck || r.Level != ToolLoopCritical || r.Detector != "generic_repeat" {
		t.Fatalf("got stuck=%v level=%v detector=%s, want critical at 5", r.Stuck, r.Level, r.Detector)
	}
}

func TestToolLoopDetector_DifferentArgs(t *testing.T) {
	cfg := DefaultToolLoopConfig()
	cfg.WarningThreshold = 3
	d := NewToolLoopDetector(cfg, slog.Default())

	// Different args should not trigger.
	for i := range 10 {
		args := []byte(`{"path": "` + string(rune('a'+i)) + `"}`)
		r := d.RecordAndCheck("read", args)
		if r.Stuck {
			t.Fatalf("unexpected stuck with different args at call %d", i+1)
		}
	}
}

func TestToolLoopDetector_PollNoProgress(t *testing.T) {
	cfg := DefaultToolLoopConfig()
	cfg.WarningThreshold = 3
	cfg.CriticalThreshold = 5
	cfg.GlobalCircuitBreakerThreshold = 10
	d := NewToolLoopDetector(cfg, slog.Default())

	args := []byte(`{"action": "poll", "pid": 123}`)

	// Simulate polling with identical results.
	for i := range 2 {
		r := d.RecordAndCheck("process", args)
		if r.Stuck {
			t.Fatalf("unexpected stuck at poll %d", i+1)
		}
		d.RecordResult("process", "running pid=123", false)
	}

	// 3rd poll with same result: warning.
	r := d.RecordAndCheck("process", args)
	if !r.Stuck || r.Level != ToolLoopWarning || r.Detector != "poll_no_progress" {
		t.Fatalf("got stuck=%v level=%v detector=%s, want poll_no_progress warning at 3", r.Stuck, r.Level, r.Detector)
	}
}

func TestToolLoopDetector_PollWithProgress(t *testing.T) {
	cfg := DefaultToolLoopConfig()
	cfg.WarningThreshold = 3
	d := NewToolLoopDetector(cfg, slog.Default())

	args := []byte(`{"action": "poll", "pid": 123}`)

	// Polling with different results should not trigger.
	for i := range 10 {
		r := d.RecordAndCheck("process", args)
		if r.Stuck {
			t.Fatalf("unexpected stuck at poll %d", i+1)
		}
		d.RecordResult("process", "progress "+string(rune('0'+i)), false)
	}
}

func TestToolLoopDetector_PingPong(t *testing.T) {
	cfg := DefaultToolLoopConfig()
	cfg.WarningThreshold = 6
	cfg.CriticalThreshold = 12
	cfg.GlobalCircuitBreakerThreshold = 30
	d := NewToolLoopDetector(cfg, slog.Default())

	argsA := []byte(`{"file": "a.go"}`)
	argsB := []byte(`{"file": "b.go"}`)

	// Alternate A and B (2 pairs = 4 calls, below threshold).
	for i := range 2 {
		r := d.RecordAndCheck("read", argsA)
		if r.Stuck {
			t.Fatalf("unexpected stuck at A call %d", i+1)
		}
		r = d.RecordAndCheck("write", argsB)
		if r.Stuck {
			t.Fatalf("unexpected stuck at B call %d", i+1)
		}
	}

	// Continue alternating to reach the warning threshold (6 alternating calls).
	d.RecordAndCheck("read", argsA)       // call 5
	r := d.RecordAndCheck("write", argsB) // call 6
	if !r.Stuck || r.Detector != "ping_pong" {
		t.Fatalf("got stuck=%v detector=%s, want ping_pong warning at 6 alternating calls", r.Stuck, r.Detector)
	}
	if r.Level != ToolLoopWarning {
		t.Fatalf("got %v, want warning level", r.Level)
	}
}

func TestToolLoopDetector_GlobalCircuitBreaker(t *testing.T) {
	// Use a non-polling tool ("read") so poll_no_progress and generic_repeat
	// don't interfere. Set warning/critical high so only the circuit breaker fires.
	cfg := ToolLoopConfig{
		Enabled:                       true,
		HistorySize:                   30,
		WarningThreshold:              100,
		CriticalThreshold:             101,
		GlobalCircuitBreakerThreshold: 5,
	}
	d := NewToolLoopDetector(cfg, slog.Default())

	args := []byte(`{"path": "/status"}`)

	for i := range 4 {
		r := d.RecordAndCheck("read", args)
		if r.Stuck {
			t.Fatalf("unexpected stuck at call %d", i+1)
		}
	}

	// 5th call: circuit breaker.
	r := d.RecordAndCheck("read", args)
	if !r.Stuck || r.Level != ToolLoopCritical || r.Detector != "circuit_breaker" {
		t.Fatalf("got stuck=%v level=%v detector=%s, want circuit_breaker at 5", r.Stuck, r.Level, r.Detector)
	}
}

func TestToolLoopDetector_HistoryWindowSlides(t *testing.T) {
	cfg := DefaultToolLoopConfig()
	cfg.HistorySize = 5
	cfg.WarningThreshold = 4
	cfg.CriticalThreshold = 10
	cfg.GlobalCircuitBreakerThreshold = 20
	d := NewToolLoopDetector(cfg, slog.Default())

	args := []byte(`{"x": 1}`)

	// Fill history with 3 identical calls.
	for range 3 {
		d.RecordAndCheck("read", args)
	}

	// Add 3 different calls to push old ones out.
	for i := range 3 {
		d.RecordAndCheck("write", []byte(`{"y": `+string(rune('0'+i))+`}`))
	}

	// Now "read" should only appear 0 times (pushed out by window).
	r := d.RecordAndCheck("read", args)
	if r.Stuck {
		t.Fatal("old history should have been evicted")
	}
}

func TestToolLoopDetector_RecordFileMutation_SamePathThrash(t *testing.T) {
	d := NewToolLoopDetector(DefaultToolLoopConfig(), slog.Default())
	root := t.TempDir()

	// Each call carries a DIFFERENT old_string/new_string — the exact case the
	// hash-based detectors miss — but targets the SAME file. The per-path
	// breaker keys on the resolved path, not name+args, so it still counts.
	edit := func(i int) string {
		args := []byte(`{"file_path": "pkg/foo.go", "old_string": "v` +
			string(rune('0'+i)) + `", "new_string": "w` + string(rune('0'+i)) + `"}`)
		return d.RecordFileMutation(root, "edit", args)
	}

	// First samePathEditNudgeThreshold-1 edits: silent.
	for i := 0; i < samePathEditNudgeThreshold-1; i++ {
		if nudge := edit(i); nudge != "" {
			t.Fatalf("unexpected nudge at edit %d: %q", i+1, nudge)
		}
	}

	// The threshold-th edit fires the one-shot nudge, naming the display path.
	nudge := edit(samePathEditNudgeThreshold - 1)
	if nudge == "" {
		t.Fatalf("expected nudge at edit %d", samePathEditNudgeThreshold)
	}
	if !strings.Contains(nudge, "pkg/foo.go") {
		t.Fatalf("nudge should name the file path, got %q", nudge)
	}

	// Further edits to the same path keep counting but must NOT re-fire.
	if again := edit(samePathEditNudgeThreshold); again != "" {
		t.Fatalf("nudge should fire once per path, got repeat: %q", again)
	}
}

func TestToolLoopDetector_RecordFileMutation_DistinctPathsIndependent(t *testing.T) {
	d := NewToolLoopDetector(DefaultToolLoopConfig(), slog.Default())
	root := t.TempDir()

	// Editing many DIFFERENT files (normal multi-file work) never nudges, even
	// well past the per-file threshold in aggregate.
	for i := 0; i < samePathEditNudgeThreshold*2; i++ {
		args := []byte(`{"file_path": "file` + string(rune('a'+i)) + `.go"}`)
		if nudge := d.RecordFileMutation(root, "write", args); nudge != "" {
			t.Fatalf("distinct paths must not nudge, got %q at %d", nudge, i+1)
		}
	}
}

func TestToolLoopDetector_RecordFileMutation_NonMutatingAndNoRoot(t *testing.T) {
	d := NewToolLoopDetector(DefaultToolLoopConfig(), slog.Default())
	root := t.TempDir()

	// A non-file-mutating tool resolves to no path and is ignored, no matter
	// how often it is called.
	for i := 0; i < samePathEditNudgeThreshold*2; i++ {
		if nudge := d.RecordFileMutation(root, "read", []byte(`{"file_path": "foo.go"}`)); nudge != "" {
			t.Fatalf("non-mutating tool must not nudge, got %q", nudge)
		}
	}

	// With no provenance root, paths cannot be resolved (root-confinement is
	// unavailable), so the breaker stays silent rather than guessing.
	for i := 0; i < samePathEditNudgeThreshold*2; i++ {
		if nudge := d.RecordFileMutation("", "edit", []byte(`{"file_path": "foo.go"}`)); nudge != "" {
			t.Fatalf("empty root must not nudge, got %q", nudge)
		}
	}
}

func TestToolLoopDetector_RecordFileMutation_Disabled(t *testing.T) {
	cfg := DefaultToolLoopConfig()
	cfg.Enabled = false
	d := NewToolLoopDetector(cfg, slog.Default())
	root := t.TempDir()

	for i := 0; i < samePathEditNudgeThreshold*2; i++ {
		if nudge := d.RecordFileMutation(root, "edit", []byte(`{"file_path": "foo.go"}`)); nudge != "" {
			t.Fatalf("disabled detector must not nudge, got %q", nudge)
		}
	}
}
