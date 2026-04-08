package dispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

func TestReplyDispatcherSendAndComplete(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	calls := 0
	d := NewReplyDispatcher(func(_ context.Context, _ types.ReplyPayload, _ types.ReplyDispatchKind) error {
		calls++
		return nil
	}, logger)

	ctx := context.Background()
	if ok := d.Send(ctx, types.ReplyPayload{Text: "hi"}, types.DispatchKindFinal); !ok {
		t.Fatal("expected first send to succeed")
	}
	d.MarkComplete()
	if ok := d.Send(ctx, types.ReplyPayload{Text: "late"}, types.DispatchKindFinal); ok {
		t.Fatal("expected send after complete to fail")
	}
	if calls != 1 {
		t.Fatalf("got %d, want 1 deliver call", calls)
	}
	if got := d.Counts()[types.DispatchKindFinal]; got != 1 {
		t.Fatalf("got %d, want final count=1", got)
	}
}
