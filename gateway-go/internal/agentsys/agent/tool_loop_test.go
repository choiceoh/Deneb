package agent

import (
	"log/slog"
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
		t.Fatalf("expected warning at 3, got stuck=%v level=%v detector=%s", r.Stuck, r.Level, r.Detector)
	}

	// Calls 4: still warning.
	r = d.RecordAndCheck("read", args)
	if r.Level != ToolLoopWarning {
		t.Fatalf("expected warning at 4, got level=%v", r.Level)
	}

	// 5th call: critical.
	r = d.RecordAndCheck("read", args)
	if !r.Stuck || r.Level != ToolLoopCritical || r.Detector != "generic_repeat" {
		t.Fatalf("expected critical at 5, got stuck=%v level=%v detector=%s", r.Stuck, r.Level, r.Detector)
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
		t.Fatalf("expected poll_no_progress warning at 3, got stuck=%v level=%v detector=%s", r.Stuck, r.Level, r.Detector)
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
		t.Fatalf("expected ping_pong warning at 6 alternating calls, got stuck=%v detector=%s", r.Stuck, r.Detector)
	}
	if r.Level != ToolLoopWarning {
		t.Fatalf("expected warning level, got %v", r.Level)
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
		t.Fatalf("expected circuit_breaker at 5, got stuck=%v level=%v detector=%s", r.Stuck, r.Level, r.Detector)
	}
}

func TestToolLoopDetector_Disabled(t *testing.T) {
	cfg := DefaultToolLoopConfig()
	cfg.Enabled = false
	d := NewToolLoopDetector(cfg, slog.Default())

	args := []byte(`{"x": 1}`)
	for range 50 {
		r := d.RecordAndCheck("read", args)
		if r.Stuck {
			t.Fatal("detector should be disabled")
		}
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
