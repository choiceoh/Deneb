package chat

import (
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// fakeAppSettings is a tiny in-memory AppSettings used for handler tests.
// It records the last SetActiveHome call so assertions can confirm the
// /use-forum path actually persisted the right chat ID.
type fakeAppSettings struct {
	mu       sync.Mutex
	chatID   int64
	chatType string
	setCalls int
	failWith error
}

func (f *fakeAppSettings) SetActiveHome(chatID int64, chatType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	if f.failWith != nil {
		return f.failWith
	}
	f.chatID = chatID
	f.chatType = chatType
	return nil
}

func newTestHandlerWithAppSettings(as AppSettings) *Handler {
	h := newTestHandler()
	h.appSettings = as
	h.logger = slog.Default()
	return h
}

// TestUseForum_FromSupergroup is the happy path: the user runs /use-forum
// inside a Telegram supergroup (negative chat ID), the handler persists the
// active home and returns a success reply.
func TestUseForum_FromSupergroup(t *testing.T) {
	as := &fakeAppSettings{}
	h := newTestHandlerWithAppSettings(as)
	reply := h.handleUseForum(&DeliveryContext{To: "-1001234567890"})
	if !strings.HasPrefix(reply, "✅") {
		t.Fatalf("expected success reply, got: %q", reply)
	}
	if as.setCalls != 1 {
		t.Fatalf("SetActiveHome called %d times, want 1", as.setCalls)
	}
	if as.chatID != -1001234567890 {
		t.Fatalf("persisted chatID = %d, want -1001234567890", as.chatID)
	}
	if as.chatType != "supergroup" {
		t.Fatalf("persisted type = %q, want supergroup", as.chatType)
	}
}

// TestUseForum_FromDirectChatRefused locks in the safety check: running
// /use-forum from a 1:1 (positive chat ID) is exactly the misuse the
// migration is trying to escape. We refuse loudly rather than silently
// re-binding the bot to the chat it's supposed to leave.
func TestUseForum_FromDirectChatRefused(t *testing.T) {
	as := &fakeAppSettings{}
	h := newTestHandlerWithAppSettings(as)
	reply := h.handleUseForum(&DeliveryContext{To: "7074071666"})
	if !strings.Contains(reply, "supergroup") {
		t.Fatalf("expected supergroup-required refusal, got: %q", reply)
	}
	if as.setCalls != 0 {
		t.Fatalf("SetActiveHome should NOT be called on direct chat, got %d", as.setCalls)
	}
}

// TestUseForum_NoAppSettings covers the degraded boot path where the
// settings dir was unavailable. /use-forum should refuse cleanly with an
// explanation rather than panicking on the nil dependency.
func TestUseForum_NoAppSettings(t *testing.T) {
	h := newTestHandler() // appSettings remains nil
	reply := h.handleUseForum(&DeliveryContext{To: "-1001234567890"})
	if !strings.Contains(reply, "사용 불가") {
		t.Fatalf("expected unavailable message, got: %q", reply)
	}
}

// TestUseForum_NilDelivery — defensive check that an unexpectedly missing
// delivery context doesn't dereference nil. This mirrors what the dispatch
// path does for other slash handlers (deliverSlashResponse already tolerates
// nil delivery), so /use-forum should too.
func TestUseForum_NilDelivery(t *testing.T) {
	as := &fakeAppSettings{}
	h := newTestHandlerWithAppSettings(as)
	reply := h.handleUseForum(nil)
	if !strings.Contains(reply, "텔레그램") {
		t.Fatalf("expected telegram-required message, got: %q", reply)
	}
	if as.setCalls != 0 {
		t.Fatalf("SetActiveHome should NOT be called when delivery is nil")
	}
}
