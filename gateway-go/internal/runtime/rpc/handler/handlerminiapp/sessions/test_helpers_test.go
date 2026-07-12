package sessions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func authedCtx() context.Context {
	return clientauth.WithContext(context.Background(), &clientauth.Identity{
		User: &clientauth.User{ID: 42, FirstName: "Tester"},
	})
}

func reqWith(t *testing.T, method string, params any) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("t-1", method, params)
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	return req
}

func decode(t *testing.T, resp *protocol.ResponseFrame, dest any) {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
	}
	if !resp.OK {
		t.Fatalf("response not OK: code=%s message=%s", resp.Error.Code, resp.Error.Message)
	}
	if err := json.Unmarshal(resp.Payload, dest); err != nil {
		t.Fatalf("decode payload: %v (raw=%s)", err, string(resp.Payload))
	}
}
