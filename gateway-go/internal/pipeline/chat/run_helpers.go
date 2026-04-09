package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
)

// executeAgentRunWithDelta is a variant of executeAgentRun that accepts a direct
// onDelta callback for streaming text to HTTP clients.
func executeAgentRunWithDelta(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	onDelta func(string),
	logger *slog.Logger,
) (*chatRunResult, error) {
	deltaRaw := streaming.BroadcastRawFunc(func(event string, data []byte) int {
		if onDelta == nil || event != "chat.delta" {
			return 0
		}
		var envelope struct {
			Payload struct {
				Delta string `json:"delta"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(data, &envelope); err == nil && envelope.Payload.Delta != "" {
			onDelta(envelope.Payload.Delta)
		}
		return 1
	})
	broadcaster := streaming.NewBroadcaster(deltaRaw, params.SessionKey, params.ClientRunID)
	runLog := agentlog.NewRunLogger(deps.agentLog, params.SessionKey, params.ClientRunID)
	return executeAgentRun(ctx, params, deps, broadcaster, nil, nil, logger, runLog)
}

// isContextOverflow checks if an error indicates a context window overflow.
func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "context_too_long") ||
		strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "maximum context length")
}

// resolveWorkspaceDirForPrompt returns the workspace directory for system prompt assembly.
// Reads agents.defaults.workspace / agents.list[].workspace from config,
// falling back to ~/.deneb/workspace (matching TS resolveAgentWorkspaceDir).
func resolveWorkspaceDirForPrompt() string {
	cachedWorkspaceDirOnce.Do(func() {
		snap, err := config.LoadConfigFromDefaultPath()
		if err == nil && snap != nil {
			dir := config.ResolveAgentWorkspaceDir(&snap.Config)
			if dir != "" {
				cachedWorkspaceDir = dir
				return
			}
		}
		cachedWorkspaceDir = config.ResolveAgentWorkspaceDir(nil)
	})
	return cachedWorkspaceDir
}

// memoryContextOpts returns LoadContextOptions for context file loading.
func memoryContextOpts(_ runDeps) []prompt.LoadContextOption {
	return nil
}

// deliveryChannel extracts the channel name from a delivery context.
func deliveryChannel(d *DeliveryContext) string {
	if d == nil {
		return ""
	}
	return d.Channel
}

// Definitions returns all registered tool definitions (for system prompt assembly).
func (r *ToolRegistry) Definitions() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDef, 0, len(r.order))
	for _, name := range r.order {
		defs = append(defs, r.tools[name])
	}
	return defs
}

// formatToolActivitySummary builds a compact, context-friendly summary of tool
// invocations from an agent run. Returns "" when there are no activities.
//
// The output is a plain metadata line (no brackets) that lists each unique tool
// with its call count, e.g.:
//
//	Tools used: read_file ×3, edit ×2, exec ×1
//
// IMPORTANT: Do NOT use bracket syntax here — models (especially GLM) mimic
// bracketed patterns as text output instead of making structured tool calls.
//
// This is prepended to the assistant's text before persisting to the transcript
// and Aurora store, so subsequent context assemblies include what the agent
// actually did — not just what it said.
func formatToolActivitySummary(activities []agent.ToolActivity) string {
	if len(activities) == 0 {
		return ""
	}

	// Count occurrences preserving first-seen order.
	type entry struct {
		name  string
		count int
	}
	seen := make(map[string]int) // name -> index in ordered
	var ordered []entry
	for _, a := range activities {
		if idx, ok := seen[a.Name]; ok {
			ordered[idx].count++
		} else {
			seen[a.Name] = len(ordered)
			ordered = append(ordered, entry{name: a.Name, count: 1})
		}
	}

	var sb strings.Builder
	sb.WriteString("Tools used: ")
	for i, e := range ordered {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(e.name)
		if e.count > 1 {
			fmt.Fprintf(&sb, " ×%d", e.count)
		}
	}
	return sb.String()
}

// toPromptToolDefs converts chat.ToolDef slice to the minimal prompt.ToolDef
// slice needed for system prompt assembly. Deferred tools are excluded — they
// are listed separately via DeferredSummaries in SystemPromptParams.
func toPromptToolDefs(defs []ToolDef) []prompt.ToolDef {
	out := make([]prompt.ToolDef, 0, len(defs))
	for _, d := range defs {
		if d.Deferred {
			continue
		}
		out = append(out, prompt.ToolDef{Name: d.Name})
	}
	return out
}

// extractTextContent pulls plain text from a message's content field.
// Handles both plain string and content block array formats.
func extractTextContent(raw json.RawMessage) string {
	// Try plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try content blocks.
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return string(raw)
}
