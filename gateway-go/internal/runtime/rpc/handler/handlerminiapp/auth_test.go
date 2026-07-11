package handlerminiapp

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type authBindParams struct {
	Value string `json:"value"`
}

func TestBindAuthenticatedContracts(t *testing.T) {
	called := false
	handler := bindAuthenticated[authBindParams](func(_ context.Context, req *protocol.RequestFrame, p authBindParams) *protocol.ResponseFrame {
		called = true
		return rpcutil.RespondOK(req.ID, p)
	})

	t.Run("unauthorized precedes invalid params", func(t *testing.T) {
		called = false
		req := &protocol.RequestFrame{ID: "auth-1", Params: []byte(`{"value":`)}
		resp := handler(context.Background(), req)
		if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
			t.Fatalf("response = %+v, want UNAUTHORIZED", resp)
		}
		if called {
			t.Fatal("handler ran before authentication")
		}
	})

	t.Run("authenticated invalid params", func(t *testing.T) {
		called = false
		req := &protocol.RequestFrame{ID: "auth-2", Params: []byte(`{"value":`)}
		resp := handler(authedCtx(), req)
		if resp.OK || resp.Error.Code != protocol.ErrInvalidRequest {
			t.Fatalf("response = %+v, want INVALID_REQUEST", resp)
		}
		if called {
			t.Fatal("handler ran with invalid params")
		}
	})

	t.Run("success", func(t *testing.T) {
		called = false
		resp := handler(authedCtx(), reqWith(t, "miniapp.test", authBindParams{Value: "ok"}))
		if !resp.OK || !called {
			t.Fatalf("response = %+v, called = %v", resp, called)
		}
	})
}

func TestBindAuthenticatedOptionalAllowsEmptyParams(t *testing.T) {
	handler := bindAuthenticatedOptional[authBindParams](func(_ context.Context, req *protocol.RequestFrame, p authBindParams) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, p)
	})
	resp := handler(authedCtx(), &protocol.RequestFrame{ID: "auth-optional"})
	if !resp.OK {
		t.Fatalf("response = %+v, want success", resp)
	}
}
