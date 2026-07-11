package observe

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestRing_AppendAndQueryNewestFirst(t *testing.T) {
	r := NewRing(10)
	for i := range 5 {
		r.append(LogLine{Ts: int64(i), Msg: "m", lvl: slog.LevelInfo})
	}
	got := r.Query(QueryOpts{Limit: 10})
	if len(got) != 5 {
		t.Fatalf("len=%d want 5", len(got))
	}
	if got[0].Ts != 4 {
		t.Errorf("newest Ts=%d want 4", got[0].Ts)
	}
	if got[4].Ts != 0 {
		t.Errorf("oldest Ts=%d want 0", got[4].Ts)
	}
}

func TestRing_WrapEvictsOldest(t *testing.T) {
	r := NewRing(3)
	for i := range 5 {
		r.append(LogLine{Ts: int64(i), lvl: slog.LevelInfo})
	}
	got := r.Query(QueryOpts{Limit: 10})
	if len(got) != 3 {
		t.Fatalf("len=%d want 3 (capacity)", len(got))
	}
	// Newest three are 4,3,2; entries 0 and 1 were evicted by the wrap.
	if got[0].Ts != 4 || got[1].Ts != 3 || got[2].Ts != 2 {
		t.Errorf("got Ts %d,%d,%d want 4,3,2", got[0].Ts, got[1].Ts, got[2].Ts)
	}
	if r.Len() != 3 || r.Cap() != 3 {
		t.Errorf("Len/Cap = %d/%d want 3/3", r.Len(), r.Cap())
	}
}

func TestRing_Filters(t *testing.T) {
	r := NewRing(10)
	r.append(LogLine{Ts: 1, RunID: "A", Session: "s1", Msg: "hello", lvl: slog.LevelInfo})
	r.append(LogLine{Ts: 2, RunID: "B", Session: "s2", Msg: "world", lvl: slog.LevelError})
	r.append(LogLine{Ts: 3, RunID: "A", Session: "s1", Msg: "again", lvl: slog.LevelWarn})

	cases := []struct {
		name string
		opts QueryOpts
		want int
	}{
		{"runId", QueryOpts{RunID: "A"}, 2},
		{"session", QueryOpts{Session: "s2"}, 1},
		{"minLevel warn", QueryOpts{MinLevel: slog.LevelWarn}, 2},
		{"contains", QueryOpts{Contains: "wor"}, 1},
		{"since", QueryOpts{SinceMs: 3}, 1},
		{"limit", QueryOpts{Limit: 1}, 1},
		{"combined miss", QueryOpts{RunID: "A", MinLevel: slog.LevelError}, 0},
	}
	for _, c := range cases {
		if got := r.Query(c.opts); len(got) != c.want {
			t.Errorf("%s: got %d want %d", c.name, len(got), c.want)
		}
	}
}

// TestLogCapture_CapturesWithAttrs is the load-bearing test: slog applies
// With() attributes by wrapping handlers rather than mutating the record, so a
// naive capture handler would never see the run-scoped session/runId. This
// verifies the WithAttrs accumulation surfaces them as join keys at Handle time.
func TestLogCapture_CapturesWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	ring := NewRing(10)
	lc := NewCapture(base, ring)

	logger := slog.New(lc).With("session", "client:main", "runId", "run-123")
	logger.Info("doing work", "tool", "grep")

	got := ring.Query(QueryOpts{RunID: "run-123"})
	if len(got) != 1 {
		t.Fatalf("captured %d lines want 1", len(got))
	}
	l := got[0]
	if l.RunID != "run-123" {
		t.Errorf("RunID=%q want run-123", l.RunID)
	}
	if l.Session != "client:main" {
		t.Errorf("Session=%q want client:main", l.Session)
	}
	if l.Msg != "doing work" {
		t.Errorf("Msg=%q", l.Msg)
	}
	if l.Attrs["tool"] != "grep" {
		t.Errorf("Attrs[tool]=%q want grep", l.Attrs["tool"])
	}
	if l.Level != "INFO" {
		t.Errorf("Level=%q want INFO", l.Level)
	}
	// The delegate must still receive the record — capture is a tap, not a sink.
	if !bytes.Contains(buf.Bytes(), []byte("doing work")) {
		t.Errorf("delegate did not receive the record")
	}
}

func TestLogCapture_NilRingPassthrough(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, nil)
	lc := NewCapture(base, nil) // nil ring must not panic
	slog.New(lc).Error("boom")
	if !bytes.Contains(buf.Bytes(), []byte("boom")) {
		t.Errorf("nil-ring capture dropped the record")
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"":      slog.LevelDebug, // empty = no filter = everything
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"bogus": slog.LevelDebug,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q)=%v want %v", in, got, want)
		}
	}
}
