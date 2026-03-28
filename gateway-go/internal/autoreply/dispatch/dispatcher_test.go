package dispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

func TestReplyDispatcherSendAndComplete(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	calls := 0
	d := NewReplyDispatcher(context.Background(), func(_ context.Context, _ types.ReplyPayload, _ types.ReplyDispatchKind) error {
		calls++
		return nil
	}, logger)

	if ok := d.Send(types.ReplyPayload{Text: "hi"}, types.DispatchKindFinal); !ok {
		t.Fatal("expected first send to succeed")
	}
	d.MarkComplete()
	if ok := d.Send(types.ReplyPayload{Text: "late"}, types.DispatchKindFinal); ok {
		t.Fatal("expected send after complete to fail")
	}
	if calls != 1 {
		t.Fatalf("expected 1 deliver call, got %d", calls)
	}
	if got := d.Counts()[types.DispatchKindFinal]; got != 1 {
		t.Fatalf("expected final count=1, got %d", got)
	}
}
