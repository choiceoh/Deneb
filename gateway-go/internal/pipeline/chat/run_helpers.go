package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/leafbind"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/chatportwire"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
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
	// ResultSummary is the gateway-owned one-line digest of a completed call
	// (see tool_result_summary.go). Empty on `started` frames and whenever the
	// result carries nothing worth showing.
	ResultSummary string
	// ResultPreview is the bounded, readable head of the same result — what the
	// chip shows when the user expands it.
	ResultPreview string
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
	// OnReasoning carries the full reasoning-so-far (throttled with OnThinking) so
	// the client can grow a live expandable reasoning block. Empty until reasoning
	// streams; nil when the transport doesn't want it.
	OnReasoning func(full string)
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
					Result    string `json:"result"`
					IsError   bool   `json:"isError"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(data, &envelope); err == nil && envelope.Payload.Tool != "" {
				sinks.OnTool(ToolStreamEvent{
					State:         envelope.Payload.State,
					Tool:          envelope.Payload.Tool,
					ToolUseID:     envelope.Payload.ToolUseID,
					Detail:        envelope.Payload.Detail,
					IsError:       envelope.Payload.IsError,
					ResultSummary: toolport.SummarizeToolResult(envelope.Payload.Result),
					ResultPreview: toolport.ToolResultPreview(envelope.Payload.Result),
				})
			}
		case streaming.EventThinking:
			if sinks.OnThinking == nil && sinks.OnReasoning == nil {
				return 0
			}
			var envelope struct {
				Payload struct {
					Preview       string `json:"preview"`
					ReasoningFull string `json:"reasoningFull"`
				} `json:"payload"`
			}
			// Best-effort: a parse failure still delivers the liveness pulse,
			// just without the preview text.
			_ = json.Unmarshal(data, &envelope)
			if sinks.OnThinking != nil {
				sinks.OnThinking(envelope.Payload.Preview)
			}
			if sinks.OnReasoning != nil && envelope.Payload.ReasoningFull != "" {
				sinks.OnReasoning(envelope.Payload.ReasoningFull)
			}
		default:
			return 0
		}
		return 1
	})
	broadcaster := streaming.NewBroadcaster(deltaRaw, params.SessionKey, params.ClientRunID)
	broadcaster.SetThinkingSummarizer(newThinkingSummarizer(ctx))
	runLog := agentlog.NewRunLogger(deps.agentLog, params.SessionKey, params.ClientRunID)
	return executeAgentRun(ctx, params, deps, broadcaster, nil, logger, runLog)
}

// classifyLLMError runs llmerr.Classify against an error, lifting the HTTP
// status out of any wrapped *httpretry.APIError so the classifier's status
// pipeline (not just its message patterns) is engaged. Without this,
// errors like "API error 502: bad gateway" would fall through to
// ReasonUnknown because llmerr.Classify intentionally does not match bare
// digits inside a message.
func classifyLLMError(err error) llmerr.Classified {
	return chatportwire.Classify(err)
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

// shouldStripThinking reports whether an error is an Anthropic-style
// thinking-block signature rejection whose classified recovery is to drop
// thinking blocks from the history and retry once (llmerr.ReasonThinkingSignature
// -> Action.StripThink). Backed by the shared llmerr classifier so the
// decision uses the same taxonomy as isContextOverflow / isTransientLLMError;
// reading the Action keeps the recovery matrix (llmerr/action.go) the single
// source of truth for what each reason warrants.
func shouldStripThinking(err error) bool {
	if err == nil {
		return false
	}
	return classifyLLMError(err).Reason.DefaultAction(1).StripThink
}

// resolveWorkspaceDirForPrompt returns the workspace directory for system prompt assembly.
// Reads agents.defaults.workspace / agents.list[].workspace from config,
// falling back to ~/.deneb/workspace (matching TS resolveAgentWorkspaceDir).
func resolveWorkspaceDirForPrompt() string {
	cachedWorkspaceDirOnce.Do(func() {
		snap, err := leafbind.LoadConfigFromDefaultPath()
		if err == nil && snap != nil {
			dir := leafbind.ResolveAgentWorkspaceDir(&snap.Config)
			if dir != "" {
				cachedWorkspaceDir = dir
				return
			}
		}
		cachedWorkspaceDir = leafbind.ResolveAgentWorkspaceDir(nil)
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

// sessionFallbackChannel infers the prompt-facing channel for runs that have
// no DeliveryContext but piggyback on a client session (heartbeat, boot).
// Keeping the channel equal to the session's interactive turns keeps the
// system prompt byte-identical across both run families — one vLLM APC
// prefix instead of two. Only client sessions map; automation sessions
// (cron:, system:) keep "" so their prompts are unchanged.
func sessionFallbackChannel(sessionKey string) string {
	if session.IsClientSession(sessionKey) {
		return "client"
	}
	return ""
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

// formatToolHist renders the per-turn tool-call histogram as a compact
// "name:count,name:count" string ordered by descending count (ties broken by
// name) for the delivery/audit card. Empty counts yield "".
func formatToolHist(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	type kv struct {
		name  string
		count int
	}
	pairs := make([]kv, 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].name < pairs[j].name
	})
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("%s:%d", p.name, p.count))
	}
	return strings.Join(parts, ",")
}
