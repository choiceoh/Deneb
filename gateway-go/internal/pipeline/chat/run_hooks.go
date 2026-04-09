package chat

import (
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	hookspkg "github.com/choiceoh/deneb/gateway-go/internal/runtime/hooks"
)

// wireStreamHooks registers all non-draft streaming hooks on the compositor:
// WebSocket broadcaster, typing signaler, status reactions, gateway events,
// and internal hook registry. The draft stream loop is wired separately in
// executeAgentRun because it has defer-based cleanup tied to that scope.
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
	if deps.emitAgentFn != nil {
		hc.OnToolStart(func(name, _ string, _ []byte) {
			deps.emitAgentFn("tool.start", params.SessionKey, params.ClientRunID, map[string]any{
				"tool": name,
				"ts":   time.Now().UnixMilli(),
			})
		})
		hc.OnToolResult(func(name, _, _ string, isErr bool) {
			deps.emitAgentFn("tool.end", params.SessionKey, params.ClientRunID, map[string]any{
				"tool":    name,
				"isError": isErr,
				"ts":      time.Now().UnixMilli(),
			})
		})
	}

	// Internal hook registry: fire tool.use event after each tool completes.
	if deps.internalHookRegistry != nil {
		hc.OnToolResult(func(name, toolUseID, _ string, isErr bool) {
			env := map[string]string{
				"DENEB_TOOL":        name,
				"DENEB_TOOL_USE_ID": toolUseID,
				"DENEB_IS_ERROR":    fmt.Sprintf("%t", isErr),
				"DENEB_SESSION_KEY": params.SessionKey,
			}
			go deps.internalHookRegistry.TriggerFromEvent(deps.shutdownCtx, hookspkg.EventToolUse, params.SessionKey, env)
		})
	}
}
