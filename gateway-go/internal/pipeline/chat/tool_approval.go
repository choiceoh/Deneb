// tool_approval.go — Two-phase tool approval classification.
//
// Provides classification-based approval policies for autonomous tool execution.
// Tools are classified into three categories:
//   - "auto": safe for autonomous execution (read-only operations)
//   - "confirm": requires user confirmation in autonomous contexts (mutating operations)
//   - "block": never executed by autonomous agents (dangerous operations)
//
// In user-interactive sessions (Telegram, WebSocket), all tools execute normally.
// The approval policy is only enforced in autonomous contexts (cron, subagent).
package chat

// ToolApprovalAuto means the tool can execute without user confirmation.
const ToolApprovalAuto = "auto"

// ToolApprovalConfirm means the tool requires user confirmation.
const ToolApprovalConfirm = "confirm"

// ToolApprovalBlock means the tool is never executed autonomously.
const ToolApprovalBlock = "block"

// ToolApprovalPolicy returns the approval policy for a tool.
// Returns "auto" for tools not explicitly classified (default safe).
func ToolApprovalPolicy(toolName string) string {
	if policy, ok := toolApprovalPolicy[toolName]; ok {
		return policy
	}
	// Unclassified tools default to "auto" to avoid blocking legitimate tools
	// that were added after the classification was last updated.
	return ToolApprovalAuto
}

// IsToolAutoApproved returns true if a tool can execute without user confirmation.
func IsToolAutoApproved(toolName string) bool {
	return ToolApprovalPolicy(toolName) == ToolApprovalAuto
}

// IsToolBlocked returns true if a tool should never be executed autonomously.
func IsToolBlocked(toolName string) bool {
	return ToolApprovalPolicy(toolName) == ToolApprovalBlock
}

// ShouldConfirmTool returns true if a tool requires user confirmation
// in autonomous contexts (cron, subagent).
func ShouldConfirmTool(toolName string) bool {
	return ToolApprovalPolicy(toolName) == ToolApprovalConfirm
}

// FilterToolsForAutonomous returns a list of tool names that are safe for
// autonomous execution (auto or confirm) — excludes blocked tools.
func FilterToolsForAutonomous(toolNames []string) []string {
	var safe []string
	for _, name := range toolNames {
		if !IsToolBlocked(name) {
			safe = append(safe, name)
		}
	}
	return safe
}
