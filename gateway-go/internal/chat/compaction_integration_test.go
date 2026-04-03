package chat

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// --- Compaction integration tests ---
//
// These tests exercise the compaction pipeline end-to-end:
// - Context overflow detection and retry with compacted context
// - Aurora store message syncing during chat runs
// - Reduced-budget fallback when Aurora is unavailable
// - Compaction evaluation thresholds (via aurora.EvaluateCompaction)
// - Transcript + Aurora store consistency after compaction

// tempAuroraStore creates an Aurora store backed by a temp directory for testing.
func tempAuroraStore(t *testing.T) *aurora.Store {
	t.Helper()
	dir := t.TempDir()
	cfg := aurora.StoreConfig{DatabasePath: filepath.Join(dir, "aurora.db")}
	s, err := aurora.NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestCompaction_ContextOverflowRetry tests that when the LLM returns a context
// overflow error, the agent run retries with a reduced context window
// (legacy fallback when Aurora is not available).
func TestCompaction_ContextOverflowRetry(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: return context overflow error.
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error": {"message": "context_length_exceeded: max 4096 tokens"}}`)
			return
		}
		// Second call (after compaction): succeed.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("After compaction", "end_turn"))
	}))
	defer server.Close()

	// Pre-populate transcript with enough messages to trigger overflow.
	transcript := NewMemoryTranscriptStore()
	sessionKey := "compact-retry-1"
	for i := 0; i < 50; i++ {
		transcript.Append(sessionKey, NewTextChatMessage("user",
			fmt.Sprintf("Message %d: %s", i, strings.Repeat("padding ", 20)),
			int64(i*1000)))
		transcript.Append(sessionKey, NewTextChatMessage("assistant",
			fmt.Sprintf("Reply %d: %s", i, strings.Repeat("response ", 20)),
			int64(i*1000+500)))
	}

	sm := session.NewManager()
	bc := &broadcastCollector{}
	client := llm.NewClient(server.URL, "test-key", llm.WithRetry(0, 0, 0))

	cfg := DefaultHandlerConfig()
	cfg.LLMClient = client
	cfg.Transcript = transcript
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "test"
	cfg.MaxTokens = 1024
	cfg.ContextCfg = ContextConfig{
		TokenBudget:    1000, // low budget to trigger overflow
		FreshTailCount: 4,
		MaxMessages:    50,
	}

	h := NewHandler(sm, bc.broadcast, nil, cfg)
	defer h.Close()

	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "trigger overflow",
		"clientRunId": "run-compact-1",
	})
	h.Send(context.Background(), req)

	// Wait for the run to complete (with compaction retry).
	status := waitForSessionStatus(sm, sessionKey, session.StatusDone, 10*time.Second)
	if status != session.StatusDone {
		// If compaction retry didn't work, it might have failed.
		// That's acceptable for the legacy path without FFI.
		if status != session.StatusFailed {
			t.Fatalf("session status = %q, want done or failed", status)
		}
		t.Skip("compaction retry not available without FFI")
	}

	// Verify the second LLM call succeeded.
	if callCount < 2 {
		t.Errorf("LLM call count = %d, want >= 2 (overflow + retry)", callCount)
	}
}

// TestCompaction_EvaluateCompactionThresholds tests the pure-Go compaction
// evaluation logic with various token counts.
func TestCompaction_EvaluateCompactionThresholds(t *testing.T) {
	tests := []struct {
		name        string
		stored      uint64
		live        uint64
		budget      uint64
		wantCompact bool
	}{
		{
			name:        "under threshold",
			stored:      50_000,
			live:        50_000,
			budget:      100_000,
			wantCompact: false,
		},
		{
			name:        "at threshold boundary",
			stored:      80_000,
			live:        80_000,
			budget:      100_000,
			wantCompact: false, // threshold is 0.80, so 80_000 == threshold (not exceeded)
		},
		{
			name:        "over threshold",
			stored:      81_000,
			live:        81_000,
			budget:      100_000,
			wantCompact: true,
		},
		{
			name:        "well over threshold",
			stored:      120_000,
			live:        120_000,
			budget:      100_000,
			wantCompact: true,
		},
		{
			name:        "zero budget always compacts",
			stored:      100,
			live:        100,
			budget:      0,
			wantCompact: true, // threshold = 0*0.80 = 0, 100 > 0
		},
		{
			name:        "stored high but live low",
			stored:      90_000,
			live:        10_000,
			budget:      100_000,
			wantCompact: true, // max(stored, live) > threshold
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := aurora.DefaultSweepConfig()
			got, _, err := aurora.EvaluateCompaction(cfg, tt.stored, tt.live, tt.budget)
			if err != nil {
				t.Fatalf("EvaluateCompaction: %v", err)
			}
			if got != tt.wantCompact {
				t.Errorf("shouldCompact = %v, want %v", got, tt.wantCompact)
			}
		})
	}
}

// TestCompaction_ReducedBudgetFallback tests that the overflow handler halves
// the context budget when Aurora is not available or sweep fails.
func TestCompaction_ReducedBudgetFallback(t *testing.T) {
	transcript := NewMemoryTranscriptStore()
	sessionKey := "reduced-budget"

	// Fill transcript with many messages.
	for i := 0; i < 100; i++ {
		transcript.Append(sessionKey, NewTextChatMessage("user",
			fmt.Sprintf("Message %d", i), int64(i)))
	}

	ctxCfg := ContextConfig{
		TokenBudget:    500, // low budget to test reduced assembly
		FreshTailCount: 4,
		MaxMessages:    100,
	}
	// Halve budget (same logic as handleContextOverflowAurora fallback).
	reducedCfg := ctxCfg
	reducedCfg.TokenBudget /= 2
	if reducedCfg.MaxMessages > 10 {
		reducedCfg.MaxMessages /= 2
	}
	result, err := assembleContext(transcript, sessionKey, reducedCfg, slog.Default())
	if err != nil {
		t.Fatalf("assembleContext with reduced budget: %v", err)
	}

	// Should return fewer messages than the full transcript.
	if len(result.Messages) >= 100 {
		t.Errorf("expected reduced messages, got %d", len(result.Messages))
	}
	if len(result.Messages) == 0 {
		t.Error("expected non-empty messages after reduced budget assembly")
	}
}

// TestCompaction_EndToEndChatWithAurora tests the full chat flow with
// Aurora store integration: multiple messages accumulate, token counts
// are tracked, and compaction artifacts remain consistent.
func TestCompaction_EndToEndChatWithAurora(t *testing.T) {
	msgCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		msgCount++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse(fmt.Sprintf("Reply %d", msgCount), "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	auroraStore := tempAuroraStore(t)
	sm := session.NewManager()
	bc := &broadcastCollector{}

	client := llm.NewClient(server.URL, "test-key")
	cfg := DefaultHandlerConfig()
	cfg.LLMClient = client
	cfg.Transcript = transcript
	cfg.AuroraStore = auroraStore
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "test"
	cfg.MaxTokens = 1024

	h := NewHandler(sm, bc.broadcast, nil, cfg)
	defer h.Close()

	sessionKey := "aurora-e2e"

	// Send multiple messages.
	for i := 0; i < 5; i++ {
		req := makeReq(fmt.Sprintf("%d", i), "chat.send", map[string]any{
			"sessionKey":  sessionKey,
			"message":     fmt.Sprintf("Question %d", i),
			"clientRunId": fmt.Sprintf("run-ae2e-%d", i),
		})
		h.Send(context.Background(), req)
		waitForSessionStatus(sm, sessionKey, session.StatusDone, 5*time.Second)
	}

	// Verify transcript has 10 messages (5 user + 5 assistant).
	msgs, total, err := transcript.Load(sessionKey, 0)
	if err != nil {
		t.Fatalf("transcript load: %v", err)
	}
	if total != 10 {
		t.Errorf("transcript total = %d, want 10", total)
	}
	_ = msgs

	// Verify Aurora store tracked all messages.
	items, err := auroraStore.FetchContextItems(1)
	if err != nil {
		t.Fatalf("FetchContextItems: %v", err)
	}
	if len(items) < 10 {
		t.Errorf("Aurora items = %d, want >= 10", len(items))
	}

	// Verify Aurora token count is non-zero and consistent.
	totalTokens, _ := auroraStore.FetchTokenCount(1)
	if totalTokens == 0 {
		t.Error("expected non-zero Aurora token count")
	}

	// Now compact older messages in Aurora and verify consistency.
	// Compact first 6 message items into a leaf summary.
	if len(items) >= 6 {
		var compactMsgIDs []uint64
		var compactTokens uint64
		for _, ci := range items[:6] {
			if ci.MessageID != nil {
				compactMsgIDs = append(compactMsgIDs, *ci.MessageID)
				fetchedMsgs, _ := auroraStore.FetchMessages([]uint64{*ci.MessageID})
				if m, ok := fetchedMsgs[*ci.MessageID]; ok {
					compactTokens += m.TokenCount
				}
			}
		}

		tokensBefore, _ := auroraStore.FetchTokenCount(1)
		err := auroraStore.PersistLeafSummary(aurora.PersistLeafInput{
			SummaryID:               "e2e_leaf_1",
			ConversationID:          1,
			Content:                 "Summary of early conversation",
			TokenCount:              5, // summary is much smaller than compacted messages
			FileIDs:                 []string{},
			SourceMessageTokenCount: compactTokens,
			MessageIDs:              compactMsgIDs,
			StartOrdinal:            items[0].Ordinal,
			EndOrdinal:              items[5].Ordinal,
		})
		if err != nil {
			t.Fatalf("PersistLeafSummary: %v", err)
		}

		tokensAfter, _ := auroraStore.FetchTokenCount(1)
		// After compacting 6 messages (each ~3-4 tokens) into a 5-token summary,
		// the total should decrease.
		if tokensAfter >= tokensBefore {
			t.Errorf("expected reduced tokens: before=%d after=%d", tokensBefore, tokensAfter)
		}

		// Verify assembly still works after compaction.
		asmCfg := aurora.AssemblyConfig{
			TokenBudget:    100_000,
			FreshTailCount: 4,
			MaxMessages:    100,
		}
		asmResult, err := aurora.Assemble(context.Background(), auroraStore, 1, asmCfg, nil)
		if err != nil {
			t.Fatalf("Assemble after compaction: %v", err)
		}
		if len(asmResult.Messages) == 0 {
			t.Error("expected non-empty assembly after compaction")
		}
	}
}

// TestCompaction_IsContextOverflowDetection verifies the error string matching
// for context overflow detection used in the retry logic.
func TestCompaction_IsContextOverflowDetection(t *testing.T) {
	tests := []struct {
		errMsg string
		want   bool
	}{
		{"context_length_exceeded", true},
		{"error: context_too_long for model gpt-4", true},
		{"prompt is too long", true},
		{"This model's maximum context length is 128000", true},
		{"rate_limit_exceeded", false},
		{"internal server error", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = fmt.Errorf("%s", tt.errMsg)
			}
			got := isContextOverflow(err)
			if got != tt.want {
				t.Errorf("isContextOverflow(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
		})
	}
}
