package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
)

// SyncResult holds the outcome of a synchronous agent run.
type SyncResult struct {
	Text    string
	AllText string // accumulated text from all turns; used by cron delivery as a fallback when the final turn is NO_REPLY
	// DeliverableText is AllText minus the brief progress narration the model
	// emits alongside tool calls. Preferred by proactive/cron delivery so a
	// multi-turn report ships its answer turns without the "이제 위키 검색부터
	// 할게요" working narration. See agent.AgentResult.DeliverableText.
	DeliverableText string
	Model           string
	FellBack        bool // true when the model fallback chain fired (Model is the model that actually answered)
	InputTokens     int
	OutputTokens    int
	StopReason      string // "end_turn", "max_tokens", "tool_use", etc.
}

// BestText returns the answer to surface to the user. It prefers DeliverableText
// — the accumulation of every substantial answer turn with the interim
// "이제 ~할게요" tool-call narration removed — which fixes two failure modes at
// once: a short wrap-up final turn (the agent writes the body mid-run, then
// closes with "위키에 기록했습니다") no longer makes the answer vanish, and the
// working narration the model emits before tool calls never reaches the surface.
//
// Mirrors cronChatAdapter so every proactive surface agrees:
//   - DeliverableText present → use it (the common multi-turn case).
//   - else Text (the final turn) → use it.
//   - else AllText (last resort: a run that produced only narration before
//     aborting).
//
// NO_REPLY is stripped so the marker never leaks to the client.
func (r *SyncResult) BestText() string {
	if d := strings.TrimSpace(StripSilentToken(r.DeliverableText)); d != "" {
		return d
	}
	if t := strings.TrimSpace(r.Text); t != "" {
		return t
	}
	return strings.TrimSpace(StripSilentToken(r.AllText))
}

// SyncOptions holds optional parameters for synchronous agent runs.
// Used by the OpenAI-compatible HTTP endpoints to pass through sampling
// parameters and conversation context.
type SyncOptions struct {
	Temperature      *float64
	TopP             *float64
	MaxTokens        *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Stop             []string
	ResponseFormat   *llm.ResponseFormat
	ToolChoice       any // "auto", "none", "required", or structured object
	// Thinking overrides the session's thinking level for this run — a
	// resolveThinkingConfig level or "off"/"none" to disable the thinking
	// phase (cron jobs use this). Empty = session/provider default.
	Thinking string

	// Messages provides a full conversation context (system, user, assistant,
	// tool messages). When set, this replaces the normal transcript-based
	// context assembly, and the `message` parameter is ignored.
	Messages []llm.Message

	// SystemPrompt provides a system prompt extracted from the messages array.
	// Used when Messages is set and system messages were present.
	SystemPrompt string

	// ToolPreset restricts available tools for this run (e.g. "boot", "conversation").
	// Empty means no restriction.
	ToolPreset string

	// MaxHistoryTokens overrides the transcript history token budget.
	// When set, assembleContext trims older messages to fit within this budget.
	MaxHistoryTokens int

	// Delivery carries channel routing for proactive tool sends (e.g. message.send).
	// Required in cron / scheduled contexts: without it the message tool fails
	// with "no active delivery target" and the agent falls back to text-only
	// replies that the cron delivery layer may not route correctly.
	Delivery *DeliveryContext

	// EphemeralUser suppresses persistence of the inbound user-role message —
	// see RunParams.EphemeralUser. Set by autonomous triggers (heartbeat) so
	// recurring self-triggers do not crowd out the recent-history window.
	EphemeralUser bool

	// SkipRecall skips the long-term-memory recall preflight for this turn —
	// see RunParams.SkipRecall. Set from the native client's "memory off /
	// focused chat" toggle so general questions skip work-context injection.
	SkipRecall bool

	// EphemeralAssistant suppresses persistence of assistant/tool_result
	// messages produced during the run — see RunParams.EphemeralAssistant.
	// Heartbeat sets this true so autonomous ticks do not crowd out the
	// user's short-term conversation context; heartbeat state belongs in
	// HEARTBEAT.md instead.
	EphemeralAssistant bool

	// AutoDeliveredOutput marks a run whose final reply text is delivered by
	// the caller's run-completion path (e.g. the cron delivery layer) rather
	// than by the agent's in-loop `message` tool. Propagated to RunParams;
	// see RunParams.AutoDeliveredOutput.
	AutoDeliveredOutput bool

	// BeforeToolCall, when set, gates each tool execution (block + reason).
	// Propagated to RunParams.BeforeToolCall; the goal loop uses it for its
	// idempotency guard. nil = no gate.
	BeforeToolCall func(name, toolCallID string, input []byte) (block bool, blockReason string)

	// OnToolResult, when set, observes each tool result. Propagated to
	// RunParams.OnToolResult; the goal loop uses it to record committed
	// destructive actions into the ledger. nil = no observer.
	OnToolResult func(name, toolUseID, result string, isErr bool)

	// OnToolEvent, when set on a streaming run (SendSyncStream only), receives
	// tool lifecycle transitions (started/completed, with detail hint and
	// error flag) so the transport can surface live tool progress — the native
	// client renders these as the waiting indicator's tool label. Nil-safe.
	OnToolEvent func(ev ToolStreamEvent)

	// OnThinking, when set on a streaming run (SendSyncStream only), fires
	// while the model emits reasoning deltas (throttled by the broadcaster) so
	// the transport can show a "thinking" hint before the first visible token.
	// preview carries a chip-sized tail of the recent reasoning text ("" when
	// nothing readable accumulated yet).
	OnThinking func(preview string)

	// GateUntrustedTools enables the untrusted-origin tool gate (block exec /
	// gmail send when promptware enters the turn). Set by the interactive
	// native-client transports. Propagated to RunParams.GateUntrustedTools.
	GateUntrustedTools bool
}

// prepareSyncRun builds RunParams and runDeps from the common sync arguments.
// Both SendSync and SendSyncStream share this setup.
func (h *Handler) prepareSyncRun(sessionKey, message, model, runIDPrefix string, opts *SyncOptions) (RunParams, runDeps, error) {
	if h.sessions == nil {
		return RunParams{}, runDeps{}, fmt.Errorf("chat handler not initialized")
	}

	sess := h.sessions.Get(sessionKey)
	if sess == nil {
		sess = h.sessions.Create(sessionKey, "direct")
	}

	params := RunParams{
		SessionKey:  sessionKey,
		Message:     sanitizeInput(message),
		Model:       model,
		ClientRunID: shortid.New(runIDPrefix),
	}

	if opts != nil {
		params.Temperature = opts.Temperature
		params.TopP = opts.TopP
		params.MaxTokens = opts.MaxTokens
		params.FrequencyPenalty = opts.FrequencyPenalty
		params.PresencePenalty = opts.PresencePenalty
		params.Stop = opts.Stop
		params.ResponseFormat = opts.ResponseFormat
		params.ToolChoice = opts.ToolChoice
		params.Thinking = opts.Thinking
		if len(opts.Messages) > 0 {
			params.PrebuiltMessages = opts.Messages
		}
		if opts.SystemPrompt != "" {
			params.System = opts.SystemPrompt
		}
		if opts.ToolPreset != "" {
			sess.ToolPreset = opts.ToolPreset
		}
		if opts.Delivery != nil {
			params.Delivery = opts.Delivery
		}
		params.EphemeralUser = opts.EphemeralUser
		params.SkipRecall = opts.SkipRecall
		params.EphemeralAssistant = opts.EphemeralAssistant
		params.AutoDeliveredOutput = opts.AutoDeliveredOutput
		params.BeforeToolCall = opts.BeforeToolCall
		params.OnToolResult = opts.OnToolResult
		params.GateUntrustedTools = opts.GateUntrustedTools
	}

	deps := h.buildRunDeps()
	if opts != nil && opts.MaxHistoryTokens > 0 {
		// MaxHistoryTokens is the HISTORY budget, but MemoryTokenBudget is the
		// TOTAL (system + history) budget — run_exec derives
		// contextBudget = MemoryTokenBudget - SystemPromptBudget. Setting total =
		// history collapsed contextBudget to (history - system): boot's 30K-30K=0,
		// and skill-review's 1-30K underflowed (uint64) to a giant budget. Both
		// drove compaction to process the full uncapped transcript and stall. Add
		// the system budget back so the requested history budget survives intact.
		deps.contextCfg.MemoryTokenBudget = uint64(opts.MaxHistoryTokens) + deps.contextCfg.SystemPromptBudget
	}
	if h.recordActivity != nil && !params.EphemeralUser {
		h.recordActivity(sessionKey)
	}

	return params, deps, nil
}

// buildSyncResult converts a chatRunResult into a SyncResult, resolving the
// model name through the fallback chain (explicit → default → registry).
func (h *Handler) buildSyncResult(model string, result *chatRunResult) (*SyncResult, error) {
	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = h.DefaultModel()
	}
	if resolvedModel == "" && h.registry != nil {
		resolvedModel = h.registry.FullModelID(modelrole.RoleMain)
	}

	if result == nil {
		return nil, fmt.Errorf("agent run returned nil result")
	}

	// Prefer the model that actually answered (set when the fallback chain fired).
	if result.ActualModel != "" {
		resolvedModel = result.ActualModel
	}

	// Strip any chain-of-thought delimiters that leaked into the answer (see
	// reasoning_leak.go). The block regex matches here because the full assembled
	// text is available. TrimSpace cleans the gap a removed leading block leaves.
	return &SyncResult{
		Text:            strings.TrimSpace(stripReasoningLeak(result.Text)),
		AllText:         strings.TrimSpace(stripReasoningLeak(result.AllText)),
		DeliverableText: strings.TrimSpace(stripReasoningLeak(result.DeliverableText)),
		Model:           resolvedModel,
		FellBack:        result.FellBack,
		InputTokens:     result.Usage.InputTokens,
		OutputTokens:    result.Usage.OutputTokens,
		StopReason:      result.StopReason,
	}, nil
}

// SendSync runs the agent loop synchronously, blocking until the response is
// complete or the context is canceled. Used by the OpenAI-compatible HTTP
// endpoints and the native client's miniapp.chat.send.
func (h *Handler) SendSync(ctx context.Context, sessionKey, message, model string, opts *SyncOptions) (*SyncResult, error) {
	if res, handled := h.trySlashSync(sessionKey, message, opts); handled {
		return res, nil
	}
	message = h.maybeEnrichLinks(ctx, message, opts)
	params, deps, err := h.prepareSyncRun(sessionKey, message, model, "sync", opts)
	if err != nil {
		return nil, err
	}

	// Agent detail logging: without a RunLogger every SendSync surface
	// (miniapp.chat.send, cron single-run, heartbeat, boot, mail-qa, BTW) is
	// invisible in ~/.deneb/agent-logs and to the modeltuner's AggregateByModel.
	runLog := agentlog.NewRunLogger(deps.agentLog, params.SessionKey, params.ClientRunID)
	result, err := executeAgentRun(ctx, params, deps, nil, nil, nil, h.logger, runLog)
	if err != nil {
		return nil, err
	}
	res, err := h.buildSyncResult(model, result)
	if err == nil {
		h.autoTitleSessionAsync(sessionKey, message, res)
	}
	return res, err
}

// trySlashSync short-circuits slash commands on the synchronous send paths.
// The native client talks to the gateway via miniapp.chat.send (SendSync), so
// without this, slash input fell through to the LLM as plain text and the
// dispatch layer's reply (delivered via ReplyFn, unwired on the native-only
// deployment) was lost. The collector captures every immediate respond() call
// so the slash reply returns in the RPC response the client renders.
// Long-running commands (/update, /rollback, …) reply from their own
// goroutines later; their sync response is an acknowledgement only.
func (h *Handler) trySlashSync(sessionKey, message string, opts *SyncOptions) (*SyncResult, bool) {
	// PrebuiltMessages flows (OpenAI-compatible HTTP with full history) are
	// API traffic, not interactive chat — leave them untouched.
	if opts != nil && len(opts.Messages) > 0 {
		return nil, false
	}
	cmd := ParseSlashCommand(message)
	if cmd == nil || !cmd.Handled {
		return nil, false
	}
	var delivery *DeliveryContext
	if opts != nil {
		delivery = opts.Delivery
	}
	var reply strings.Builder
	h.handleSlashCommand(shortid.New("slash"), sessionKey, delivery, cmd, func(text string) {
		if text == "" {
			return
		}
		if reply.Len() > 0 {
			reply.WriteString("\n\n")
		}
		reply.WriteString(text)
	})
	text := reply.String()
	if text == "" {
		// Async commands ack immediately; their real output arrives later.
		text = fmt.Sprintf("`/%s` 명령을 실행했습니다.", cmd.Command)
	}
	return &SyncResult{
		Text:            text,
		AllText:         text,
		DeliverableText: text,
		Model:           "slash:" + cmd.Command,
		StopReason:      "slash_command",
	}, true
}

// SendSyncStream runs the agent loop, calling onDelta for each text chunk,
// then returning the final result. Used by streaming OpenAI-compatible
// endpoints and the native client's miniapp.chat.stream.
func (h *Handler) SendSyncStream(ctx context.Context, sessionKey, message, model string, opts *SyncOptions, onDelta func(string)) (*SyncResult, error) {
	if res, handled := h.trySlashSync(sessionKey, message, opts); handled {
		if onDelta != nil && res.Text != "" {
			onDelta(res.Text)
		}
		return res, nil
	}
	message = h.maybeEnrichLinks(ctx, message, opts)
	params, deps, err := h.prepareSyncRun(sessionKey, message, model, "stream", opts)
	if err != nil {
		return nil, err
	}

	// Wrap onDelta to scrub leaked reasoning delimiters per chunk so a literal
	// "[thinking]" never reaches the stream. The block regex can't match across
	// delta boundaries, but the standalone-marker strip catches the tokens; the
	// final answer is fully cleaned in buildSyncResult. See reasoning_leak.go.
	streamDelta := onDelta
	if onDelta != nil {
		streamDelta = func(delta string) {
			if cleaned := stripReasoningLeak(delta); cleaned != "" {
				onDelta(cleaned)
			}
		}
	}

	sinks := streamEventSinks{OnDelta: streamDelta}
	if opts != nil {
		sinks.OnTool = opts.OnToolEvent
		sinks.OnThinking = opts.OnThinking
	}
	result, err := executeAgentRunWithDelta(ctx, params, deps, sinks, h.logger)
	if err != nil {
		return nil, err
	}
	res, err := h.buildSyncResult(model, result)
	if err == nil {
		h.autoTitleSessionAsync(sessionKey, message, res)
	}
	return res, err
}
