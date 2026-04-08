package telegram

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestNewStickyDialer_DefaultStrategy(t *testing.T) {
	t.Parallel()

	d := newStickyDialer(slog.Default())
	if len(d.strategies) != 3 {
		t.Fatalf("got %d, want 3 strategies", len(d.strategies))
	}
	if d.strategies[0].name != "dual-stack" {
		t.Errorf("got %s, want first strategy dual-stack", d.strategies[0].name)
	}
	if d.strategies[1].name != "ipv4-only" {
		t.Errorf("got %s, want second strategy ipv4-only", d.strategies[1].name)
	}
	if d.strategies[2].name != "pinned-ip" {
		t.Errorf("got %s, want third strategy pinned-ip", d.strategies[2].name)
	}
}

func TestStickyDialer_ConnectsToLocalServer(t *testing.T) {
	t.Parallel()

	lc := net.ListenConfig{}
	ln := testutil.Must(lc.Listen(context.Background(), "tcp", "127.0.0.1:0"))
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	d := newStickyDialer(slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := testutil.Must(d.DialContext(ctx, "tcp", ln.Addr().String()))
	conn.Close()

	if idx := d.stickyIndex.Load(); idx != 0 {
		t.Errorf("got %d, want sticky index 0", idx)
	}
}

func TestStickyDialer_ResetSticky(t *testing.T) {
	t.Parallel()

	d := newStickyDialer(slog.Default())
	d.stickyIndex.Store(2)
	d.ResetSticky()
	if idx := d.stickyIndex.Load(); idx != 0 {
		t.Errorf("got %d, want sticky index 0 after reset", idx)
	}
}

func TestStickyDialer_PinnedIPResolveHost(t *testing.T) {
	t.Parallel()

	d := newStickyDialer(slog.Default())
	pinnedStrategy := d.strategies[2]

	if got := pinnedStrategy.resolveHost(telegramAPIHost); got != fallbackIPv4 {
		t.Errorf("got %s, want %s for %s", got, fallbackIPv4, telegramAPIHost)
	}

	if got := pinnedStrategy.resolveHost("example.com"); got != "example.com" {
		t.Errorf("got %s, want example.com", got)
	}
}

func TestNewTelegramTransport(t *testing.T) {
	t.Parallel()

	transport := NewTelegramTransport(slog.Default())
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	if transport.MaxIdleConns != maxIdleConns {
		t.Errorf("got %d, want MaxIdleConns %d", transport.MaxIdleConns, maxIdleConns)
	}
	if transport.IdleConnTimeout != keepAliveTimeout {
		t.Errorf("got %v, want IdleConnTimeout %v", transport.IdleConnTimeout, keepAliveTimeout)
	}
	if transport.ForceAttemptHTTP2 {
		t.Error("expected ForceAttemptHTTP2 to be false")
	}
}
