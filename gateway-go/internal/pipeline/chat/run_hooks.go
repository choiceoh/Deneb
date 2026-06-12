package chat

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// wireStreamHooks registers all non-draft streaming hooks on the compositor:
// WebSocket broadcaster, typing signaler, status reactions, and gateway events.
// The draft stream loop is wired separately in executeAgentRun because it has
// defer-based cleanup tied to that scope.
func wireStreamHooks(
	hc *agent.HookCompositor,
	params RunParams,
	deps runDeps,
	broadcaster *streaming.Broadcaster,
	typingSignaler chatport.TypingSignaler,
	statusCtrl statusReactor,
) {
	// Broadcaster: WebSocket streaming deltas.
	if broadcaster != nil {
		hc.OnTextDelta(broadcaster.EmitDelta)
		// Reasoning liveness for streaming transports (throttled inside the
		// broadcaster — OnThinking fires once per reasoning delta).
		hc.OnThinking(broadcaster.EmitThinking)
		hc.OnToolEmit(func(name, toolUseID string, input []byte) {
			// The detail hint (query/command/file name) turns the client's
			// waiting chip from "메일 확인 중" into "메일 확인 중: 아르고".
			broadcaster.EmitToolStart(name, toolUseID, toolStreamDetail(name, input))
		})
		hc.OnToolResult(func(name, toolUseID, result string, isErr bool) {
			broadcaster.EmitToolResult(name, toolUseID, result, isErr)
			if deps.broadcast != nil {
				deps.broadcast("session.tool", SessionToolEvent{
					SessionKey: params.SessionKey,
					RunID:      params.ClientRunID,
					Tool:       name,
					ToolUseID:  toolUseID,
					IsError:    isErr,
				})
			}
		})
	}

	// Typing signaler: UI typing indicators.
	if typingSignaler != nil {
		hc.OnTextDelta(typingSignaler.SignalTextDelta)
		hc.OnThinking(func(string) { typingSignaler.SignalReasoningDelta() })
		hc.OnToolStart(func(_ string, _ string, _ []byte) {
			typingSignaler.SignalToolStart()
		})
		// Long-running tool heartbeat: refresh typing TTL periodically while
		// a single tool call is still executing. Without this, Telegram's
		// 30s typing TTL expires during multi-minute compile/test/network
		// tool calls that emit no streaming tokens, and the "typing..."
		// indicator disappears from the chat while the agent is still busy.
		hc.OnToolProgress(func(_ string, _ string, _ int) {
			typingSignaler.SignalToolProgress(0)
		})
	}

	// Status controller: Telegram emoji reactions.
	if statusCtrl != nil {
		hc.OnThinking(func(string) { statusCtrl.SetThinking() })
		hc.OnToolStart(func(name, _ string, _ []byte) { statusCtrl.SetTool(name) })
		// First text delta means we moved past thinking — set thinking
		// emoji if not already in a tool phase.
		hc.OnTextDelta(func(_ string) { statusCtrl.SetThinking() })
	}

	// Mutation failure escalation: a mutation tool reported an in-band failure
	// (banner added by MutationFailureAnnotator) that the agent loop saw as
	// isError=false. Per .claude/rules/logging.md, a user-observable failure must
	// surface as Error + a broadcast so the operator/UI sees the dropped action,
	// not just the agent. Runs regardless of broadcaster wiring. (research finding A)
	hc.OnToolResult(func(name, _ string, result string, isErr bool) {
		if isErr || !isMutationFailureResult(result) {
			return
		}
		if deps.logger != nil {
			deps.logger.Error("mutation tool reported in-band failure",
				"tool", name, "session", params.SessionKey, "runId", params.ClientRunID)
		}
		if deps.broadcast != nil {
			deps.broadcast("chat.tool_failed", ChatToolFailedEvent{
				Session:    params.SessionKey,
				SessionKey: params.SessionKey,
				RunID:      params.ClientRunID,
				Tool:       name,
				Reason:     "mutation_tool_in_band_failure",
				Error:      mutationFailureError(result),
			})
		}
	})

	// Gateway event subscription: emit tool.start / tool.end for WebSocket clients.
	if deps.callbacks.emitAgentFn != nil {
		hc.OnToolStart(func(name, _ string, _ []byte) {
			deps.callbacks.emitAgentFn("tool.start", params.SessionKey, params.ClientRunID, map[string]any{
				"tool": name,
				"ts":   time.Now().UnixMilli(),
			})
		})
		hc.OnToolResult(func(name, _, _ string, isErr bool) {
			deps.callbacks.emitAgentFn("tool.end", params.SessionKey, params.ClientRunID, map[string]any{
				"tool":    name,
				"isError": isErr,
				"ts":      time.Now().UnixMilli(),
			})
		})
	}

}
