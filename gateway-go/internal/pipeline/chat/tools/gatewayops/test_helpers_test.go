package gatewayops

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func callTool(t *testing.T, fn toolport.ToolFunc, params any) (string, error) {
	t.Helper()
	raw := testutil.Must(json.Marshal(params))
	return fn(context.Background(), json.RawMessage(raw))
}

func mustCallTool(t *testing.T, fn toolport.ToolFunc, params any) string {
	t.Helper()
	out := testutil.Must(callTool(t, fn, params))
	return out
}
