package chat

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	chattranscript "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

func TestWireStreamHooksPersistsCompletedToolResultReceipt(t *testing.T) {
	t.Parallel()
	store := chattranscript.NewMemoryTranscriptStore()
	fixedNow := time.UnixMilli(1_700_000_000_123)
	deps := runDeps{
		transcript:  store,
		semanticNow: func() time.Time { return fixedNow },
	}
	var compositor agent.HookCompositor
	wireStreamHooks(&compositor, RunParams{SessionKey: "client:receipt"}, deps, nil, nil)
	hooks := compositor.Build()
	if hooks.OnToolResult == nil {
		t.Fatal("tool result receipt hook was not registered")
	}

	hooks.OnToolResult("read", "tool-1", "completed output", false)
	receipts, err := store.LoadToolResultReceipts("client:receipt")
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 {
		t.Fatalf("receipts = %d, want 1", len(receipts))
	}
	got := receipts[0]
	if got.ToolUseID != "tool-1" || got.ToolName != "read" || got.Content != "completed output" {
		t.Fatalf("receipt = %+v", got)
	}
	if got.CompletedAt != fixedNow.UnixMilli() {
		t.Fatalf("completedAt = %d, want %d", got.CompletedAt, fixedNow.UnixMilli())
	}
}

func TestPerTurnToolResultPersistenceCleansRecoveryReceipts(t *testing.T) {
	t.Parallel()
	store := chattranscript.NewMemoryTranscriptStore()
	const sessionKey = "client:committed"
	if err := store.AppendToolResultReceipt(sessionKey, chatport.ToolResultReceipt{
		ToolUseID: "tool-1", ToolName: "read", Content: "completed output",
	}); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	persist := buildMessagePersister(runDeps{transcript: store}, RunParams{SessionKey: sessionKey}, logger)
	if persist == nil {
		t.Fatal("message persister is nil")
	}
	persist(llm.NewBlockMessage("user", []llm.ContentBlock{{
		Type: "tool_result", ToolUseID: "tool-1", Content: "completed output",
	}}))

	receipts, err := store.LoadToolResultReceipts(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 0 {
		t.Fatalf("receipts remain after canonical tool_result persist: %+v", receipts)
	}
	messages, total, err := store.Load(sessionKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("persisted messages = %+v (total %d)", messages, total)
	}
}
