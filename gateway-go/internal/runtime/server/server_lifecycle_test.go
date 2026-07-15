package server

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestStartAndListenMarksBoundAddrAndCallsOnListening(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newTestServer(t)
	defer s.Close(context.Background()) //nolint:errcheck // best-effort test cleanup

	listening := make(chan string, 1)
	s.OnListening = func(addr net.Addr) {
		if addr != nil {
			listening <- addr.String()
		}
	}

	addr, err := s.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}

	if got := s.BoundAddr(); got != addr.String() {
		t.Fatalf("BoundAddr = %q, want %q", got, addr.String())
	}

	select {
	case got := <-listening:
		if got != addr.String() {
			t.Fatalf("OnListening addr = %q, want %q", got, addr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnListening was not called")
	}
}

func TestStartAndListenParentCancelDoesNotAbortDetachedRuntimeContext(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	s := newTestServer(t)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	if _, err := s.StartAndListen(ctx); err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	cancel()

	select {
	case <-s.ShutdownCtx().Done():
		t.Fatal("parent cancellation reached detached runtime context before graceful drain")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestChatDrainRejectsNewRunsWithRuntimeError(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	s := newTestServer(t)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	if err := s.chatHandler.BeginDrain(context.Background()); err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	if s.chatHandler.ChatReady() {
		t.Fatal("draining chat handler still reported ready")
	}
	if _, err := s.chatHandler.SendSync(context.Background(), "client:main", "hello", "", nil); !errors.Is(err, chat.ErrRuntimeDraining) {
		t.Fatalf("SendSync error = %v, want ErrRuntimeDraining", err)
	}
	req, err := protocol.NewRequestFrame("req-1", "sessions.send", map[string]string{
		"key": "client:main", "message": "replace the active turn",
	})
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	resp := s.chatHandler.SessionsSend(context.Background(), req)
	if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("SessionsSend while draining = %+v, want UNAVAILABLE", resp)
	}
}
