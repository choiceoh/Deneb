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
		t.Fatalf("expected 3 strategies, got %d", len(d.strategies))
	}
	if d.strategies[0].name != "dual-stack" {
		t.Errorf("expected first strategy dual-stack, got %s", d.strategies[0].name)
	}
	if d.strategies[1].name != "ipv4-only" {
		t.Errorf("expected second strategy ipv4-only, got %s", d.strategies[1].name)
	}
	if d.strategies[2].name != "pinned-ip" {
		t.Errorf("expected third strategy pinned-ip, got %s", d.strategies[2].name)
	}
}

func TestStickyDialer_ConnectsToLocalServer(t *testing.T) {
	t.Parallel()

	lc := net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	testutil.NoError(t, err)
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

	conn, err := d.DialContext(ctx, "tcp", ln.Addr().String())
	testutil.NoError(t, err)
	conn.Close()

	if idx := d.stickyIndex.Load(); idx != 0 {
		t.Errorf("expected sticky index 0, got %d", idx)
	}
}

func TestStickyDialer_ResetSticky(t *testing.T) {
	t.Parallel()

	d := newStickyDialer(slog.Default())
	d.stickyIndex.Store(2)
	d.ResetSticky()
	if idx := d.stickyIndex.Load(); idx != 0 {
		t.Errorf("expected sticky index 0 after reset, got %d", idx)
	}
}

func TestStickyDialer_PinnedIPResolveHost(t *testing.T) {
	t.Parallel()

	d := newStickyDialer(slog.Default())
	pinnedStrategy := d.strategies[2]

	if got := pinnedStrategy.resolveHost(telegramAPIHost); got != fallbackIPv4 {
		t.Errorf("expected %s for %s, got %s", fallbackIPv4, telegramAPIHost, got)
	}

	if got := pinnedStrategy.resolveHost("example.com"); got != "example.com" {
		t.Errorf("expected example.com, got %s", got)
	}
}

func TestNewTelegramTransport(t *testing.T) {
	t.Parallel()

	transport := NewTelegramTransport(slog.Default())
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	if transport.MaxIdleConns != maxIdleConns {
		t.Errorf("expected MaxIdleConns %d, got %d", maxIdleConns, transport.MaxIdleConns)
	}
	if transport.IdleConnTimeout != keepAliveTimeout {
		t.Errorf("expected IdleConnTimeout %v, got %v", keepAliveTimeout, transport.IdleConnTimeout)
	}
	if transport.ForceAttemptHTTP2 {
		t.Error("expected ForceAttemptHTTP2 to be false")
	}
}
