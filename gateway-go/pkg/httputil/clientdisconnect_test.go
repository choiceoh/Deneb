package httputil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"syscall"
	"testing"
)

func TestIsClientDisconnect(t *testing.T) {
	disconnects := []error{
		syscall.EPIPE,
		syscall.ECONNRESET,
		net.ErrClosed,
		fmt.Errorf("write tcp 127.0.0.1:18789->127.0.0.1:60250: write: broken pipe"),
		fmt.Errorf("read tcp ...: connection reset by peer"),
		fmt.Errorf("use of closed network connection"),
		fmt.Errorf("wrapped: %w", syscall.EPIPE),
	}
	for _, err := range disconnects {
		if !IsClientDisconnect(err) {
			t.Errorf("expected client-disconnect: %v", err)
		}
	}

	genuine := []error{
		nil,
		errors.New("json: unsupported value: NaN"),
		context.DeadlineExceeded,
		fmt.Errorf("marshal struct: chan field"),
	}
	for _, err := range genuine {
		if IsClientDisconnect(err) {
			t.Errorf("expected NOT a client-disconnect: %v", err)
		}
	}
}

func TestLogEncodeError_LevelRouting(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	LogEncodeError(logger, "json encode error", fmt.Errorf("write: broken pipe"))
	if got := buf.String(); !strings.Contains(got, "level=DEBUG") || !strings.Contains(got, "client disconnected") {
		t.Fatalf("broken pipe should log DEBUG: %q", got)
	}

	buf.Reset()
	LogEncodeError(logger, "json encode error", errors.New("json: unsupported value: NaN"))
	if got := buf.String(); !strings.Contains(got, "level=ERROR") {
		t.Fatalf("genuine encode failure should log ERROR: %q", got)
	}

	buf.Reset()
	LogEncodeError(logger, "x", nil)
	LogEncodeError(nil, "x", errors.New("y"))
	if buf.Len() != 0 {
		t.Fatalf("nil err / nil logger must be no-ops: %q", buf.String())
	}
}
