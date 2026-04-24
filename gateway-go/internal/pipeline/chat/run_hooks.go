package chat

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
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
	statusCtrl *telegram.StatusReactionController,
) {
	// Broadcaster: WebSocket streaming deltas.
	if broadcaster != nil {
		hc.OnTextDelta(broadcaster.EmitDelta)
		hc.OnToolEmit(broadcaster.EmitToolStart)
		hc.OnToolResult(func(name, toolUseID, result string, isErr bool) {
			broadcaster.EmitToolResult(name, toolUseID, result, isErr)
			if deps.broadcast != nil {
				deps.broadcast("session.tool", map[string]any{
					"sessionKey": params.SessionKey,
					"runId":      params.ClientRunID,
					"tool":       name,
					"toolUseId":  toolUseID,
					"isError":    isErr,
				})
			}
		})
	}

	// Typing signaler: UI typing indicators.
	if typingSignaler != nil {
		hc.OnTextDelta(typingSignaler.SignalTextDelta)
		hc.OnThinking(typingSignaler.SignalReasoningDelta)
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
		hc.OnThinking(statusCtrl.SetThinking)
		hc.OnToolStart(func(name, _ string, _ []byte) { statusCtrl.SetTool(name) })
		// First text delta means we moved past thinking — set thinking
		// emoji if not already in a tool phase.
		hc.OnTextDelta(func(_ string) { statusCtrl.SetThinking() })
	}

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
