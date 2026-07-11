package runtimeops

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func callTool(t *testing.T, fn toolctx.ToolFunc, params any) (string, error) {
	t.Helper()
	raw := testutil.Must(json.Marshal(params))
	return fn(context.Background(), json.RawMessage(raw))
}

func mustCallTool(t *testing.T, fn toolctx.ToolFunc, params any) string {
	t.Helper()
	out := testutil.Must(callTool(t, fn, params))
	return out
}
