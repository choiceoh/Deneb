package miniapp

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

func TestMethodsRejectTypedNilPort(t *testing.T) {
	var typedNil *chatPortStub
	if got := Methods(Deps{Chat: typedNil}); got != nil {
		t.Fatalf("Methods with typed nil chat = %#v", got)
	}
}

func TestChatSendAppliesNativeTurnDeadline(t *testing.T) {
	var remaining time.Duration
	var captured chatport.SyncRequest
	stub := &chatPortStub{runSync: func(ctx context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
		captured = req
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("native sync turn has no deadline")
		}
		remaining = time.Until(deadline)
		return &chatport.SyncResult{BestText: "ok"}, nil
	}}
	handler := Methods(Deps{Chat: stub})["miniapp.chat.send"]
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
	if !captured.TrustedDirectUserInput || !captured.GateUntrustedTools {
		t.Fatalf("native chat provenance = trusted:%v gate:%v, want both true", captured.TrustedDirectUserInput, captured.GateUntrustedTools)
	}
}

func TestUntrustedCaptureDoesNotClaimDirectUserProvenance(t *testing.T) {
	req := untrustedCaptureRequest("client:main", "captured text")
	if !req.GateUntrustedTools {
		t.Fatal("capture must retain the promptware gate")
	}
	if req.TrustedDirectUserInput {
		t.Fatal("capture claimed direct-user provenance")
	}
}
