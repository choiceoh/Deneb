package chat

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func newSyncTestHandler(server *httptest.Server, transcript TranscriptStore) *Handler {
	sm := session.NewManager()
	broadcast := func(event string, payload any) (int, []error) { return 1, nil }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultHandlerConfig()
	cfg.LLMClient = llm.NewClient(server.URL, "test-key")
	cfg.Transcript = transcript
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "You are a test assistant."
	cfg.MaxTokens = 1024
	return NewHandler(sm, broadcast, logger, cfg)
}
func TestSendSync_UninitializedHandler(t *testing.T) {
	h := &Handler{}
	_, err := h.SendSync(context.Background(), "sess-1", "hello", "", nil)
	if err == nil || err.Error() != "chat handler not initialized" {
		t.Fatalf("expected initialization error, got: %v", err)
	}
}

func TestSendSync_UsesDefaultModelWhenRequestModelEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("sync reply", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	h := newSyncTestHandler(server, transcript)
	defer h.Close()

	result, err := h.SendSync(context.Background(), "sync-default-model", "  hello sync  ", "", nil)
	testutil.NoError(t, err)
	if result.Text != "sync reply" {
		t.Fatalf("Text = %q, want %q", result.Text, "sync reply")
	}
	if result.Model != "test-model" {
		t.Fatalf("Model = %q, want %q", result.Model, "test-model")
	}

	msgs, total, err := transcript.Load("sync-default-model", 0)
	testutil.NoError(t, err)
	if total < 1 {
		t.Fatalf("transcript total = %d, want >= 1", total)
	}
	if msgs[0].Role != "user" || msgs[0].TextContent() != "hello sync" {
		t.Fatalf("first user content = %q, want sanitized input", msgs[0].TextContent())
	}
}

func TestSendSyncStream_StreamsDeltaAndPreservesExplicitModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("stream reply", "end_turn"))
	}))
	defer server.Close()

	h := newSyncTestHandler(server, NewMemoryTranscriptStore())
	defer h.Close()

	var deltas []string
	result, err := h.SendSyncStream(
		context.Background(),
		"sync-stream",
		"hello",
		"explicit-model",
		nil,
		func(delta string) { deltas = append(deltas, delta) },
	)
	testutil.NoError(t, err)
	if result.Text != "stream reply" {
		t.Fatalf("Text = %q, want %q", result.Text, "stream reply")
	}
	if result.Model != "explicit-model" {
		t.Fatalf("Model = %q, want %q", result.Model, "explicit-model")
	}
	if !reflect.DeepEqual(deltas, []string{"stream reply"}) {
		t.Fatalf("deltas = %#v, want %#v", deltas, []string{"stream reply"})
	}
}
