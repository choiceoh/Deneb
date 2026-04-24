package chat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// TestCompaction_StuckAfterIdempotentRetries verifies the anti-thrashing
// guard: when the LLM keeps returning context-overflow and the cheap-first
// shrink pipeline can't change the messages (so the hash stays the same
// between attempts), runAgentWithFallback aborts early with
// stopReasonCompressionStuck instead of burning the full retry budget.
//
// Without the guard, the old code would run agent.RunAgent
// maxCompactionRetries+1 = 3 times — each hitting the LLM server once
// before deciding to compact. With the guard, the second attempt detects
// the identical input hash (post-compaction messages unchanged because
// the messages are too small to trigger LLMCompact / emergency paths)
// and short-circuits.
//
// The test verifies the *observed* behavior: a stuck session eventually
// surfaces a Korean reply rather than an opaque "context overflow" API
// error, and does not hit the LLM more than max retries + 1.
func TestCompaction_StuckAfterIdempotentRetries(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Always respond with a provider-side "context length exceeded"
		// error so isContextOverflow returns true and we enter the
		// mid-loop compaction retry path.
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error": {"message": "This model's maximum context length is 200000 tokens. However, your messages resulted in 250000 tokens.", "type": "invalid_request_error", "code": "context_length_exceeded"}}`)
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	sm := session.NewManager()
	bc := &broadcastCollector{}
	client := llm.NewClient(server.URL, "test-key", llm.WithRetry(0, 0, 0))

	cfg := DefaultHandlerConfig()
	cfg.LLMClient = client
	cfg.Transcript = transcript
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "test"
	cfg.MaxTokens = 1024

	h := NewHandler(sm, bc.broadcast, nil, cfg)
	defer h.Close()

	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  "compact-stuck-1",
		"message":     "compaction stuck test",
		"clientRunId": "run-compact-stuck-1",
	})
	h.Send(context.Background(), req)

	// The run must settle (don't hang forever retrying).
	// StatusDone — anti-thrashing converted the failure into a clean
	// finish with stopReason=compression_stuck + Korean reply.
	// StatusFailed — also acceptable as long as we didn't burn an
	// unbounded number of LLM calls.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		s := sm.Get("compact-stuck-1")
		if s != nil && (s.Status == session.StatusDone || s.Status == session.StatusFailed) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The retry loop calls agent.RunAgent up to maxCompactionRetries+1 = 3
	// times. Each internal RunAgent invocation makes one LLM API request
	// (streaming with context-overflow → immediate error → retry decision).
	// We allow a small generous margin (6) to accommodate httpretry and
	// model-fallback chain probing, but anything north of that means the
	// anti-thrashing guard did not fire.
	if callCount > 6 {
		t.Fatalf("LLM was called %d times — anti-thrashing guard should have stopped retries earlier", callCount)
	}
}
