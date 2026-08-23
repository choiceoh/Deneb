package observatory

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
)

func TestSnapshotHandlersReturnReportAndMarkdown(t *testing.T) {
	deps := Deps{StateDir: func() string { return t.TempDir() }}
	local := Methods(deps)
	if len(local) != 1 || local["observatory.snapshot"] == nil {
		t.Fatalf("local methods = %v", local)
	}

	resp := rpctest.Call(local, "observatory.snapshot", nil)
	rpctest.MustOK(t, resp)
	result := rpctest.Result[map[string]any](t, resp)
	if result["report"] == nil {
		t.Fatal("snapshot response omitted report")
	}
	if markdown, ok := result["markdown"].(string); !ok || markdown == "" {
		t.Fatalf("markdown = %#v", result["markdown"])
	}
}

func TestSnapshotAllowsNilStateDirResolver(t *testing.T) {
	resp := rpctest.Call(Methods(Deps{}), "observatory.snapshot", nil)
	rpctest.MustOK(t, resp)
}
