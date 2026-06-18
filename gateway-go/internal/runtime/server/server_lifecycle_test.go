package server

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestStartAndListenMarksBoundAddrAndCallsOnListening(t *testing.T) {
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
