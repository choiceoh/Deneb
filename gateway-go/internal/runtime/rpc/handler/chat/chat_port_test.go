package chat

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type chatPortStub struct{}

func (s *chatPortStub) ChatReady() bool { return s != nil }
func (*chatPortStub) RunSync(context.Context, chatport.SyncRequest) (*chatport.SyncResult, error) {
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
	deps := Deps{Chat: typedNil}
	if got := Methods(deps); got != nil {
		t.Fatalf("Methods with typed nil chat = %#v", got)
	}
	if got := MiniappMethods(deps); got != nil {
		t.Fatalf("MiniappMethods with typed nil chat = %#v", got)
	}
}
