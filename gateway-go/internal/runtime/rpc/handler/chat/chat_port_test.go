package chat

import (
	"context"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type chatPortStub struct {
	runSync func(context.Context, chatport.SyncRequest) (*chatport.SyncResult, error)
}

func (s *chatPortStub) ChatReady() bool { return s != nil }

func (s *chatPortStub) RunSync(ctx context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
	if s.runSync != nil {
		return s.runSync(ctx, req)
	}
	return &chatport.SyncResult{}, nil
}

func (*chatPortStub) Send(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	return &protocol.ResponseFrame{ID: req.ID, OK: true}
}

func (*chatPortStub) History(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	return &protocol.ResponseFrame{ID: req.ID, OK: true}
}

func (*chatPortStub) Abort(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	return &protocol.ResponseFrame{ID: req.ID, OK: true}
}
func (*chatPortStub) EnqueueSteer(string, string) bool { return true }

func TestChatMethodsRejectTypedNilPort(t *testing.T) {
	var typedNil *chatPortStub
	if got := Methods(Deps{Chat: typedNil}); got != nil {
		t.Fatalf("Methods with typed nil chat = %#v", got)
	}
	if got := MiniappMethods(MiniappDeps{Chat: typedNil}); got != nil {
		t.Fatalf("MiniappMethods with typed nil chat = %#v", got)
	}
}

func TestMiniappChatSendAppliesNativeTurnDeadline(t *testing.T) {
	var remaining time.Duration
	stub := &chatPortStub{runSync: func(ctx context.Context, _ chatport.SyncRequest) (*chatport.SyncResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("native sync turn has no deadline")
		}
		remaining = time.Until(deadline)
		return &chatport.SyncResult{BestText: "ok"}, nil
	}}
	handler := MiniappMethods(MiniappDeps{Chat: stub})["miniapp.chat.send"]
	req, err := protocol.NewRequestFrame("req-1", "miniapp.chat.send", map[string]string{"message": "hello"})
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("miniapp.chat.send = %+v", resp)
	}
	if remaining < nativeSyncTurnDeadline-time.Second || remaining > nativeSyncTurnDeadline {
		t.Fatalf("native turn deadline remaining = %v, want about %v", remaining, nativeSyncTurnDeadline)
	}
}
