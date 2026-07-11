package files

import (
	"context"
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
