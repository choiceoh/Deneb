package bridge

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/goals"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/codeaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/fetchops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/runtimeops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
)

// RegistryBridge is the ToolRegistry surface needed to register tools that dial
// back into the same registry (fetch_tools activation + code_action bridge).
type RegistryBridge interface {
	toolport.ToolRegistrar
	fetchops.FetchToolsRegistry
	codeaction.ToolInvoker
}

// RegisterRegistryBridgeTools registers fetch_tools and code_action. Both need
// the concrete registry (not just ToolRegistrar) so they can activate deferred
// tools / invoke sibling tools mid-turn.
func RegisterRegistryBridgeTools(registry RegistryBridge, deps *tooldeps.CoreToolDeps) {
	// Deferred tool activation: fetch_tools lets the LLM load schemas on demand.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "fetch_tools",
		Description: "Load full schemas for deferred tools so you can call them. Use names (exact) or query (keyword search). The activated tools become available on the next turn",
		InputSchema: schema.FetchToolsToolSchema(),
		Fn:          fetchops.ToolFetchToolsWithReranker(registry, deps.FetchToolsEmbedder, deps.FetchToolsReranker),
	})

	// code_action (CodeAct): the model writes Python to orchestrate several
	// read-only tools / batch-process data in one turn. Eager (2026-06-17):
	// batching N read/grep/calendar/wiki ops into one Python turn collapses N
	// tool-loop steps into 1, and multi-step tool turns are decode-bound (each
	// step pays full thinking decode), so fewer steps is the main latency lever
	// — worth the ~300-450 prompt tokens/turn for its schema.
	// Main-only is preserved independently: absent from toolpreset, so restricted
	// sub-agents cannot reach this primitive (the preset allowlist gates them
	// regardless of eager/deferred).
	registry.RegisterTool(toolport.ToolDef{
		Name:        "code_action",
		Description: codeaction.CodeActionDescription,
		InputSchema: codeaction.CodeActionSchema(),
		Fn: codeaction.ToolCodeAction(codeaction.CodeActionDeps{
			Invoker:  registry,
			Contacts: deps.Contacts.Store, // structured deneb.contacts(as_json=True); nil-safe
			Calendar: &deps.Calendar,      // structured deneb.calendar(as_json=True)
			Wiki:     deps.Wiki.Store,     // structured deneb.wiki(as_json=True); nil-safe
		}),
	})
}

// ExecCommandPreservesRunCache reports whether an exec command is provably
// read-only for run-cache invalidation. Re-exported so chat.ToolRegistry does
// not import tools/runtimeops solely for this predicate.
func ExecCommandPreservesRunCache(command string) bool {
	return runtimeops.ExecCommandPreservesRunCache(command)
}

// SubstituteMarketLetterTokens replaces {{market:*}} letter tokens with cached
// display values. Re-exported so chat run paths do not import domain/market.
func SubstituteMarketLetterTokens(s string) string {
	return market.SubstituteLetterTokens(s)
}

// ClearStandingGoal stops any active standing goal for sessionKey (used by
// /reset). No-op when the goals store is not wired.
func ClearStandingGoal(sessionKey string) {
	if gs := goals.Default(); gs != nil {
		gs.Clear(sessionKey)
	}
}

// FetchToolsSchema returns the fetch_tools schema for external registration.
func FetchToolsSchema() map[string]any { return schema.FetchToolsToolSchema() }
