package tools

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// ToolEnableCodingTools returns a tool handler that signals a mid-run upgrade
// to the full coding tool set. The actual tool-list swap happens in the agent
// executor via the OnToolsUpgrade callback; this handler just fires the signal.
func ToolEnableCodingTools() toolctx.ToolFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		if signal := toolctx.CodingUpgradeSignalFromContext(ctx); signal != nil {
			signal()
		}
		return "Coding tools enabled. You now have access to: read, write, edit, multi_edit, grep, find, tree, diff, analyze, test, git, exec, process.", nil
	}
}
