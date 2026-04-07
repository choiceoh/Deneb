package chat

import (
	"encoding/json"
	"fmt"
	"strings"
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
		t.Errorf("expected control chars to be stripped, got %q", got)
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
	if got := h.drainPending(key); got != nil {
		t.Fatalf("expected nil drain, got %+v", got)
	}

	// Enqueue a message.
	h.enqueuePending(key, RunParams{SessionKey: key, Message: "first"})
	p := h.drainPending(key)
	if p == nil {
		t.Fatal("expected pending message")
	}
	if p.Message != "first" {
		t.Errorf("got message %q, want %q", p.Message, "first")
	}

	// After drain, queue is empty.
	if got := h.drainPending(key); got != nil {
		t.Fatal("expected nil after drain")
	}
}

func TestPendingQueue_newerSupersedes(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-session-2"
	h.enqueuePending(key, RunParams{SessionKey: key, Message: "old"})
	h.enqueuePending(key, RunParams{SessionKey: key, Message: "new"})

	// Only latest message survives (at-most-1 semantics).
	p := h.drainPending(key)
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
	h.enqueuePending(key, RunParams{SessionKey: key, Message: "queued"})
	h.clearPending(key)

	if got := h.drainPending(key); got != nil {
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
	h.abortMu.Lock()
	h.abortMap["run-1"] = &AbortEntry{
		SessionKey: "sess-A",
		ClientRun:  "run-1",
		CancelFn:   func() { canceled = true },
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	h.abortMap["run-2"] = &AbortEntry{
		SessionKey: "sess-B",
		ClientRun:  "run-2",
		CancelFn:   func() {},
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	h.abortMu.Unlock()

	h.InterruptActiveRun("sess-A")

	if !canceled {
		t.Error("expected sess-A run to be canceled")
	}

	h.abortMu.Lock()
	if _, ok := h.abortMap["run-1"]; ok {
		t.Error("run-1 should have been removed from abortMap")
	}
	if _, ok := h.abortMap["run-2"]; !ok {
		t.Error("run-2 (different session) should still be in abortMap")
	}
	h.abortMu.Unlock()
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

	if got := h.countActiveRuns("sess"); got != 0 {
		t.Errorf("got %d, want 0", got)
	}

	h.abortMu.Lock()
	h.abortMap["r1"] = &AbortEntry{SessionKey: "sess", CancelFn: func() {}, ExpiresAt: time.Now().Add(time.Hour)}
	h.abortMap["r2"] = &AbortEntry{SessionKey: "sess", CancelFn: func() {}, ExpiresAt: time.Now().Add(time.Hour)}
	h.abortMap["r3"] = &AbortEntry{SessionKey: "other", CancelFn: func() {}, ExpiresAt: time.Now().Add(time.Hour)}
	h.abortMu.Unlock()

	if got := h.countActiveRuns("sess"); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
	if got := h.countActiveRuns("other"); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

// ─── cleanupAbort ──────────────────────────────────────────────────────────

func TestCleanupAbort_removesEntry(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	h.abortMu.Lock()
	h.abortMap["run-x"] = &AbortEntry{SessionKey: "s", CancelFn: func() {}, ExpiresAt: time.Now().Add(time.Hour)}
	h.abortMu.Unlock()

	h.cleanupAbort("run-x")

	h.abortMu.Lock()
	if _, ok := h.abortMap["run-x"]; ok {
		t.Error("expected run-x to be removed")
	}
	h.abortMu.Unlock()
}

func TestCleanupAbort_emptyIDNoop(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	// Should not panic.
	h.cleanupAbort("")
}

// ─── budgetHistory ─────────────────────────────────────────────────────────

func TestBudgetHistory_keepsRecentMessages(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	cfg := DefaultHandlerConfig()
	cfg.MaxHistoryBytes = 200 // very small budget to force truncation
	cfg.MaxMessageBytes = 500
	h := NewHandler(sm, bc, nil, cfg)
	defer h.Close()

	msgs := make([]json.RawMessage, 0, 20)
	for i := 0; i < 20; i++ {
		raw, _ := json.Marshal(map[string]any{
			"role":    "user",
			"content": strings.Repeat("x", 50) + fmt.Sprintf(" msg-%d", i),
		})
		msgs = append(msgs, raw)
	}
	payload, _ := json.Marshal(map[string]any{"messages": msgs, "total": 20})

	resp := h.budgetHistory("req-1", payload)
	if resp == nil {
		t.Fatal("expected response")
	}
	var result struct {
		Messages []json.RawMessage `json:"messages"`
		Budgeted bool              `json:"budgeted"`
	}
	json.Unmarshal(resp.Payload, &result)
	if !result.Budgeted {
		t.Error("expected budgeted=true")
	}
	// Should have fewer messages than original due to budget.
	if len(result.Messages) >= 20 {
		t.Errorf("expected messages to be truncated, got %d", len(result.Messages))
	}
	if len(result.Messages) == 0 {
		t.Error("expected at least some messages")
	}
}

func TestBudgetHistory_invalidPayload(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	resp := h.budgetHistory("req-1", json.RawMessage(`invalid json`))
	if resp == nil {
		t.Fatal("expected response even for invalid JSON")
	}
	var result map[string]any
	json.Unmarshal(resp.Payload, &result)
	if result["error"] == nil {
		t.Error("expected error field in result")
	}
}
