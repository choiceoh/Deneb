package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// TestImplementerSeesCodegraphFromTurnOne pins the SUPPLY half of the
// impact-first procedure end to end: allow-list + deferred pre-load.
//
// The allow-list gates three separate things (the deferred prompt listing,
// fetch_tools activation, and Execute), so a preset test on the name set alone
// would still pass while the assembled turn-1 tool array stayed empty. This
// drives the real assembly. It is the regression guard for the state found on
// 2026-08-29: codegraph configured, procedure written, and zero calls made,
// because the implementer preset never advertised the tools.
func TestImplementerSeesCodegraphFromTurnOne(t *testing.T) {
	registry := NewToolRegistry()
	noop := func(context.Context, json.RawMessage) (string, error) { return "", nil }
	// External MCP registers codegraph tools deferred (mcp_external_tools.go).
	for _, name := range []string{
		"codegraph_impact", "codegraph_node", "codegraph_callers", "codegraph_explore",
	} {
		registry.RegisterTool(toolport.ToolDef{Name: name, Description: "codegraph", Deferred: true, Fn: noop})
	}
	registry.RegisterTool(toolport.ToolDef{Name: "read", Description: "read", Fn: noop})
	registry.RegisterTool(toolport.ToolDef{Name: "unrelated_mcp_tool", Description: "x", Deferred: true, Fn: noop})

	names := map[string]bool{}
	for _, tool := range buildAgentTools(registry, "implementer", nil) {
		names[tool.Name] = true
	}
	// Pre-loaded: callable on turn 1, no fetch_tools round-trip.
	for _, want := range []string{"codegraph_impact", "codegraph_node"} {
		if !names[want] {
			t.Errorf("implementer turn-1 toolset missing pre-loaded %q", want)
		}
	}
	// Not pre-loaded, but reachable: the allow-list must admit them so
	// fetch_tools can activate them and Execute will not reject the call.
	for _, name := range []string{"codegraph_callers", "codegraph_explore"} {
		if err := checkToolPresetAllowed(name, "implementer"); err != nil {
			t.Errorf("implementer may not execute %q: %v", name, err)
		}
	}
	// An unrelated external MCP server stays out of the sandbox.
	if err := checkToolPresetAllowed("unrelated_mcp_tool", "implementer"); err == nil {
		t.Error("implementer preset should still reject unrelated external MCP tools")
	}

	// The read-only research preset keeps its narrower surface.
	for _, tool := range buildAgentTools(registry, "researcher", nil) {
		if tool.Name == "codegraph_impact" || tool.Name == "codegraph_node" {
			t.Errorf("researcher must not pre-load %q", tool.Name)
		}
	}
}
