package chat

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// ─── sanitizeInput ─────────────────────────────────────────────────────────

func TestSanitizeInput_empty(t *testing.T) {
	if got := sanitizeInput(""); got != "" {
		t.Errorf("sanitizeInput(\"\") = %q, want \"\"", got)
	}
}

func TestSanitizeInput_normalText(t *testing.T) {
	if got := sanitizeInput("hello world"); got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestSanitizeInput_korean(t *testing.T) {
	input := "안녕하세요 세계"
	if got := sanitizeInput(input); got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestSanitizeInput_stripsControlChars(t *testing.T) {
	input := "hello\x00world\x01!"
	want := "hello\x00world\x01!"
	got := sanitizeInput(input)
	// \x00 and \x01 are control chars and should be stripped.
	if got == want {
		t.Errorf("got %q, want control chars to be stripped", got)
	}
	if got != "helloworld!" {
		t.Errorf("got %q, want %q", got, "helloworld!")
	}
}

func TestSanitizeInput_preservesWhitespace(t *testing.T) {
	input := "  line1\n\tline2\r\n  "
	got := sanitizeInput(input)
	// TrimSpace removes leading/trailing, but tabs/newlines in the middle survive.
	if got != "line1\n\tline2" {
		t.Errorf("got %q, want %q", got, "line1\n\tline2")
	}
}

func TestSanitizeInput_invalidUTF8(t *testing.T) {
	// Invalid UTF-8 byte sequence.
	input := "hello\xfe\xffworld"
	got := sanitizeInput(input)
	if got != "helloworld" {
		t.Errorf("got %q, want %q", got, "helloworld")
	}
}

// ─── formatCompactTokens ──────────────────────────────────────────────────

func TestFormatCompactTokens(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{500, "500"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{999_999, "1000.0K"},
		{1_000_000, "1.0M"},
		{2_500_000, "2.5M"},
	}
	for _, tt := range tests {
		got := formatCompactTokens(tt.n)
		if got != tt.want {
			t.Errorf("formatCompactTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// ─── formatUptime ──────────────────────────────────────────────────────────

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0m"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h 30m"},
		{25 * time.Hour, "1d 1h 0m"},
		{49*time.Hour + 30*time.Minute, "2d 1h 30m"},
		{-1 * time.Hour, "0m"}, // negative clamped to 0
	}
	for _, tt := range tests {
		got := formatUptime(tt.d)
		if got != tt.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// ─── pending queue operations ──────────────────────────────────────────────

func TestPendingQueue_enqueueAndDrain(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-session-1"

	// Initially no pending.
	if got := h.pending.Drain(key); got != nil {
		t.Fatalf("got %+v, want nil drain", got)
	}

	// Enqueue a message.
	h.pending.Enqueue(key, RunParams{SessionKey: key, Message: "first"})
	p := h.pending.Drain(key)
	if p == nil {
		t.Fatal("expected pending message")
	}
	if p.Message != "first" {
		t.Errorf("got message %q, want %q", p.Message, "first")
	}

	// After drain, queue is empty.
	if got := h.pending.Drain(key); got != nil {
		t.Fatal("expected nil after drain")
	}
}

func TestPendingQueue_newerSupersedes(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-session-2"
	h.pending.Enqueue(key, RunParams{SessionKey: key, Message: "old"})
	h.pending.Enqueue(key, RunParams{SessionKey: key, Message: "new"})

	// Only latest message survives (at-most-1 semantics).
	p := h.pending.Drain(key)
	if p == nil {
		t.Fatal("expected pending message")
	}
	if p.Message != "new" {
		t.Errorf("got message %q, want %q (newest should win)", p.Message, "new")
	}
}

func TestPendingQueue_clearRemovesAll(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-session-3"
	h.pending.Enqueue(key, RunParams{SessionKey: key, Message: "queued"})
	h.pending.Clear(key)

	if got := h.pending.Drain(key); got != nil {
		t.Fatal("expected nil after clear")
	}
}

// ─── InterruptActiveRun ────────────────────────────────────────────────────

func TestInterruptActiveRun_cancelsMatchingSession(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	canceled := false
	h.abort.Register("run-1", &AbortEntry{
		SessionKey: "sess-A",
		ClientRun:  "run-1",
		CancelFn:   func() { canceled = true },
		ExpiresAt:  time.Now().Add(time.Hour),
	})
	h.abort.Register("run-2", &AbortEntry{
		SessionKey: "sess-B",
		ClientRun:  "run-2",
		CancelFn:   func() {},
		ExpiresAt:  time.Now().Add(time.Hour),
	})

	h.InterruptActiveRun("sess-A")

	if !canceled {
		t.Error("expected sess-A run to be canceled")
	}

	if h.abort.HasActiveRun("sess-A") {
		t.Error("run-1 should have been removed")
	}
	if !h.abort.HasActiveRun("sess-B") {
		t.Error("run-2 (different session) should still exist")
	}
}

func TestInterruptActiveRun_noopWhenEmpty(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	// Should not panic or error when no runs exist.
	h.InterruptActiveRun("nonexistent-session")
}

// ─── countActiveRuns ───────────────────────────────────────────────────────

func TestCountActiveRuns(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	if got := h.abort.CountForSession("sess"); got != 0 {
		t.Errorf("got %d, want 0", got)
	}

	h.abort.Register("r1", &AbortEntry{SessionKey: "sess", CancelFn: func() {}, ExpiresAt: time.Now().Add(time.Hour)})
	h.abort.Register("r2", &AbortEntry{SessionKey: "sess", CancelFn: func() {}, ExpiresAt: time.Now().Add(time.Hour)})
	h.abort.Register("r3", &AbortEntry{SessionKey: "other", CancelFn: func() {}, ExpiresAt: time.Now().Add(time.Hour)})

	if got := h.abort.CountForSession("sess"); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
	if got := h.abort.CountForSession("other"); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

// ─── cleanupAbort ──────────────────────────────────────────────────────────

func TestCleanupAbort_removesEntry(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	h.abort.Register("run-x", &AbortEntry{SessionKey: "s", CancelFn: func() {}, ExpiresAt: time.Now().Add(time.Hour)})

	h.abort.Cleanup("run-x")

	if h.abort.HasActiveRun("s") {
		t.Error("expected run-x to be removed")
	}
}

func TestCleanupAbort_emptyIDNoop(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	// Should not panic.
	h.abort.Cleanup("")
}
