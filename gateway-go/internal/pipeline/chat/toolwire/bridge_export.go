package toolwire

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/bridge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/wire"
)

// RegistryBridge is the ToolRegistry surface needed for fetch_tools / code_action.
type RegistryBridge = bridge.RegistryBridge

// RegisterRegistryBridgeTools registers fetch_tools and code_action.
func RegisterRegistryBridgeTools(registry RegistryBridge, deps *wire.CoreToolDeps) {
	bridge.RegisterRegistryBridgeTools(registry, deps)
}

func ExecCommandPreservesRunCache(command string) bool {
	return bridge.ExecCommandPreservesRunCache(command)
}

func SumVllmPrefixCacheCounters(ctx context.Context, bases []string, model string) (queries, hits int64, ok bool) {
	return bridge.SumVllmPrefixCacheCounters(ctx, bases, model)
}

func SubstituteMarketLetterTokens(s string) string {
	return bridge.SubstituteMarketLetterTokens(s)
}

func ClearStandingGoal(sessionKey string) {
	bridge.ClearStandingGoal(sessionKey)
}

func FetchToolsSchema() map[string]any {
	return bridge.FetchToolsSchema()
}
