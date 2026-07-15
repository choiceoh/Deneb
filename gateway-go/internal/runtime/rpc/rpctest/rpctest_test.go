package rpctest

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestCallEncodesParamsAndDispatchesToHandler(t *testing.T) {
	methods := map[string]rpcutil.HandlerFunc{
		"echo": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			return rpcutil.RespondOK(req.ID, map[string]any{
				"method": req.Method,
				"params": string(req.Params),
			})
		},
	}

	resp := Call(methods, "echo", map[string]string{"value": "ok"})
	MustOK(t, resp)
	result := Result[map[string]any](t, resp)
	if result["method"] != "echo" {
		t.Fatalf("method = %v, want echo", result["method"])
	}
	if result["params"] != `{"value":"ok"}` {
		t.Fatalf("params = %v", result["params"])
	}
}

func TestCallReturnsNilForUnknownMethod(t *testing.T) {
	if got := Call(nil, "missing", nil); got != nil {
		t.Fatalf("Call() = %#v, want nil", got)
	}
}

func TestNewLoggerReturnsErrorEnabledLogger(t *testing.T) {
	logger := NewLogger()
	if logger == nil || !logger.Enabled(context.Background(), 8) {
		t.Fatal("NewLogger() must return an error-enabled logger")
	}
}
