package minibind

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type bindParams struct {
	Value string `json:"value"`
}

func authenticatedContext() context.Context {
	return clientauth.WithContext(context.Background(), &clientauth.Identity{})
}

func TestBindContracts(t *testing.T) {
	called := false
	handler := Bind[bindParams](func(_ context.Context, req *protocol.RequestFrame, p bindParams) *protocol.ResponseFrame {
		called = true
		return rpcutil.RespondOK(req.ID, p)
	})

	t.Run("unauthorized precedes invalid params", func(t *testing.T) {
		called = false
		resp := handler(context.Background(), &protocol.RequestFrame{ID: "1", Params: []byte(`{"value":`)})
		if resp.OK || resp.Error.Code != protocol.ErrUnauthorized || called {
			t.Fatalf("response=%+v called=%v, want auth rejection", resp, called)
		}
	})

	t.Run("invalid params", func(t *testing.T) {
		called = false
		resp := handler(authenticatedContext(), &protocol.RequestFrame{ID: "2", Params: []byte(`{"value":`)})
		if resp.OK || resp.Error.Code != protocol.ErrInvalidRequest || called {
			t.Fatalf("response=%+v called=%v, want params rejection", resp, called)
		}
	})

	t.Run("success", func(t *testing.T) {
		called = false
		req, err := protocol.NewRequestFrame("3", "miniapp.test", bindParams{Value: "ok"})
		if err != nil {
			t.Fatal(err)
		}
		if resp := handler(authenticatedContext(), req); !resp.OK || !called {
			t.Fatalf("response=%+v called=%v", resp, called)
		}
	})
}

func TestBindOptionalAllowsEmptyParams(t *testing.T) {
	handler := BindOptional[bindParams](func(_ context.Context, req *protocol.RequestFrame, p bindParams) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, p)
	})
	if resp := handler(authenticatedContext(), &protocol.RequestFrame{ID: "4"}); !resp.OK {
		t.Fatalf("response=%+v, want success", resp)
	}
}
