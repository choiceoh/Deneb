package chat

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// wireStreamHooks registers all non-draft streaming hooks on the compositor:
// SSE broadcaster, typing signaler, and gateway events.
// The draft stream loop is wired separately in executeAgentRun because it has
// defer-based cleanup tied to that scope.
//
// It returns the per-run Hanja→Hangul transliterator wrapping the broadcaster's
// delta sink (nil when there is no broadcaster). The caller MUST Flush it after
// the agent loop so any backticks held across a fence boundary are released —
// see executeAgentRun.
func wireStreamHooks(
	hc *agent.HookCompositor,
	params RunParams,
	deps runDeps,
	broadcaster *streaming.Broadcaster,
	typingSignaler chatport.TypingSignaler,
) *streaming.Streamer {
	var deltaTranslit *streaming.Streamer

	// Persist each completed tool call before any UI observer sees it. The
	// executor later commits the canonical ordered tool_result batch; this
	// receipt exists only to bridge a process crash between completion and that
	// batch write. Ephemeral and Briefcase runs deliberately keep their existing
	// isolated persistence semantics.
	if !deps.briefcaseMode && !params.EphemeralAssistant {
		if receipts := chatport.ResolveToolResultReceiptStore(deps.transcript); receipts != nil {
			hc.OnToolResult(func(name, toolUseID, result string, isErr bool) {
				receipt := chatport.ToolResultReceipt{
					ToolUseID:   toolUseID,
					ToolName:    name,
					Content:     result,
					IsError:     isErr,
					CompletedAt: deps.now().UnixMilli(),
				}
				if err := receipts.AppendToolResultReceipt(params.SessionKey, receipt); err != nil && deps.logger != nil {
					deps.logger.Warn("tool result recovery receipt persist failed",
						"session", params.SessionKey,
						"tool", name,
						"toolUseId", toolUseID,
						"error", err)
				}
			})
		}
	}

	// Broadcaster: SSE streaming deltas. Read Sino-Korean Hanja as Hangul
	// live (報告書 → 보고서) so a Chinese-lineage model's stream doesn't flash Hanja
	// before the final settle. The Streamer is stream-safe (carries fence/word
	// state across deltas) and EmitDelta no-ops on the empty string it returns
	// while holding a partial fence marker.
	if broadcaster != nil {
		deltaTranslit = streaming.NewStreamer()
		hc.OnTextDelta(func(s string) { broadcaster.EmitDelta(deltaTranslit.Write(s)) })
		// Reasoning liveness for streaming transports (throttled inside the
		// broadcaster — OnThinking fires once per reasoning delta).
		hc.OnThinking(broadcaster.EmitThinking)
		hc.OnToolEmit(func(name, toolUseID string, input []byte) {
			// The detail hint (query/command/file name) turns the client's
			// waiting chip from "메일 확인 중" into "메일 확인 중: 아르고".
			broadcaster.EmitToolStart(name, toolUseID, toolport.ToolStreamDetail(name, input))
		})
		hc.OnToolResult(func(name, toolUseID, result string, isErr bool) {
			broadcaster.EmitToolResult(name, toolUseID, result, isErr)
			if deps.broadcast != nil {
				broadcastPayload(deps.broadcast, "session.tool", SessionToolEvent{
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
		// a single tool call is still executing. Without this, the typing
		// indicator's TTL expires during multi-minute compile/test/network
		// tool calls that emit no streaming tokens, and the "typing..."
		// indicator disappears from the chat while the agent is still busy.
		hc.OnToolProgress(func(_ string, _ string, _ int) {
			typingSignaler.SignalToolProgress(0)
		})
	}

	// Mutation failure escalation: a mutation tool reported an in-band failure
	// (banner added by MutationFailureAnnotator) that the agent loop saw as
	// isError=false. Per docs/agent-rules/logging.md, a user-observable failure must
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
			broadcastPayload(deps.broadcast, "chat.tool_failed", ChatToolFailedEvent{
				Session:    params.SessionKey,
				SessionKey: params.SessionKey,
				RunID:      params.ClientRunID,
				Tool:       name,
				Reason:     "mutation_tool_in_band_failure",
				Error:      mutationFailureError(result),
			})
		}
	})

	// Gateway event subscription: emit tool.start / tool.end for SSE clients.
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

	// Goal loop: idempotency guard (blocks duplicate destructive actions) +
	// ledger recorder (observes committed actions). Set only for goal-driven
	// runs; nil for all interactive/cron/heartbeat runs. Registered first so
	// it gates ahead of the untrusted-origin gate (wired after this function);
	// the compositor composes gates first-block-wins in registration order.
	if params.BeforeToolCall != nil {
		hc.OnBeforeToolCall(params.BeforeToolCall)
	}
	if params.OnToolResult != nil {
		hc.OnToolResult(params.OnToolResult)
	}
	return deltaTranslit
}
