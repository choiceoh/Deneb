package chat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestHandleBtw_SyncResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("btw answer", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	h := newSyncTestHandler(server, transcript)
	defer h.Close()

	text := testutil.Must(h.HandleBtw(context.Background(), "main-session", "what is 2+2?"))
	want := "btw answer" + btwResponseTag
	if text != want {
		t.Fatalf("Text = %q, want %q", text, want)
	}
}

func TestHandleBtw_SessionIsolation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("side answer", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	// Seed the main session with a message.
	testutil.NoError(t, transcript.Append("main-session", NewTextChatMessage("user", "hello", 0)))

	h := newSyncTestHandler(server, transcript)
	defer h.Close()

	testutil.Must(h.HandleBtw(context.Background(), "main-session", "side question"))

	// Main session transcript should be unchanged (1 original message only).
	msgs, total, err := transcript.Load("main-session", 0)
	testutil.NoError(t, err)
	if total != 1 {
		t.Fatalf("main session total = %d, want 1 (btw should not pollute)", total)
	}
	if msgs[0].TextContent() != "hello" {
		t.Fatalf("main session msg = %q, want original", msgs[0].TextContent())
	}

	// Ephemeral btw session should be cleaned up.
	keys, err := transcript.ListKeys()
	testutil.NoError(t, err)
	for _, k := range keys {
		if strings.HasPrefix(k, "btw:") {
			t.Fatalf("btw session %q should have been deleted", k)
		}
	}
}

func TestHandleBtw_ClonesParentTranscript(t *testing.T) {
	// Track whether the LLM request includes the parent context.
	var receivedMessages int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Count messages in the request to verify context was cloned.
		receivedMessages++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("answer with context", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	// Seed the parent session with conversation history.
	testutil.NoError(t, transcript.Append("parent-session", NewTextChatMessage("user", "my name is Alice", 0)))
	testutil.NoError(t, transcript.Append("parent-session", NewTextChatMessage("assistant", "nice to meet you Alice", 0)))

	h := newSyncTestHandler(server, transcript)
	defer h.Close()

	text := testutil.Must(h.HandleBtw(context.Background(), "parent-session", "what is my name?"))
	want := "answer with context" + btwResponseTag
	if text != want {
		t.Fatalf("Text = %q, want %q", text, want)
	}
	if receivedMessages == 0 {
		t.Fatal("expected LLM to be called")
	}
}

func TestHandleBtw_UninitializedHandler(t *testing.T) {
	h := &Handler{}
	_, err := h.HandleBtw(context.Background(), "sess-1", "hello")
	if err == nil || err.Error() != "chat handler not initialized" {
		t.Fatalf("expected initialization error, got: %v", err)
	}
}
