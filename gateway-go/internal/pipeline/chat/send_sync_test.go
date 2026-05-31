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

// fakeTopicResolver implements TopicResolver for prepareSyncRun tests.
type fakeTopicResolver struct{ rev map[string]string }

func (f fakeTopicResolver) TopicKey(threadID string) string {
	for k, tid := range f.rev {
		if tid == threadID {
			return k
		}
	}
	return ""
}
func (f fakeTopicResolver) ThreadIDForKey(key string) (string, bool) {
	tid, ok := f.rev[key]
	return tid, ok
}
func (f fakeTopicResolver) Dir() string { return "topics" }

func TestPrepareSyncRun_TopicKeyResolvesToThreadID(t *testing.T) {
	sm := session.NewManager()
	broadcast := func(string, any) (int, []error) { return 0, nil }
	cfg := DefaultHandlerConfig()
	cfg.TopicResolver = fakeTopicResolver{rev: map[string]string{"코딩": "42", "업무": "0"}}
	h := NewHandler(sm, broadcast, nil, cfg)
	defer h.Close()

	// Known key → threadID stamped onto a per-request Delivery copy.
	orig := &DeliveryContext{Channel: "client", To: "client:topic:코딩"}
	params, _, err := h.prepareSyncRun("client:topic:코딩", "hi", "", "test", &SyncOptions{
		Delivery: orig,
		TopicKey: "코딩",
	})
	testutil.NoError(t, err)
	if params.Delivery == nil || params.Delivery.ThreadID != "42" {
		t.Fatalf("Delivery = %+v, want ThreadID=42", params.Delivery)
	}
	if orig.ThreadID != "" {
		t.Errorf("caller's Delivery was mutated: ThreadID=%q (must copy, not mutate)", orig.ThreadID)
	}

	// Unknown key → no stamp, so a stale client topic can't mis-inject General.
	params2, _, err := h.prepareSyncRun("client:main", "hi", "", "test", &SyncOptions{
		Delivery: &DeliveryContext{Channel: "client", To: "client:main"},
		TopicKey: "stale-topic",
	})
	testutil.NoError(t, err)
	if params2.Delivery != nil && params2.Delivery.ThreadID != "" {
		t.Errorf("unknown key stamped ThreadID=%q, want empty", params2.Delivery.ThreadID)
	}

	// No topicKey → Delivery untouched (legacy untopiced send).
	params3, _, err := h.prepareSyncRun("client:main", "hi", "", "test", &SyncOptions{
		Delivery: &DeliveryContext{Channel: "client", To: "client:main"},
	})
	testutil.NoError(t, err)
	if params3.Delivery.ThreadID != "" {
		t.Errorf("no topicKey stamped ThreadID=%q, want empty", params3.Delivery.ThreadID)
	}
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

	result := testutil.Must(h.SendSync(context.Background(), "sync-default-model", "  hello sync  ", "", nil))
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
	// Transcript user messages carry a leading "[<RFC3339 ts>] " prefix
	// (see executeAgentRun); strip when comparing to raw input.
	if msgs[0].Role != "user" || StripUserMessageTimestamp(msgs[0].TextContent()) != "hello sync" {
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
