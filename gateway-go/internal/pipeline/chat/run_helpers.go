package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/pkg/llmerr"
)

// ToolStreamEvent is one tool lifecycle transition surfaced to a streaming
// transport. State is "started" or "completed"; Detail (started only) is a
// short human hint extracted from the tool input (query, command, file name);
// IsError (completed only) marks a tool that returned an error result.
type ToolStreamEvent struct {
	State     string
	Tool      string
	ToolUseID string
	Detail    string
	IsError   bool
}

// streamEventSinks carries the per-event callbacks a streaming HTTP transport
// (the miniapp SSE bridge) registers for one chat turn. All fields are
// optional; nil callbacks drop their events.
type streamEventSinks struct {
	// OnDelta receives each assistant text chunk.
	OnDelta func(delta string)
	// OnTool receives tool lifecycle transitions so the client can show live
	// tool progress in its waiting indicator.
	OnTool func(ev ToolStreamEvent)
	// OnThinking signals reasoning-in-progress (throttled by the broadcaster).
	// preview is a chip-sized tail of the recent reasoning text ("" when the
	// broadcaster has nothing readable yet).
	OnThinking func(preview string)
}

// executeAgentRunWithDelta is a variant of executeAgentRun that forwards the
// run's broadcast stream (text deltas, tool lifecycle, thinking liveness) to
// direct callbacks for streaming HTTP clients.
func executeAgentRunWithDelta(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	sinks streamEventSinks,
	logger *slog.Logger,
) (*chatRunResult, error) {
	deltaRaw := streaming.BroadcastRawFunc(func(event string, data []byte) int {
		switch event {
		case streaming.EventDelta:
			if sinks.OnDelta == nil {
				return 0
			}
			var envelope struct {
				Payload struct {
					Delta string `json:"delta"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(data, &envelope); err == nil && envelope.Payload.Delta != "" {
				sinks.OnDelta(envelope.Payload.Delta)
			}
		case streaming.EventTool:
			if sinks.OnTool == nil {
				return 0
			}
			var envelope struct {
				Payload struct {
					State     string `json:"state"`
					Tool      string `json:"tool"`
					ToolUseID string `json:"toolUseId"`
					Detail    string `json:"detail"`
					IsError   bool   `json:"isError"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(data, &envelope); err == nil && envelope.Payload.Tool != "" {
				sinks.OnTool(ToolStreamEvent{
					State:     envelope.Payload.State,
					Tool:      envelope.Payload.Tool,
					ToolUseID: envelope.Payload.ToolUseID,
					Detail:    envelope.Payload.Detail,
					IsError:   envelope.Payload.IsError,
				})
			}
		case streaming.EventThinking:
			if sinks.OnThinking == nil {
				return 0
			}
			var envelope struct {
				Payload struct {
					Preview string `json:"preview"`
				} `json:"payload"`
			}
			// Best-effort: a parse failure still delivers the liveness pulse,
			// just without the preview text.
			_ = json.Unmarshal(data, &envelope)
			sinks.OnThinking(envelope.Payload.Preview)
		default:
			return 0
		}
		return 1
	})
	broadcaster := streaming.NewBroadcaster(deltaRaw, params.SessionKey, params.ClientRunID)
	runLog := agentlog.NewRunLogger(deps.agentLog, params.SessionKey, params.ClientRunID)
	return executeAgentRun(ctx, params, deps, broadcaster, nil, nil, logger, runLog)
}

// classifyLLMError runs llmerr.Classify against an error, lifting the HTTP
// status out of any wrapped *httpretry.APIError so the classifier's status
// pipeline (not just its message patterns) is engaged. Without this,
// errors like "API error 502: bad gateway" would fall through to
// ReasonUnknown because llmerr.Classify intentionally does not match bare
// digits inside a message.
func classifyLLMError(err error) llmerr.Classified {
	var apiErr *httpretry.APIError
	status := 0
	var body []byte
	if errors.As(err, &apiErr) {
		status = apiErr.StatusCode
		if apiErr.Message != "" {
			body = []byte(apiErr.Message)
		}
	}
	return llmerr.Classify(err, status, body)
}

// isContextOverflow reports whether an error indicates a context window
// overflow. Backed by the shared llmerr classifier, so it covers a much
// wider pattern set (OpenAI, Anthropic, Gemini, vLLM, Ollama, llama.cpp,
// AWS Bedrock) than the previous hand-rolled substring list, plus
// structured error codes and large-session transport disconnects.
//
// Behavior is strictly more correct than the prior substring check — every
// pattern the old implementation matched is also covered by
// llmerr.ReasonContextOverflow.
func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	return classifyLLMError(err).Reason == llmerr.ReasonContextOverflow
}

// isTransientLLMError reports whether an error is a retryable transient
// failure that a single short-backoff retry can plausibly recover from.
//
// Backed by llmerr.Classify so it shares one taxonomy with
// isContextOverflow and the autoreply classifier. The set is intentionally
// narrower than llmerr.Reason.Retryable(): we whitelist only the reasons
// the pre-migration IsTransientError string match used to catch (HTTP
// 500/502/503/521/529/429), plus transport-level timeouts which the old
// code missed. ReasonUnknown is excluded so the caller doesn't burn a
// retry on genuinely unclassifiable errors; ReasonContextOverflow is
// handled by a separate compaction path upstream.
func isTransientLLMError(err error) bool {
	if err == nil {
		return false
	}
	switch classifyLLMError(err).Reason {
	case llmerr.ReasonServerError,
		llmerr.ReasonOverloaded,
		llmerr.ReasonRateLimit,
		llmerr.ReasonTimeout:
		return true
	default:
		return false
	}
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
