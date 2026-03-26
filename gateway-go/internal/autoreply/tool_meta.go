package autoreply

import "strings"

// ToolInvocation represents metadata about a tool invocation during agent execution.
type ToolInvocation struct {
	Name     string `json:"name"`
	ID       string `json:"id,omitempty"`
	Input    string `json:"input,omitempty"`
	Output   string `json:"output,omitempty"`
	IsError  bool   `json:"isError,omitempty"`
	Duration int64  `json:"durationMs,omitempty"`
}

// ToolMeta tracks tool invocations during an agent run.
type ToolMeta struct {
	Invocations []ToolInvocation
}

// NewToolMeta creates a new tool metadata tracker.
func NewToolMeta() *ToolMeta {
	return &ToolMeta{}
}

// Record adds a tool invocation to the tracker.
func (tm *ToolMeta) Record(inv ToolInvocation) {
	tm.Invocations = append(tm.Invocations, inv)
}

// Count returns the total number of tool invocations.
func (tm *ToolMeta) Count() int {
	return len(tm.Invocations)
}

// HasTool returns true if a tool with the given name was invoked.
func (tm *ToolMeta) HasTool(name string) bool {
	for _, inv := range tm.Invocations {
		if inv.Name == name {
			return true
		}
	}
	return false
}

// ErrorCount returns the number of tool invocations that resulted in errors.
func (tm *ToolMeta) ErrorCount() int {
	count := 0
	for _, inv := range tm.Invocations {
		if inv.IsError {
			count++
		}
	}
	return count
}

// ToolNames returns a deduplicated list of tool names used.
func (tm *ToolMeta) ToolNames() []string {
	seen := make(map[string]bool)
	var names []string
	for _, inv := range tm.Invocations {
		if !seen[inv.Name] {
			seen[inv.Name] = true
			names = append(names, inv.Name)
		}
	}
	return names
}

// Summary returns a brief summary of tool usage.
func (tm *ToolMeta) Summary() string {
	if len(tm.Invocations) == 0 {
		return ""
	}
	names := tm.ToolNames()
	return strings.Join(names, ", ")
}
