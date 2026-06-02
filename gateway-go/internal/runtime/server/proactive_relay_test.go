package server

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// recordingTranscriptStore is a TranscriptStore fake that records Append calls.
type recordingTranscriptStore struct {
	appends map[string][]toolctx.ChatMessage
}

func newRecordingTranscriptStore() *recordingTranscriptStore {
	return &recordingTranscriptStore{appends: map[string][]toolctx.ChatMessage{}}
}

func (s *recordingTranscriptStore) Append(sessionKey string, msg toolctx.ChatMessage) error {
	s.appends[sessionKey] = append(s.appends[sessionKey], msg)
	return nil
}
func (s *recordingTranscriptStore) Load(string, int) ([]toolctx.ChatMessage, int, error) {
	return nil, 0, nil
}
func (s *recordingTranscriptStore) Delete(string) error         { return nil }
func (s *recordingTranscriptStore) ListKeys() ([]string, error) { return nil, nil }
func (s *recordingTranscriptStore) Search(string, int) ([]toolctx.SearchResult, error) {
	return nil, nil
}
func (s *recordingTranscriptStore) CloneRecent(string, string, int) error { return nil }

// TestRelay verifies that relay() always delivers to the native 업무 session
// (client:main) plus a live push, regardless of the session key argument.
func TestRelay(t *testing.T) {
	store := newRecordingTranscriptStore()
	hub := newClientPushHub()
	events, unsub := hub.subscribe()
	defer unsub()

	d := proactiveRelayDeps{
		transcriptStore: store,
		pushHub:         hub,
	}

	delivered, err := d.relay(context.Background(), "ignored-session-key", "📬 업무 리포트 본문")
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if !delivered {
		t.Fatal("relay should report delivered when transcript store is wired")
	}

	got := store.appends[nativeWorkSessionKey]
	if len(got) != 1 {
		t.Fatalf("want 1 append to %q, got %d (all keys: %v)", nativeWorkSessionKey, len(got), store.appends)
	}
	if got[0].Role != "assistant" {
		t.Errorf("appended role = %q, want assistant", got[0].Role)
	}
	for k := range store.appends {
		if strings.HasPrefix(k, "telegram:") {
			t.Errorf("relay must not write a telegram session, wrote %q", k)
		}
	}

	select {
	case ev := <-events:
		if ev.Title != "Deneb" {
			t.Errorf("push title = %q, want Deneb", ev.Title)
		}
	default:
		t.Error("expected a live push event, got none")
	}
}
